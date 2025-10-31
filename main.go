package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gocarina/gocsv"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/jinzhu/now"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

var (
	store *sessions.CookieStore
)

// webFail – centralised error responder
func webFail(msg string, w http.ResponseWriter, err error, data ...interface{}) {
	if err != nil {
		log.Printf("%s | data: %v | error: %v", msg, data, err)
		fmt.Printf("%s | data: %v | error: %v\n", msg, data, err)
	} else {
		log.Printf("%s | data: %v", msg, data)
		fmt.Printf("%s | data: %v\n", msg, data)
	}
	type errResp struct {
		Message string `json:"message"`
		Details string `json:"details,omitempty"`
	}
	resp := errResp{Message: msg}
	if err != nil {
		resp.Details = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(resp)
}

// AuthMiddleware checks authentication and optionally role
func AuthMiddleware(requireRole string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := store.Get(r, "session-name")
		if err != nil {
			log.Printf("Session error: %v", err)
			http.Error(w, `{"message": "Session error"}`, http.StatusInternalServerError)
			return
		}

		userID, ok := session.Values["user_id"].(int)
		if !ok || userID == 0 {
			log.Printf("No user_id in session for %s", r.URL.Path)
			http.Error(w, `{"message": "Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		var companyID string
		var username, role string
		err = DB.QueryRow("SELECT c.company_id, u.username, u.role FROM users u JOIN companies c ON u.company_id = c.id WHERE u.id = ?", userID).Scan(&companyID, &username, &role)
		if err != nil {
			log.Printf("User not found for id %d: %v", userID, err)
			http.Error(w, `{"message": "Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		if requireRole != "" && role != requireRole {
			log.Printf("User %s (role %s) not authorized for %s (requires %s)", username, role, r.URL.Path, requireRole)
			http.Error(w, `{"message": "Forbidden"}`, http.StatusForbidden)
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, "company_id", companyID)
		ctx = context.WithValue(ctx, "user_id", userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserInfoHandler returns the current user's information including numeric id
func UserInfoHandler(w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, "session-name")
	if err != nil {
		log.Printf("Session error: %v", err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	userID, ok := session.Values["user_id"].(int)
	if !ok || userID == 0 {
		log.Printf("No user_id in session for %s", r.URL.Path)
		http.Error(w, `{"message": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var companyID string
	var username, role string
	err = DB.QueryRow("SELECT c.company_id, u.username, u.role FROM users u JOIN companies c ON u.company_id = c.id WHERE u.id = ?", userID).Scan(&companyID, &username, &role)
	if err != nil {
		log.Printf("User not found for id %d: %v", userID, err)
		http.Error(w, `{"message": "Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	response := map[string]interface{}{
		"id":         userID,
		"company_id": companyID,
		"username":   username,
		"role":       role,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ---------- LIST ASSIGNED STATS (for non-admin users) ----------
func ListAssignedStatsHandler(w http.ResponseWriter, r *http.Request) {
	uid := r.Context().Value("user_id").(int)

	rows, err := DB.Query(`
        SELECT 
            s.id, 
            s.short_id, 
            s.full_name, 
            s.type, 
            s.value_type, 
            s.reversed,
            COALESCE(GROUP_CONCAT(DISTINCT sua.user_id), '') AS user_ids,
            COALESCE(GROUP_CONCAT(DISTINCT sda.division_id), '') AS division_ids
        FROM stats s
        LEFT JOIN stat_user_assignments sua ON s.id = sua.stat_id
        LEFT JOIN stat_division_assignments sda ON s.id = sda.stat_id
        WHERE s.id IN (
            SELECT stat_id FROM stat_user_assignments WHERE user_id = ?
        )
        GROUP BY s.id
    `, uid)
	if err != nil {
		webFail("Failed to query assigned stats", w, err)
		return
	}
	defer rows.Close()

	type stat struct {
		ID          int    `json:"id"`
		ShortID     string `json:"short_id"`
		FullName    string `json:"full_name"`
		Type        string `json:"type"`
		ValueType   string `json:"value_type"`
		Reversed    bool   `json:"reversed"`
		UserIDs     []int  `json:"user_ids"`
		DivisionIDs []int  `json:"division_ids"`
	}

	var stats []stat

	for rows.Next() {
		var s stat
		var uids, dids string
		if err := rows.Scan(
			&s.ID, &s.ShortID, &s.FullName, &s.Type, &s.ValueType, &s.Reversed,
			&uids, &dids,
		); err != nil {
			webFail("Failed to scan assigned stat row", w, err)
			return
		}
		s.UserIDs = splitInt(uids)
		s.DivisionIDs = splitInt(dids)
		stats = append(stats, s)
	}

	if err = rows.Err(); err != nil {
		webFail("Error iterating assigned stats", w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// ---------- SERVICES: DB-backed daily/weekly handlers ----------

// Helper to parse int safely (for weekly/daily numeric fields)
func parseIntLenient(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	i, err := strconv.Atoi(s)
	return i, err
}


// GET /services/getDailyStats?date=<we>&stat=<name>
// DB-backed version: returns JSON with Thursday/Friday/Monday/Tuesday/Wednesday/Quota like old CSV-based handler
// ---------- Helper: resolve stat type and short id by stat_id or short_id ----------
func resolveStatIdentity(statIDStr, statShort string) (resolvedShort string, resolvedType string, err error) {
	// prefer stat_id if provided
	if statIDStr != "" {
		id, convErr := strconv.Atoi(statIDStr)
		if convErr != nil {
			return "", "", convErr
		}
		var shortID string
		var stype string
		q := `SELECT short_id, type FROM stats WHERE id = ? LIMIT 1`
		if scanErr := DB.QueryRow(q, id).Scan(&shortID, &stype); scanErr != nil {
			return "", "", scanErr
		}
		return strings.ToLower(shortID), stype, nil
	}

	// fallback to short id
	if statShort != "" {
		var stype string
		q := `SELECT type FROM stats WHERE LOWER(short_id) = ? LIMIT 1`
		if scanErr := DB.QueryRow(q, strings.ToLower(statShort)).Scan(&stype); scanErr != nil {
			// not found -> default to personal (safe)
			return strings.ToLower(statShort), "personal", nil
		}
		return strings.ToLower(statShort), stype, nil
	}

	return "", "", errors.New("either stat_id or stat (short id) must be provided")
}

// ---------- GET /services/getDailyStats (DB-backed, scoped by personal vs division), supports stat_id ----------
// Update to handleGetDailyStats: format currency values correctly (convert stored cents to "500.00" style strings).
// This replaces the previous handleGetDailyStats implementation to detect stat value_type and format returned daily values.

func handleGetDailyStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	thisWeek := q.Get("date")
	statIDStr := q.Get("stat_id")
	statShort := q.Get("stat")

	if thisWeek == "" || (statIDStr == "" && statShort == "") {
		webFail("date and (stat_id or stat) are required", w, errors.New("missing params"))
		return
	}
	if err := checkIfValidWE(thisWeek); err != nil {
		webFail("Invalid W/E date", w, err)
		return
	}

	// Resolve stat identity and metadata (prefer stat_id)
	var nameLower, statType, valueType string
	if statIDStr != "" {
		id, err := strconv.Atoi(statIDStr)
		if err != nil {
			webFail("Invalid stat_id", w, err)
			return
		}
		if err := DB.QueryRow(`SELECT short_id, type, value_type FROM stats WHERE id = ? LIMIT 1`, id).Scan(&nameLower, &statType, &valueType); err != nil {
			if err == sql.ErrNoRows {
				webFail("Stat not found", w, err)
				return
			}
			webFail("Failed to query stat", w, err)
			return
		}
		nameLower = strings.ToLower(nameLower)
	} else {
		// fallback to short id resolution (still support for legacy callers)
		if err := DB.QueryRow(`SELECT short_id, type, value_type FROM stats WHERE LOWER(short_id) = ? LIMIT 1`, strings.ToLower(statShort)).Scan(&nameLower, &statType, &valueType); err != nil {
			// If not found, treat as personal and default to string values
			nameLower = strings.ToLower(statShort)
			statType = "personal"
			valueType = "number"
		} else {
			nameLower = strings.ToLower(nameLower)
		}
	}

	// compute concrete dates (Thursday = thisWeek)
	we, _ := time.Parse("2006-01-02", thisWeek)
	dates := map[string]string{
		"Thursday":  we.Format("2006-01-02"),
		"Friday":    we.AddDate(0, 0, 1).Format("2006-01-02"),
		"Monday":    we.AddDate(0, 0, 4).Format("2006-01-02"),
		"Tuesday":   we.AddDate(0, 0, 5).Format("2006-01-02"),
		"Wednesday": we.AddDate(0, 0, 6).Format("2006-01-02"),
	}

	// Prepare DailyStat response and format currency values if needed.
	var rowDaily = DailyStat{
		Name:  strings.ToUpper(nameLower),
		Quota: "",
	}

	// session user id (for personal scope)
	sessionUserID := r.Context().Value("user_id").(int)

	for day, dateStr := range dates {
		// Query the stored integer value (assumed stored as integer; for currency it's cents)
		var v sql.NullInt64
		var err error
		if statType == "personal" {
			err = DB.QueryRow(`SELECT value FROM daily_stats WHERE LOWER(name)=? AND date=? AND user_id=? LIMIT 1`, nameLower, dateStr, sessionUserID).Scan(&v)
		} else {
			// divisional/main: look for division-level row (as before)
			err = DB.QueryRow(`SELECT value FROM daily_stats WHERE LOWER(name)=? AND date=? AND division_id IS NOT NULL LIMIT 1`, nameLower, dateStr).Scan(&v)
		}
		if err != nil && err != sql.ErrNoRows {
			webFail("Failed to query daily_stats", w, err)
			return
		}
		if !v.Valid {
			// leave empty string for missing values
			continue
		}

		// Format depending on value_type
		switch valueType {
		case "currency":
			// v.Int64 is cents (stored as integer); convert to USD and string like "500.00"
			usd := USD(v.Int64)
			// USD.String uses cents -> formatted dollars with two decimals
			formatted := usd.String()
			switch day {
			case "Thursday":
				rowDaily.Thursday = formatted
			case "Friday":
				rowDaily.Friday = formatted
			case "Monday":
				rowDaily.Monday = formatted
			case "Tuesday":
				rowDaily.Tuesday = formatted
			case "Wednesday":
				rowDaily.Wednesday = formatted
			}
		default:
			// For number/percentage (or unknown), return plain integer string
			s := fmt.Sprintf("%d", v.Int64)
			switch day {
			case "Thursday":
				rowDaily.Thursday = s
			case "Friday":
				rowDaily.Friday = s
			case "Monday":
				rowDaily.Monday = s
			case "Tuesday":
				rowDaily.Tuesday = s
			case "Wednesday":
				rowDaily.Wednesday = s
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rowDaily)
}

// ---------- POST /services/save7R (DB-backed), accepts stat_id or stat short id ----------
// Replace the handleSave7R implementation in main.go with this version.
// This version REQUIRES StatID to be provided per row and will NOT fall back to Name/short_id.
// It resolves the stat by id to determine its type (personal/divisional/main) and scopes deletes/inserts accordingly.

// Updated handleSave7R: requires StatID per row and uses statType directly (no unused vars).
func handleSave7R(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	thisWeek := q.Get("thisWeek")
	if thisWeek == "" {
		webFail("thisWeek query param required", w, errors.New("missing thisWeek"))
		return
	}
	if err := checkIfValidWE(thisWeek); err != nil {
		webFail("Invalid W/E date", w, err)
		return
	}

	// Decode incoming JSON; expect array of objects each with a required StatID
	var rawRows []map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&rawRows); err != nil {
		webFail("Failed to decode body", w, err)
		return
	}

	type Row struct {
		StatID    int
		Name      string
		Thursday  string
		Friday    string
		Monday    string
		Tuesday   string
		Wednesday string
		Quota     string
	}
	rows := make([]Row, 0, len(rawRows))

	for idx, rr := range rawRows {
		rw := Row{}
		v, ok := rr["StatID"]
		if !ok || v == nil {
			webFail(fmt.Sprintf("Missing StatID in payload row %d", idx), w, errors.New("StatID required"))
			return
		}
		switch vv := v.(type) {
		case float64:
			rw.StatID = int(vv)
		case int:
			rw.StatID = vv
		case string:
			id, err := strconv.Atoi(vv)
			if err != nil {
				webFail(fmt.Sprintf("Invalid StatID value in row %d", idx), w, err)
				return
			}
			rw.StatID = id
		default:
			webFail(fmt.Sprintf("Invalid StatID type in row %d", idx), w, errors.New("invalid StatID"))
			return
		}
		if n, ok := rr["Name"].(string); ok {
			rw.Name = n
		}
		if t, ok := rr["Thursday"].(string); ok {
			rw.Thursday = t
		}
		if t, ok := rr["Friday"].(string); ok {
			rw.Friday = t
		}
		if t, ok := rr["Monday"].(string); ok {
			rw.Monday = t
		}
		if t, ok := rr["Tuesday"].(string); ok {
			rw.Tuesday = t
		}
		if t, ok := rr["Wednesday"].(string); ok {
			rw.Wednesday = t
		}
		if qv, ok := rr["Quota"].(string); ok {
			rw.Quota = qv
		}
		rows = append(rows, rw)
	}

	// Validate rows using existing validateDailyStats (or your improved validator)
	// After decoding rows into []Row (where Row.StatID is required), validate using stat metadata:
	for _, v := range rows {
		// resolve stat metadata by id (no fallback)
		var shortID, valueType, statType string
		err := DB.QueryRow(`SELECT short_id, value_type, type FROM stats WHERE id = ? LIMIT 1`, v.StatID).Scan(&shortID, &valueType, &statType)
		if err != nil {
			if err == sql.ErrNoRows {
				webFail(fmt.Sprintf("Stat not found for StatID %d", v.StatID), w, err)
				return
			}
			webFail("Failed to query stat metadata", w, err)
			return
		}

		// build DailyStat for validation
		ds := DailyStat{
			Name:      shortID,
			Thursday:  v.Thursday,
			Friday:    v.Friday,
			Monday:    v.Monday,
			Tuesday:   v.Tuesday,
			Wednesday: v.Wednesday,
			Quota:     v.Quota,
		}

		if err := validateDailyStatByType(shortID, valueType, ds); err != nil {
			webFail("Validation failed for daily stat", w, err)
			return
		}
	
	}

	tx, err := DB.Begin()
	if err != nil {
		webFail("Failed to start transaction", w, err)
		return
	}

	we, _ := time.Parse("2006-01-02", thisWeek)
	dates := map[string]string{
		"Thursday":  we.Format("2006-01-02"),
		"Friday":    we.AddDate(0, 0, 1).Format("2006-01-02"),
		"Monday":    we.AddDate(0, 0, 4).Format("2006-01-02"),
		"Tuesday":   we.AddDate(0, 0, 5).Format("2006-01-02"),
		"Wednesday": we.AddDate(0, 0, 6).Format("2006-01-02"),
	}

	sessionUserID := r.Context().Value("user_id").(int)

	for _, row := range rows {
		// Resolve stat by ID (no fallback)
		var shortID string
		var statType string
		if err := DB.QueryRow(`SELECT short_id, type FROM stats WHERE id = ? LIMIT 1`, row.StatID).Scan(&shortID, &statType); err != nil {
			if err == sql.ErrNoRows {
				tx.Rollback()
				webFail(fmt.Sprintf("Stat not found for StatID %d", row.StatID), w, err)
				return
			}
			tx.Rollback()
			webFail("Failed to look up stat by StatID", w, err)
			return
		}
		nameLower := strings.ToLower(shortID)

		// Only support personal writes via this endpoint (safer). Reject divisional/main writes here.
		if statType != "personal" {
			tx.Rollback()
			webFail(fmt.Sprintf("Stat %s (id=%d) is not a personal stat and cannot be written via this endpoint", shortID, row.StatID), w, errors.New("invalid stat scope"))
			return
		}

		// Delete existing rows for this user and stat name for the week
		if _, err := tx.Exec(`DELETE FROM daily_stats WHERE LOWER(name)=? AND date IN (?,?,?,?,?) AND user_id = ?`, nameLower, dates["Thursday"], dates["Friday"], dates["Monday"], dates["Tuesday"], dates["Wednesday"], sessionUserID); err != nil {
			tx.Rollback()
			webFail("Failed to clear existing personal daily rows", w, err)
			return
		}

		dayValues := map[string]string{
			"Thursday":  row.Thursday,
			"Friday":    row.Friday,
			"Monday":    row.Monday,
			"Tuesday":   row.Tuesday,
			"Wednesday": row.Wednesday,
		}
		for day, raw := range dayValues {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			valueInt := 0
			if m, err := StringToMoney(raw); err == nil {
				valueInt = int(m.MoneyToUSD())
			} else {
				if i, err := strconv.Atoi(raw); err == nil {
					valueInt = i
				} else {
					tx.Rollback()
					webFail(fmt.Sprintf("Invalid numeric value for stat %d on %s: %s", row.StatID, day, raw), w, errors.New("invalid numeric"))
					return
				}
			}
			dateStr := dates[day]
			if _, err := tx.Exec(`INSERT INTO daily_stats (name, date, value, user_id) VALUES (?, ?, ?, ?)`, nameLower, dateStr, valueInt, sessionUserID); err != nil {
				tx.Rollback()
				webFail("Failed to insert personal daily row", w, err)
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		webFail("Failed to commit daily rows", w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message":"Saved 7R grid"}`)
}

// The rest of main() remains unchanged except for wiring the new handlers
//
func main() {
	f := CreateLog()
	defer f.Close()

	InitDB()

	store = sessions.NewCookieStore([]byte("super-secret-key"))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   3600 * 8,
		HttpOnly: true,
		Secure:   false,
	}

	router := mux.NewRouter()

	corsMiddleware := handlers.CORS(
		handlers.AllowedOrigins([]string{"http://localhost:3000"}),
		handlers.AllowedMethods([]string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"}),
		handlers.AllowedHeaders([]string{"Content-Type"}),
		handlers.AllowCredentials(),
	)

	// services endpoints - use DB-backed handlers
	router.Handle("/services/getWeeklyStats", AuthMiddleware("", http.HandlerFunc(handleGetWeeklyStats)))
	router.Handle("/services/getDailyStats", AuthMiddleware("", http.HandlerFunc(handleGetDailyStats)))
	router.Handle("/services/save7R", AuthMiddleware("", http.HandlerFunc(handleSave7R)))
	router.Handle("/services/saveWeeklyEdit", AuthMiddleware("", http.HandlerFunc(handleSaveWeeklyEdit)))
	router.Handle("/services/logWeeklyStats", AuthMiddleware("", http.HandlerFunc(handleLogWeeklyStats)))

	// Admin-only endpoints
	router.Handle("/users", AuthMiddleware("admin", http.HandlerFunc(UserHandler)))
	router.Handle("/api/users", AuthMiddleware("admin", http.HandlerFunc(ListUsersHandler)))
	router.Handle("/api/users/reset-password", AuthMiddleware("admin", http.HandlerFunc(ResetPasswordHandler)))
	router.Handle("/api/users/{id}", AuthMiddleware("admin", http.HandlerFunc(DeleteUserHandler)))
	router.Handle("/api/users/{id}/role", AuthMiddleware("admin", http.HandlerFunc(UpdateUserRoleHandler)))
	router.Handle("/api/stats", AuthMiddleware("admin", http.HandlerFunc(CreateStatHandler))).Methods("POST")
	router.Handle("/api/stats/{id}", AuthMiddleware("admin", http.HandlerFunc(UpdateStatHandler))).Methods("PATCH")
	router.Handle("/api/stats/{id}", AuthMiddleware("admin", http.HandlerFunc(DeleteStatHandler))).Methods("DELETE")
	router.Handle("/api/stats/all", AuthMiddleware("admin", http.HandlerFunc(ListAllStatsHandler))).Methods("GET")
	// NEW: assigned stats endpoint for non-admin users
	router.Handle("/api/stats/assigned", AuthMiddleware("", http.HandlerFunc(ListAssignedStatsHandler))).Methods("GET")

	router.Handle("/api/divisions", AuthMiddleware("admin", http.HandlerFunc(CreateDivisionHandler))).Methods("POST")
	router.Handle("/api/divisions/{id}", AuthMiddleware("admin", http.HandlerFunc(DeleteDivisionHandler))).Methods("DELETE")
	router.Handle("/api/divisions", AuthMiddleware("admin", http.HandlerFunc(ListDivisionsHandler))).Methods("GET")

	// User info endpoint
	router.Handle("/api/user", AuthMiddleware("", http.HandlerFunc(UserInfoHandler)))

	// Change password endpoint (for any authenticated user)
	router.Handle("/api/change-password", AuthMiddleware("", http.HandlerFunc(ChangePasswordHandler)))

	// Auth endpoints (unprotected)
	router.HandleFunc("/login", LoginHandler)
	router.HandleFunc("/logout", LogoutHandler)
	router.HandleFunc("/register", RegisterHandler)

	// Static file handlers left as-is
	cssHandler := http.FileServer(http.Dir("public/css"))
	router.PathPrefix("/public/css/").Handler(http.StripPrefix("/public/css", addHeaders(cssHandler, "text/css", "public/css")))

	jsHandler := http.FileServer(http.Dir("public/js"))
	router.PathPrefix("/public/js/").Handler(http.StripPrefix("/public/js", addHeaders(jsHandler, "application/javascript", "public/js")))

	semanticHandler := http.FileServer(http.Dir("public/Semantic-UI-2.3.0/dist"))
	router.PathPrefix("/public/Semantic-UI-2.3.0/dist/").Handler(http.StripPrefix("/public/Semantic-UI-2.3.0/dist", addHeaders(semanticHandler, "", "public/Semantic-UI-2.3.0/dist")))

	videoHandler := http.FileServer(http.Dir("public/AV"))
	router.PathPrefix("/public/AV/").Handler(http.StripPrefix("/public/AV", addHeaders(videoHandler, "video/mp4", "public/AV")))

	publicHandler := http.FileServer(http.Dir("public"))
	router.PathPrefix("/public/").Handler(http.StripPrefix("/public", addHeaders(publicHandler, "", "public")))

	router.PathPrefix("/").HandlerFunc(handleIndex)

	http.Handle("/", corsMiddleware(router))

	port := ":9090"
	fmt.Printf("Running Stat HQ on %s\n", port)
	log.Fatal(http.ListenAndServe(port, nil))
}

// (other functions in the file remain unchanged; only handlers above were added/modified)
// ---------- CREATE STAT ----------
func CreateStatHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
        return
    }

    // 1. Decode FIRST
    var req struct {
        ShortID     string `json:"short_id"`
        FullName    string `json:"full_name"`
        Type        string `json:"type"`
        ValueType   string `json:"value_type"`
        Reversed    bool   `json:"reversed"`
        UserIDs     []int  `json:"user_ids"`
        DivisionIDs []int  `json:"division_ids"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        webFail("Invalid JSON payload", w, err)
        return
    }

    // 2. Validate AFTER decode
    if strings.TrimSpace(req.ShortID) == "" {
        webFail("Short ID is required", w, nil)
        return
    }
    if strings.TrimSpace(req.FullName) == "" {
        webFail("Full Name is required", w, nil)
        return
    }

    // Optional: sanitize
    req.ShortID = strings.ToUpper(strings.TrimSpace(req.ShortID))
    req.FullName = strings.TrimSpace(req.FullName)

    // 3. Start transaction
    tx, err := DB.Begin()
    if err != nil {
        webFail("Failed to start transaction", w, err)
        return
    }

    // 4. Insert stat
    res, err := tx.Exec(`
        INSERT INTO stats (short_id, full_name, type, value_type, reversed) 
        VALUES (?, ?, ?, ?, ?)`,
        req.ShortID, req.FullName, req.Type, req.ValueType, req.Reversed,
    )
    if err != nil {
        tx.Rollback()
        webFail("Failed to insert stat", w, err)
        return
    }

    statID, _ := res.LastInsertId()

    // 5. Assign users
    for _, uid := range req.UserIDs {
        if _, err := tx.Exec(
            `INSERT INTO stat_user_assignments (stat_id, user_id) VALUES (?, ?)`,
            statID, uid,
        ); err != nil {
            tx.Rollback()
            webFail("Failed to assign user", w, err, "user_id", uid)
            return
        }
    }

    // 6. Assign divisions
    for _, did := range req.DivisionIDs {
        if _, err := tx.Exec(
            `INSERT INTO stat_division_assignments (stat_id, division_id) VALUES (?, ?)`,
            statID, did,
        ); err != nil {
            tx.Rollback()
            webFail("Failed to assign division", w, err, "division_id", did)
            return
        }
    }

    // 7. Commit
    if err := tx.Commit(); err != nil {
        webFail("Failed to commit transaction", w, err)
        return
    }

    // 8. Success
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(map[string]string{"message": "Stat created"})
}

// ---------- UPDATE STAT ----------
func UpdateStatHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPatch {
        http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
        return
    }

    idStr := mux.Vars(r)["id"]
    id, err := strconv.Atoi(idStr)
    if err != nil {
        webFail("Invalid stat ID", w, err)
        return
    }

    var req struct {
        ShortID     string `json:"short_id"`
        FullName    string `json:"full_name"`
        Type        string `json:"type"`
        ValueType   string `json:"value_type"`
        Reversed    bool   `json:"reversed"`
        UserIDs     []int  `json:"user_ids"`
        DivisionIDs []int  `json:"division_ids"`
    }

    // DECODE FIRST
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        webFail("Invalid JSON payload", w, err)
        return
    }

    // VALIDATE
    if strings.TrimSpace(req.ShortID) == "" {
        webFail("Short ID is required", w, nil)
        return
    }
    if strings.TrimSpace(req.FullName) == "" {
        webFail("Full Name is required", w, nil)
        return
    }

    req.ShortID = strings.ToUpper(strings.TrimSpace(req.ShortID))
    req.FullName = strings.TrimSpace(req.FullName)

    tx, err := DB.Begin()
    if err != nil {
        webFail("Failed to start transaction", w, err)
        return
    }

    // Update stat
    _, err = tx.Exec(`
        UPDATE stats 
        SET short_id = ?, full_name = ?, type = ?, value_type = ?, reversed = ?
        WHERE id = ?`,
        req.ShortID, req.FullName, req.Type, req.ValueType, req.Reversed, id,
    )
    if err != nil {
        tx.Rollback()
        webFail("Failed to update stat", w, err, "id", id)
        return
    }

    // Clear + reassign
    tx.Exec(`DELETE FROM stat_user_assignments WHERE stat_id = ?`, id)
    tx.Exec(`DELETE FROM stat_division_assignments WHERE stat_id = ?`, id)

    for _, uid := range req.UserIDs {
        tx.Exec(`INSERT INTO stat_user_assignments (stat_id, user_id) VALUES (?, ?)`, id, uid)
    }
    for _, did := range req.DivisionIDs {
        tx.Exec(`INSERT INTO stat_division_assignments (stat_id, division_id) VALUES (?, ?)`, id, did)
    }

    if err := tx.Commit(); err != nil {
        webFail("Failed to commit", w, err)
        return
    }

    json.NewEncoder(w).Encode(map[string]string{"message": "Stat updated"})
}

// ---------- DELETE STAT ----------
func DeleteStatHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
        return
    }

    idStr := mux.Vars(r)["id"]
    id, _ := strconv.Atoi(idStr)

    _, err := DB.Exec(`DELETE FROM stats WHERE id=?`, id)
    if err != nil {
        webFail("Failed to delete stat", w, err, "id", id)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"message": "Stat deleted"})
}

// ---------- LIST ALL STATS (with assignments) ----------
func ListAllStatsHandler(w http.ResponseWriter, r *http.Request) {
    rows, err := DB.Query(`
        SELECT 
            s.id, 
            s.short_id, 
            s.full_name, 
            s.type, 
            s.value_type, 
            s.reversed,
			u.username,
			d.name as div_name,
            COALESCE(GROUP_CONCAT(DISTINCT sua.user_id), '') AS user_ids,
            COALESCE(GROUP_CONCAT(DISTINCT sda.division_id), '') AS division_ids
        FROM stats s
        LEFT JOIN stat_user_assignments sua ON s.id = sua.stat_id
		LEFT JOIN users u ON sua.user_id = u.id
        LEFT JOIN stat_division_assignments sda ON s.id = sda.stat_id
		LEFT JOIN divisions d ON sda.division_id = d.id
        GROUP BY s.id
    `)
    if err != nil {
        webFail("Failed to query stats", w, err)
        return
    }
    defer rows.Close()

    type stat struct {
        ID          int    `json:"id"`
        ShortID     string `json:"short_id"`
        FullName    string `json:"full_name"`
        Type        string `json:"type"`
        ValueType   string `json:"value_type"`
        Reversed    bool   `json:"reversed"`
		UserName    string  `json:"username"`
		DivName    string   `json:"div_name"`
        UserIDs     []int  `json:"user_ids"`
        DivisionIDs []int  `json:"division_ids"`
    }

    var stats []stat

    for rows.Next() {
        var s stat
        var uids, dids string // will be "" if no assignments
        if err := rows.Scan(
            &s.ID, &s.ShortID, &s.FullName, &s.Type, &s.ValueType, &s.Reversed,&s.UserName,&s.DivName,
            &uids, &dids,
        ); err != nil {
            webFail("Failed to scan stat row", w, err)
            return
        }
        s.UserIDs = splitInt(uids)
        s.DivisionIDs = splitInt(dids)
        stats = append(stats, s)
    }

    if err = rows.Err(); err != nil {
        webFail("Error iterating stats", w, err)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(stats) // [] if no stats
}

func splitInt(s string) []int {
    if s == "" {
        return []int{}
    }
    parts := strings.Split(s, ",")
    out := make([]int, 0, len(parts))
    for _, p := range parts {
        if i, err := strconv.Atoi(p); err == nil {
            out = append(out, i)
        }
    }
    return out
}

// ListUsersHandler returns all users for the admin's company
func ListUsersHandler(w http.ResponseWriter, r *http.Request) {
	companyID := r.Context().Value("company_id").(string)
	rows, err := DB.Query(`
		SELECT u.id, u.username, u.role
		FROM users u
		JOIN companies c ON u.company_id = c.id
		WHERE c.company_id = ?
	`, companyID)
	if err != nil {
		log.Printf("Error fetching users for company %s: %v", companyID, err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := []map[string]interface{}{}
	for rows.Next() {
		var id int
		var username, role string
		if err := rows.Scan(&id, &username, &role); err != nil {
			log.Printf("Error scanning user: %v", err)
			continue
		}
		users = append(users, map[string]interface{}{
			"id":       id,
			"username": username,
			"role":     role,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

// ChangePasswordHandler allows users to change their own password
func ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Invalid change password request: %v", err)
		http.Error(w, `{"message": "Invalid request"}`, http.StatusBadRequest)
		return
	}

	userID := r.Context().Value("user_id").(int)

	var passwordHash string
	err := DB.QueryRow("SELECT password_hash FROM users WHERE id = ?", userID).Scan(&passwordHash)
	if err != nil {
		log.Printf("User %d not found: %v", userID, err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.OldPassword)); err != nil {
		log.Printf("Invalid old password for user %d", userID)
		http.Error(w, `{"message": "Invalid old password"}`, http.StatusUnauthorized)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing new password: %v", err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	_, err = DB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(newHash), userID)
	if err != nil {
		log.Printf("Error updating password for user %d: %v", userID, err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Password changed for user %d", userID)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message": "Password changed successfully"}`)
}

// ResetPasswordHandler resets a user's password
func ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID      int    `json:"user_id"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Invalid reset password request: %v", err)
		http.Error(w, `{"message": "Invalid request"}`, http.StatusBadRequest)
		return
	}

	companyID := r.Context().Value("company_id").(string)
    var userCompanyID string
    err := DB.QueryRow("SELECT c.company_id FROM users u JOIN companies c ON u.company_id = c.id WHERE u.id = ?", req.UserID).Scan(&userCompanyID)
    if err != nil || userCompanyID != companyID {
        log.Printf("User %d not found or not in company %s: %v", req.UserID, companyID, err)
        http.Error(w, `{"message": "User not found"}`, http.StatusNotFound)
        return
    }

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	_, err = DB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(hash), req.UserID)
	if err != nil {
		log.Printf("Error updating password for user %d: %v", req.UserID, err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Password reset for user %d in company %s", req.UserID, companyID)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message": "Password reset successful"}`)
}

// DeleteUserHandler deletes a user
func DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["id"]

	companyID := r.Context().Value("company_id").(string)
	adminID := r.Context().Value("user_id").(int)

	if userID == fmt.Sprintf("%d", adminID) {
		log.Printf("Admin %d attempted to delete themselves", adminID)
		http.Error(w, `{"message": "Cannot delete own account"}`, http.StatusForbidden)
		return
	}

	var userCompanyID string
    err := DB.QueryRow("SELECT c.company_id FROM users u JOIN companies c ON u.company_id = c.id WHERE u.id = ?", userID).Scan(&userCompanyID)
    if err != nil || userCompanyID != companyID {
        log.Printf("User %s not found or not in company %s: %v", userID, companyID, err)
        http.Error(w, `{"message": "User not found"}`, http.StatusNotFound)
        return
    }

	_, err = DB.Exec("DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		log.Printf("Error deleting user %s: %v", userID, err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Deleted user %s from company %s", userID, companyID)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message": "User deleted successfully"}`)
}

// UpdateUserRoleHandler updates a user's role
func UpdateUserRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, `{"message": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	vars := mux.Vars(r)
	userID := vars["id"]
	

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Invalid update role request: %v", err)
		http.Error(w, `{"message": "Invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.Role != "user" && req.Role != "admin" {
		log.Printf("Invalid role: %s", req.Role)
		http.Error(w, `{"message": "Invalid role"}`, http.StatusBadRequest)
		return
	}

	companyID := r.Context().Value("company_id").(string)
	adminID := r.Context().Value("user_id").(int)

	if userID == fmt.Sprintf("%d", adminID) {
		log.Printf("Admin %d attempted to change their own role", adminID)
		http.Error(w, `{"message": "Cannot change own role"}`, http.StatusForbidden)
		return
	}

	var userCompanyID string
    err := DB.QueryRow("SELECT c.company_id FROM users u JOIN companies c ON u.company_id = c.id WHERE u.id = ?", userID).Scan(&userCompanyID)
    if err != nil || userCompanyID != companyID {
        log.Printf("User %s not found or not in company %s: %v", userID, companyID, err)
        http.Error(w, `{"message": "User not found"}`, http.StatusNotFound)
        return
    }

	_, err = DB.Exec("UPDATE users SET role = ? WHERE id = ?", req.Role, userID)
	if err != nil {
		log.Printf("Error updating role for user %s: %v", userID, err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Updated role for user %s to %s in company %s", userID, req.Role, companyID)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message": "Role updated successfully"}`)
}

// LoginHandler handles login requests
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var creds struct {
		CompanyID string `json:"company_id"`
		Username  string `json:"username"`
		Password  string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		log.Printf("Invalid login request: %v", err)
		http.Error(w, `{"message": "Invalid request"}`, http.StatusBadRequest)
		return
	}

	// Fetch user
	var userID int
	var hash, role string
	err := DB.QueryRow(`
		SELECT u.id, u.password_hash, u.role
		FROM users u
		JOIN companies c ON u.company_id = c.id
		WHERE c.company_id = ? AND u.username = ?
	`, creds.CompanyID, creds.Username).Scan(&userID, &hash, &role)
	if err != nil {
		log.Printf("Invalid credentials for %s/%s: %v", creds.CompanyID, creds.Username, err)
		http.Error(w, `{"message": "Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	// Compare password
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(creds.Password)); err != nil {
		log.Printf("Password mismatch for %s/%s", creds.CompanyID, creds.Username)
		http.Error(w, `{"message": "Invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	// Set session
	session, _ := store.Get(r, "session-name")
	session.Values["user_id"] = userID
	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to save session: %v", err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("Successful login for %s/%s (role %s)", creds.CompanyID, creds.Username, role)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message": "Login successful"}`)
}

// LogoutHandler clears the session
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	session, err := store.Get(r, "session-name")
	if err != nil {
		log.Printf("Session error on logout: %v", err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	// Clear session
	session.Values["user_id"] = 0
	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to save session on logout: %v", err)
		http.Error(w, `{"message": "Server error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("User logged out")
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message": "Logout successful"}`)
}

// RegisterHandler handles company signup
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CompanyID   string `json:"company_id"`
		CompanyName string `json:"company_name"`
		Username    string `json:"username"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Invalid register request: %v", err)
		http.Error(w, `{"message": "Invalid request"}`, http.StatusBadRequest)
		return
	}

	if err := RegisterCompany(req.CompanyID, req.CompanyName, req.Username, req.Password); err != nil {
		log.Printf("Registration failed for %s/%s: %v", req.CompanyID, req.Username, err)
		http.Error(w, `{"message": "Registration failed"}`, http.StatusBadRequest)
		return
	}

	log.Printf("Registered company %s and admin %s", req.CompanyID, req.Username)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message": "Registration successful"}`)
}

// UserHandler handles creating new users (admin only)
func UserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message": "Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CompanyID string `json:"company_id"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Role      string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Invalid user creation request: %v", err)
		http.Error(w, `{"message": "Invalid request"}`, http.StatusBadRequest)
		return
	}

	if err := RegisterUser(req.CompanyID, req.Username, req.Password, req.Role); err != nil {
		log.Printf("User creation failed for %s/%s: %v", req.CompanyID, req.Username, err)
		http.Error(w, `{"message": "User creation failed"}`, http.StatusBadRequest)
		return
	}

	log.Printf("Created user %s (role %s) for company %s", req.Username, req.Role, req.CompanyID)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message": "User created successfully"}`)
}


// func main() {
// 	f := CreateLog()
// 	defer f.Close()

// 	InitDB()

// 	store = sessions.NewCookieStore([]byte("super-secret-key"))
// 	store.Options = &sessions.Options{
// 		Path:     "/",
// 		MaxAge:   3600 * 8,
// 		HttpOnly: true,
// 		Secure:   false,
// 	}

// 	router := mux.NewRouter()

// 	corsMiddleware := handlers.CORS(
// 		handlers.AllowedOrigins([]string{"http://localhost:3000"}),
// 		handlers.AllowedMethods([]string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"}),
// 		handlers.AllowedHeaders([]string{"Content-Type"}),
// 		handlers.AllowCredentials(),
// 	)

// 	// services endpoints
// 	router.Handle("/services/getWeeklyStats", AuthMiddleware("", http.HandlerFunc(handleWeeklyStatsRequest)))
// 	router.Handle("/services/getDailyStats", AuthMiddleware("", http.HandlerFunc(handleDailyStatsRequest)))
// 	router.Handle("/services/save7R", AuthMiddleware("", http.HandlerFunc(handleSave7R)))
// 	router.Handle("/services/saveWeeklyEdit", AuthMiddleware("", http.HandlerFunc(handleSaveWeeklyEdit)))
// 	router.Handle("/services/logWeeklyStats", AuthMiddleware("", http.HandlerFunc(handleLogWeeklyStats)))

// 	// Admin-only endpoints
// 	router.Handle("/users", AuthMiddleware("admin", http.HandlerFunc(UserHandler)))
// 	router.Handle("/api/users", AuthMiddleware("admin", http.HandlerFunc(ListUsersHandler)))
// 	router.Handle("/api/users/reset-password", AuthMiddleware("admin", http.HandlerFunc(ResetPasswordHandler)))
// 	router.Handle("/api/users/{id}", AuthMiddleware("admin", http.HandlerFunc(DeleteUserHandler)))
// 	router.Handle("/api/users/{id}/role", AuthMiddleware("admin", http.HandlerFunc(UpdateUserRoleHandler)))
// 	router.Handle("/api/stats", AuthMiddleware("admin", http.HandlerFunc(CreateStatHandler))).Methods("POST")
// 	router.Handle("/api/stats/{id}", AuthMiddleware("admin", http.HandlerFunc(UpdateStatHandler))).Methods("PATCH")
// 	router.Handle("/api/stats/{id}", AuthMiddleware("admin", http.HandlerFunc(DeleteStatHandler))).Methods("DELETE")
// 	router.Handle("/api/stats/all", AuthMiddleware("admin", http.HandlerFunc(ListAllStatsHandler))).Methods("GET")
// 	router.Handle("/api/divisions", AuthMiddleware("admin", http.HandlerFunc(CreateDivisionHandler))).Methods("POST")
// 	router.Handle("/api/divisions/{id}", AuthMiddleware("admin", http.HandlerFunc(DeleteDivisionHandler))).Methods("DELETE")
// 	router.Handle("/api/divisions", AuthMiddleware("admin", http.HandlerFunc(ListDivisionsHandler))).Methods("GET")

// 	// User info endpoint
// 	router.Handle("/api/user", AuthMiddleware("", http.HandlerFunc(UserInfoHandler)))

// 	// Change password endpoint (for any authenticated user)
// 	router.Handle("/api/change-password", AuthMiddleware("", http.HandlerFunc(ChangePasswordHandler)))
	
// 	// Auth endpoints (unprotected)
// 	router.HandleFunc("/login", LoginHandler)
// 	router.HandleFunc("/logout", LogoutHandler)
// 	router.HandleFunc("/register", RegisterHandler)

// 	// Static file handlers
// 	cssHandler := http.FileServer(http.Dir("public/css"))
// 	router.PathPrefix("/public/css/").Handler(http.StripPrefix("/public/css", addHeaders(cssHandler, "text/css", "public/css")))

// 	jsHandler := http.FileServer(http.Dir("public/js"))
// 	router.PathPrefix("/public/js/").Handler(http.StripPrefix("/public/js", addHeaders(jsHandler, "application/javascript", "public/js")))

// 	semanticHandler := http.FileServer(http.Dir("public/Semantic-UI-2.3.0/dist"))
// 	router.PathPrefix("/public/Semantic-UI-2.3.0/dist/").Handler(http.StripPrefix("/public/Semantic-UI-2.3.0/dist", addHeaders(semanticHandler, "", "public/Semantic-UI-2.3.0/dist")))

// 	videoHandler := http.FileServer(http.Dir("public/AV"))
// 	router.PathPrefix("/public/AV/").Handler(http.StripPrefix("/public/AV", addHeaders(videoHandler, "video/mp4", "public/AV")))

// 	publicHandler := http.FileServer(http.Dir("public"))
// 	router.PathPrefix("/public/").Handler(http.StripPrefix("/public", addHeaders(publicHandler, "", "public")))

// 	router.PathPrefix("/").HandlerFunc(handleIndex)

// 	http.Handle("/", corsMiddleware(router))

// 	port := ":9090"
// 	fmt.Printf("Running Stat HQ on %s\n", port)
// 	log.Fatal(http.ListenAndServe(port, nil))
// }

// ---------- LIST ALL DIVISIONS ----------
func ListDivisionsHandler(w http.ResponseWriter, r *http.Request) {
    rows, err := DB.Query(`SELECT id, name FROM divisions ORDER BY name`)
    if err != nil {
        webFail("Failed to query divisions", w, err)
        return
    }
    defer rows.Close()

    type division struct {
        ID   int    `json:"id"`
        Name string `json:"name"`
    }

    var divs []division
    for rows.Next() {
        var d division
        if err := rows.Scan(&d.ID, &d.Name); err != nil {
            webFail("Failed to scan division", w, err)
            return
        }
        divs = append(divs, d)
    }

    if err = rows.Err(); err != nil {
        webFail("Error reading divisions", w, err)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(divs)
}

// ---------- CREATE DIVISION ----------
func CreateDivisionHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
        return
    }

    var req struct {
        Name string `json:"name"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        webFail("Invalid JSON", w, err)
        return
    }
    if strings.TrimSpace(req.Name) == "" {
        webFail("Division name is required", w, nil)
        return
    }

    i, err := DB.Exec(`INSERT INTO divisions (name) VALUES (?)`, req.Name)
    if err != nil {
        webFail("Failed to create division", w, err)
        return
    }

	fmt.Printf("Created div: %s, id: %v\n", req.Name, i)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"message": "Division created"})
}

// ---------- DELETE DIVISION ----------
func DeleteDivisionHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
        return
    }

    idStr := mux.Vars(r)["id"]
    id, _ := strconv.Atoi(idStr)

    _, err := DB.Exec(`DELETE FROM divisions WHERE id = ?`, id)
    if err != nil {
        webFail("Failed to delete division", w, err, "id", id)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"message": "Division deleted"})
}

// addHeaders sets explicit or dynamic MIME types with detailed logging
func addHeaders(fs http.Handler, mimeType, baseDir string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Path
        log.Printf("Handler for %s serving request: %s", baseDir, path)

        filePath := filepath.Join(baseDir, strings.TrimPrefix(path, "/"+baseDir))
        if _, err := os.Stat(filePath); os.IsNotExist(err) {
            http.Error(w, "File not found", http.StatusNotFound)
            log.Printf("File not found: %s (resolved as %s)", path, filePath)
            return
        }

        if mimeType != "" {
            w.Header().Set("Content-Type", mimeType)
            log.Printf("Set Content-Type: %s for %s (file: %s)", mimeType, path, filePath)
        } else {
            file, err := os.Open(filePath)
            if err != nil {
                http.Error(w, "File not found", http.StatusNotFound)
                log.Printf("Error opening file: %s, error: %v", filePath, err)
                return
            }
            defer file.Close()
            buffer := make([]byte, 512)
            n, err := file.Read(buffer)
            if err != nil && err != io.EOF {
                http.Error(w, "Error reading file", http.StatusInternalServerError)
                log.Printf("Error reading file %s: %v", filePath, err)
                return
            }
            contentType := http.DetectContentType(buffer[:n])
            if strings.HasSuffix(strings.ToLower(path), ".css") {
                contentType = "text/css"
            } else if strings.HasSuffix(strings.ToLower(path), ".js") {
                contentType = "application/javascript"
            } else if strings.HasSuffix(strings.ToLower(path), ".png") {
                contentType = "image/png"
            }
            w.Header().Set("Content-Type", contentType)
            log.Printf("Detected Content-Type: %s for %s (file: %s)", contentType, path, filePath)
            file.Seek(0, 0)
        }
        w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
        fs.ServeHTTP(w, r)
    }
}

// handleIndex serves the React app
func handleIndex(w http.ResponseWriter, r *http.Request) {
    // Serve index.html for all routes to support React Router
    http.ServeFile(w, r, "public/build/index.html")
}


func FileExists(name string) (bool, error) {
	_, err := os.Stat(name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

type Json7R struct {
	ThisWeek string      `json:"ThisWeek"`
	Stats    []DailyStat `json:"Stats"`
}

func validateDailyStats(v DailyStat) error {

	switch v.Name {
	case "GI", "VSD":
		_, err := StringToMoney(v.Thursday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Thursday for stat %s is invalid. Please check your data and try again", v.Thursday, v.Name)
			return errors.New(msg)
		}
		_, err = StringToMoney(v.Friday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Friday for stat %s is invalid. Please check your data and try again", v.Friday, v.Name)
			return errors.New(msg)
		}
		_, err = StringToMoney(v.Monday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Monday for stat %s is invalid. Please check your data and try again", v.Monday, v.Name)
			return errors.New(msg)
		}
		_, err = StringToMoney(v.Tuesday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Tuesday for stat %s is invalid. Please check your data and try again", v.Tuesday, v.Name)
			return errors.New(msg)
		}
		_, err = StringToMoney(v.Wednesday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Wednesday for stat %s is invalid. Please check your data and try again", v.Wednesday, v.Name)
			return errors.New(msg)
		}
		_, err = StringToMoney(v.Quota)
		if err != nil {
			msg := fmt.Sprintf("Value %v for the %s Quota is invalid. Please check your data and try again", v.Quota, v.Name)
			return errors.New(msg)
		}
	case "Sites":
		_, err := strconv.Atoi(v.Thursday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Thursday for stat %s is invalid. Please check your data and try again", v.Thursday, v.Name)
			return errors.New(msg)
		}
		_, err = strconv.Atoi(v.Friday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Friday for stat %s is invalid. Please check your data and try again", v.Friday, v.Name)
			return errors.New(msg)
		}
		_, err = strconv.Atoi(v.Monday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Monday for stat %s is invalid. Please check your data and try again", v.Monday, v.Name)
			return errors.New(msg)
		}
		_, err = strconv.Atoi(v.Tuesday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Tuesday for stat %s is invalid. Please check your data and try again", v.Tuesday, v.Name)
			return errors.New(msg)
		}
		_, err = strconv.Atoi(v.Wednesday)
		if err != nil {
			msg := fmt.Sprintf("Value %v on Wednesday for stat %s is invalid. Please check your data and try again", v.Wednesday, v.Name)
			return errors.New(msg)
		}
		_, err = strconv.Atoi(v.Quota)
		if err != nil {
			msg := fmt.Sprintf("Value %v for the %s Quota is invalid. Please check your data and try again", v.Quota, v.Name)
			return errors.New(msg)
		}
	default:
		msg := fmt.Sprintf("The stat name %v is not valid. It should be GI, VSD or Sites\n", v.Name)
		return errors.New(msg)
	}

	return nil
}

type Daily7RStatFloat struct {
	ThisWeek float64
	LastWeek float64
	Cum      float64
	Quota    float64
	Day      string
}

type Daily7RStatInt struct {
	ThisWeek int
	LastWeek int
	Cum      int
	Quota    int
	Day      string
}

func handleDailyStatsRequest(w http.ResponseWriter, r *http.Request) {

	q := r.URL.Query()
	thisWeek := q.Get("date")
	statName := q.Get("stat")

	err := checkIfValidWE(thisWeek)
	if err != nil {
		msg := fmt.Sprintf("Invalid W/E date")
		webFail(msg, w, err)
		return
	}

	//get this weeks 7r and parse the csv into a struct...
	fileName := fmt.Sprintf("public/dailyStats/%s.csv", thisWeek)
	file, err := os.Open(fileName)
	if errors.Is(err, os.ErrNotExist) {
		// handle the case where the file doesn't exist
		_, err = copyFile("public/dailyStats/template.csv", fileName)
		if err != nil {
			msg := "failed to create copy of the template.csv file"
			webFail(msg, w, err)
			return
		}
		file, err = os.Open(fileName)
		if err != nil {
			msg := fmt.Sprintf("Couldn't open %s", fileName)
			webFail(msg, w, err)
			return
		}
	} else if err != nil {
		msg := fmt.Sprintf("couldn't open the file: %s", fileName)
		webFail(msg, w, err)
		return
	}

	statGrid := make([]DailyStat, 0)
	err = gocsv.UnmarshalFile(file, &statGrid)
	if err != nil {
		msg := "Failed to unmarshal csv file"
		webFail(msg, w, err)
		return
	}
	var thisWkStat = DailyStat{}
	for _, v := range statGrid {
		if strings.ToUpper(v.Name) == strings.ToUpper(statName) {
			thisWkStat = v
			break
		}
	}

	if thisWkStat.Name == "" {
		msg := fmt.Sprintf("Didn't find match for %s", statName)
		webFail("", w, errors.New(msg))
		return
	}
	//get last weeks stat
	t, _ := time.Parse("2006-01-02", thisWeek)
	t.Add(time.Hour * -24 * 7)
	lastWeek := t.Format("2006-01-02")

	fileNameLastWk := fmt.Sprintf("public/dailyStats/%s.csv", lastWeek)
	fileLastWk, err := os.Open(fileNameLastWk)
	if errors.Is(err, os.ErrNotExist) {
		// handle the case where the file doesn't exist
		_, err = copyFile("public/dailyStats/template.csv", fileNameLastWk)
		if err != nil {
			msg := "failed to create copy of the template.csv file"
			webFail(msg, w, err)
			return
		}
		fileLastWk, err = os.Open(fileNameLastWk)
		if err != nil {
			msg := fmt.Sprintf("Couldn't open %s", fileNameLastWk)
			webFail(msg, w, err)
			return
		}
	} else if err != nil {
		msg := fmt.Sprintf("couldn't open the file: %s", fileNameLastWk)
		webFail(msg, w, err)
		return
	}

	statGridLastWk := make([]DailyStat, 0)
	err = gocsv.UnmarshalFile(fileLastWk, &statGridLastWk)
	if err != nil {
		msg := "Failed to unmarshal csv file"
		webFail(msg, w, err)
		return
	}
	var lastWkStat = DailyStat{}
	for _, v := range statGridLastWk {
		if v.Name == statName {
			lastWkStat = v
			break
		}
	}

	//compile stuff:

	if statName == "gi" || statName == "vsd" {
		thursCum, err := CumWeekFloat(thisWkStat.Thursday)
		if err != nil {
			webFail("", w, err)
			return
		}
		friCum, err := CumWeekFloat(thisWkStat.Thursday, thisWkStat.Friday)
		if err != nil {
			webFail("", w, err)
			return
		}
		monCum, err := CumWeekFloat(thisWkStat.Thursday, thisWkStat.Friday, thisWkStat.Monday)
		if err != nil {
			webFail("", w, err)
			return
		}
		tuesCum, err := CumWeekFloat(thisWkStat.Thursday, thisWkStat.Friday, thisWkStat.Monday, thisWkStat.Tuesday)
		if err != nil {
			webFail("", w, err)
			return
		}
		wedCum, err := CumWeekFloat(thisWkStat.Thursday, thisWkStat.Friday, thisWkStat.Monday, thisWkStat.Tuesday, thisWkStat.Wednesday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastThursCum, err := CumWeekFloat(lastWkStat.Thursday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastFriCum, err := CumWeekFloat(lastWkStat.Thursday, lastWkStat.Friday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastMonCum, err := CumWeekFloat(lastWkStat.Thursday, lastWkStat.Friday, lastWkStat.Monday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastTuesCum, err := CumWeekFloat(lastWkStat.Thursday, lastWkStat.Friday, lastWkStat.Monday, lastWkStat.Tuesday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastWedCum, err := CumWeekFloat(lastWkStat.Thursday, lastWkStat.Friday, lastWkStat.Monday, lastWkStat.Tuesday, lastWkStat.Wednesday)
		if err != nil {
			webFail("", w, err)
			return
		}
		thursQuota, err := GetQuotaFloat(1, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}
		friQuota, err := GetQuotaFloat(2, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}
		monQuota, err := GetQuotaFloat(3, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}
		tuesQuota, err := GetQuotaFloat(4, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}
		wedQuota, err := GetQuotaFloat(5, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}

		if thisWkStat.Thursday == "" {
			thisWkStat.Thursday = "0.00"
		}
		thisThursDaily, err := strconv.ParseFloat(thisWkStat.Thursday, 64)
		if err != nil {
			webFail("thursday 7r fail", w, err)
			return
		}

		week7R := make([]Daily7RStatFloat, 0)
		thurs := Daily7RStatFloat{
			ThisWeek: thisThursDaily,
			LastWeek: lastThursCum,
			Cum:      thursCum,
			Quota:    thursQuota,
			Day:      "Thursday",
		}
		week7R = append(week7R, thurs)

		if thisWkStat.Friday == "" {
			thisWkStat.Friday = "0.00"
		}
		thisFriDaily, err := strconv.ParseFloat(thisWkStat.Friday, 64)
		if err != nil {
			webFail("friday 7r fail", w, err)
			return
		}

		fri := Daily7RStatFloat{
			ThisWeek: thisFriDaily,
			LastWeek: lastFriCum,
			Cum:      friCum,
			Quota:    friQuota,
			Day:      "Friday",
		}
		week7R = append(week7R, fri)

		if thisWkStat.Monday == "" {
			thisWkStat.Monday = "0.00"
		}
		thisMonDaily, err := strconv.ParseFloat(thisWkStat.Monday, 64)
		if err != nil {
			webFail("monday 7r fail", w, err)
			return
		}

		mon := Daily7RStatFloat{
			ThisWeek: thisMonDaily,
			LastWeek: lastMonCum,
			Cum:      monCum,
			Quota:    monQuota,
			Day:      "Monday",
		}
		week7R = append(week7R, mon)

		if thisWkStat.Tuesday == "" {
			thisWkStat.Tuesday = "0.00"
		}
		thisTuesDaily, err := strconv.ParseFloat(thisWkStat.Tuesday, 64)
		if err != nil {
			webFail("tuesday 7r fail", w, err)
			return
		}
		tues := Daily7RStatFloat{
			ThisWeek: thisTuesDaily,
			LastWeek: lastTuesCum,
			Cum:      tuesCum,
			Quota:    tuesQuota,
			Day:      "Tuesday",
		}
		week7R = append(week7R, tues)

		if thisWkStat.Wednesday == "" {
			thisWkStat.Wednesday = "0.00"
		}
		thisWedDaily, err := strconv.ParseFloat(thisWkStat.Wednesday, 64)
		if err != nil {
			webFail("wednesday 7r fail", w, err)
			return
		}
		wed := Daily7RStatFloat{
			ThisWeek: thisWedDaily,
			LastWeek: lastWedCum,
			Cum:      wedCum,
			Quota:    wedQuota,
			Day:      "Wednesday",
		}
		week7R = append(week7R, wed)

		err = json.NewEncoder(w).Encode(week7R)
		if err != nil {
			msg := fmt.Sprintf("Failed to encode for 7R")
			webFail(msg, w, err)
			return
		}

	} else if statName == "sites" {
		thursCum, err := CumWeekInt(thisWkStat.Thursday)
		if err != nil {
			webFail("", w, err)
			return
		}
		friCum, err := CumWeekInt(thisWkStat.Thursday, thisWkStat.Friday)
		if err != nil {
			webFail("", w, err)
			return
		}
		monCum, err := CumWeekInt(thisWkStat.Thursday, thisWkStat.Friday, thisWkStat.Monday)
		if err != nil {
			webFail("", w, err)
			return
		}
		tuesCum, err := CumWeekInt(thisWkStat.Thursday, thisWkStat.Friday, thisWkStat.Monday, thisWkStat.Tuesday)
		if err != nil {
			webFail("", w, err)
			return
		}
		wedCum, err := CumWeekInt(thisWkStat.Thursday, thisWkStat.Friday, thisWkStat.Monday, thisWkStat.Tuesday, thisWkStat.Wednesday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastThursCum, err := CumWeekInt(lastWkStat.Thursday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastFriCum, err := CumWeekInt(lastWkStat.Thursday, lastWkStat.Friday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastMonCum, err := CumWeekInt(lastWkStat.Thursday, lastWkStat.Friday, lastWkStat.Monday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastTuesCum, err := CumWeekInt(lastWkStat.Thursday, lastWkStat.Friday, lastWkStat.Monday, lastWkStat.Tuesday)
		if err != nil {
			webFail("", w, err)
			return
		}
		lastWedCum, err := CumWeekInt(lastWkStat.Thursday, lastWkStat.Friday, lastWkStat.Monday, lastWkStat.Tuesday, lastWkStat.Wednesday)
		if err != nil {
			webFail("", w, err)
			return
		}
		thursQuota, err := GetQuotaInt(1, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}
		friQuota, err := GetQuotaInt(2, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}
		monQuota, err := GetQuotaInt(3, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}
		tuesQuota, err := GetQuotaInt(4, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}
		wedQuota, err := GetQuotaInt(5, thisWkStat.Quota)
		if err != nil {
			webFail("", w, err)
			return
		}

		if thisWkStat.Thursday == "" {
			thisWkStat.Thursday = "0.00"
		}
		thisThursDaily, err := strconv.Atoi(thisWkStat.Thursday)
		if err != nil {
			webFail("thursday 7r fail", w, err)
			return
		}
		week7R := make([]Daily7RStatInt, 0)
		thurs := Daily7RStatInt{
			ThisWeek: thisThursDaily,
			LastWeek: lastThursCum,
			Cum:      thursCum,
			Quota:    thursQuota,
			Day:      "Thursday",
		}
		week7R = append(week7R, thurs)

		if thisWkStat.Friday == "" {
			thisWkStat.Friday = "0.00"
		}
		thisFriDaily, err := strconv.Atoi(thisWkStat.Friday)
		if err != nil {
			webFail("friday 7r fail", w, err)
			return
		}
		fri := Daily7RStatInt{
			ThisWeek: thisFriDaily,
			LastWeek: lastFriCum,
			Cum:      friCum,
			Quota:    friQuota,
			Day:      "Friday",
		}
		week7R = append(week7R, fri)

		if thisWkStat.Monday == "" {
			thisWkStat.Monday = "0.00"
		}
		thisMonDaily, err := strconv.Atoi(thisWkStat.Monday)
		if err != nil {
			webFail("monday 7r fail", w, err)
			return
		}
		mon := Daily7RStatInt{
			ThisWeek: thisMonDaily,
			LastWeek: lastMonCum,
			Cum:      monCum,
			Quota:    monQuota,
			Day:      "Monday",
		}
		week7R = append(week7R, mon)

		if thisWkStat.Tuesday == "" {
			thisWkStat.Tuesday = "0.00"
		}
		thisTuesDaily, err := strconv.Atoi(thisWkStat.Tuesday)
		if err != nil {
			webFail("tuesday 7r fail", w, err)
			return
		}
		tues := Daily7RStatInt{
			ThisWeek: thisTuesDaily,
			LastWeek: lastTuesCum,
			Cum:      tuesCum,
			Quota:    tuesQuota,
			Day:      "Tuesday",
		}
		week7R = append(week7R, tues)

		if thisWkStat.Wednesday == "" {
			thisWkStat.Wednesday = "0.00"
		}
		thisWedDaily, err := strconv.Atoi(thisWkStat.Wednesday)
		if err != nil {
			webFail("wednesday 7r fail", w, err)
			return
		}
		wed := Daily7RStatInt{
			ThisWeek: thisWedDaily,
			LastWeek: lastWedCum,
			Cum:      wedCum,
			Quota:    wedQuota,
			Day:      "Wednesday",
		}
		week7R = append(week7R, wed)

		err = json.NewEncoder(w).Encode(week7R)
		if err != nil {
			msg := fmt.Sprintf("Failed to encode for 7R")
			webFail(msg, w, err)
			return
		}

	} else {
		msg := fmt.Sprintf("Didn't find stat name: %s.", statName)
		webFail(msg, w, errors.New(msg))
		return
	}

	return
}

func GetQuotaInt(i int, q string) (int, error) {
	if q == "" {
		q = "0"
	}
	n, err := strconv.Atoi(q)
	if err != nil {
		return 0, err
	}
	v := (n / 5) * i

	return v, nil
}

func GetQuotaFloat(i int, q string) (float64, error) {
	if q == "" {
		q = "0.00"
	}
	fl, err := strconv.ParseFloat(q, 64)
	if err != nil {
		return 0, err
	}
	pennies := ToUSD(fl)
	pennies = pennies.Divide(5)
	pennies = pennies.Multiply(float64(i))

	return pennies.Float64(), nil
}

func CumWeekInt(args ...string) (int, error) {
	var i int
	for _, v := range args {
		if v == "" {
			v = "0"
		}
		value, err := strconv.Atoi(v)
		if err != nil {
			return 0, err
		}
		i += value
	}
	return i, nil
}

func CumWeekFloat(args ...string) (float64, error) {

	var d USD
	for _, v := range args {
		m, err := StringToMoney(v)
		if err != nil {
			return 0.00, err
		}
		d += m.MoneyToUSD()
	}

	return d.Float64(), nil
}

// func handleSave7R(w http.ResponseWriter, r *http.Request) {

// 	statGrid := make([]DailyStat, 3)

// 	err := json.NewDecoder(r.Body).Decode(&statGrid)
// 	if err != nil {
// 		msg := "Failed to decode body"
// 		webFail(msg, w, err)
// 		return
// 	}

// 	fmt.Println(statGrid)

// 	for _, v := range statGrid {
// 		err = validateDailyStats(v)
// 		if err != nil {
// 			webFail("", w, err)
// 			return
// 		}
// 	}

// 	q := r.URL.Query()
// 	thisWeek := q.Get("thisWeek")

// 	err = checkIfValidWE(thisWeek)
// 	if err != nil {
// 		msg := "The weekending date is not valid or is not Thursday"
// 		webFail(msg, w, err)
// 		return
// 	}

// 	fileName := fmt.Sprintf("public/dailyStats/%s.csv", thisWeek)

// 	err = os.Remove(fileName)
// 	if err != nil {
// 		msg := "Failed to delete a file which makes it impossible to save"
// 		webFail(msg, w, err)
// 		return
// 	}

// 	file, err := os.Create(fileName)
// 	if err != nil {
// 		msg := fmt.Sprintf("Failed to create file %s", fileName)
// 		webFail(msg, w, err)
// 		return
// 	}

// 	err = gocsv.MarshalFile(statGrid, file)
// 	if err != nil {
// 		msg := "Failed to marshal the file"
// 		webFail(msg, w, err)
// 		return
// 	}

// 	msg := "Saved 7R grid"
// 	io.WriteString(w, msg)

// 	return
// }

// func addHeaders(fs http.Handler) http.HandlerFunc {
// 	return func(w http.ResponseWriter, r *http.Request) {
// 		w.Header().Set("Content-Type", "application/javascript")
// 		// w.Header().Add("X-Frame-Options", "DENY")
// 		fs.ServeHTTP(w, r)
// 	}
// }

// func handleIndex(w http.ResponseWriter, req *http.Request) {

// 	http.Redirect(w, req, "/tpl/home", 302)
// 	return
// 	// renderTemplate(w, req, "home", nil)

// }

type IntWeeklyStatValue struct {
	WeekEnding string
	Value      int
}

type FloatWeeklyStatValue struct {
	WeekEnding string
	Value      float64
}

type SingleWeeklyStat struct {
	WeekEnding string  `csv:"we" json:"Weekending"`
	GI         float64 `csv:"gi" json:"GI"`
	VSD        float64 `csv:"vsd" json:"VSD"`
	Expenses   float64 `csv:"expenses" json:"Expenses"`
	Scheduled  int     `csv:"scheduled" json:"Scheduled"`
	Sites      int     `csv:"sites" json:"Sites"`
	Outstanding	int     `csv:"outstanding" json:"Outstanding"`
	Profit     float64 `csv:"-"`
}

func handleWeeklyStatsRequest(w http.ResponseWriter, r *http.Request) {

	q := r.URL.Query()
	statName := q.Get("stat")

	statSlice := make([]SingleWeeklyStat, 0)

	f, err := os.Open("public/stats/weekly.csv")
	if err != nil {
		webFail("Failed to open weekly.csv", w, err)
		return
	}
	defer f.Close()

	err = gocsv.UnmarshalFile(f, &statSlice)
	if err != nil {
		webFail("Failed to unmarshal file weekly.csv", w, err)
		return
	}

	//sort the array just in case they were entered out of order
	sort.Slice(statSlice, func(i, j int) bool {
		return statSlice[i].WeekEnding < statSlice[j].WeekEnding
	})

	//build profit:
	for k, v := range statSlice {
		gUSD := ToUSD(v.GI)
		eUSD := ToUSD(v.Expenses)
		pUSD := gUSD - eUSD
		statSlice[k].Profit = pUSD.Float64()
	}

	err = json.NewEncoder(w).Encode(statSlice)
	if err != nil {
		msg := fmt.Sprintf("Failed to encode for stat %s", statName)
		webFail(msg, w, err)
		return
	}

	return
}

func getWeeklyStatData(statName string) (interface{}, error) {

	switch statName {
	case "gi", "vsd", "expenses", "outstanding":
		fileName := fmt.Sprintf("public/stats/%s.csv", statName)

		f, err := os.Open(fileName)
		if err != nil {
			msg := fmt.Sprintf("Failed to open file %s. %v\n", fileName, err.Error())
			return nil, errors.New(msg)
		}
		statValues := make([]FloatWeeklyStatValue, 0)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			lineSep := strings.Split(line, ",")
			if len(lineSep) != 2 {
				msg := fmt.Sprintf("Couldn't split line: %v. %s\n", line, err.Error())
				return nil, errors.New(msg)
			}
			m, err := StringToMoney(lineSep[1])
			if err != nil {
				msg := fmt.Sprintf("Couldn't convert money %v. %s\n", lineSep[1], err.Error())
				return nil, errors.New(msg)
			}
			i := m.MoneyToUSD().Float64()
			stat := FloatWeeklyStatValue{
				WeekEnding: lineSep[0],
				Value:      i,
			}
			statValues = append(statValues, stat)
		}

		sort.Slice(statValues, func(i, j int) bool {
			return statValues[i].WeekEnding < statValues[j].WeekEnding
		})

		return statValues, nil

	case "sites", "scheduled":
		fileName := fmt.Sprintf("public/stats/%s.csv", statName)

		f, err := os.Open(fileName)
		if err != nil {
			msg := fmt.Sprintf("Failed to open file %s. %s\n", fileName, err.Error())
			return nil, errors.New(msg)
		}
		statValues := make([]IntWeeklyStatValue, 0)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			lineSep := strings.Split(line, ",")
			if len(lineSep) != 2 {
				msg := fmt.Sprintf("Couldn't split line: %v. %s\n", line, err.Error())
				return nil, errors.New(msg)
			}
			i, err := strconv.Atoi(lineSep[1])
			if err != nil {
				msg := fmt.Sprintf("Couldn't convert string to number. The source data is bad: %v. %s\n", lineSep[1], err.Error())
				return nil, errors.New(msg)
			}
			stat := IntWeeklyStatValue{
				WeekEnding: lineSep[0],
				Value:      i,
			}
			statValues = append(statValues, stat)
		}

		sort.Slice(statValues, func(i, j int) bool {
			return statValues[i].WeekEnding < statValues[j].WeekEnding
		})

		return statValues, nil

	case "profit":
		expenseFileName := fmt.Sprintf("public/stats/expenses.csv")
		ef, err := os.Open(expenseFileName)
		if err != nil {
			msg := fmt.Sprintf("Failed to open file %s. %s\n", expenseFileName, err.Error())
			return nil, errors.New(msg)
		}
		giFileName := fmt.Sprintf("public/stats/gi.csv")
		gf, err := os.Open(giFileName)
		if err != nil {
			msg := fmt.Sprintf("Failed to open file %s. %s\n", giFileName, err.Error())
			return nil, errors.New(msg)
		}

		eValues := make([]FloatWeeklyStatValue, 0)
		eScanner := bufio.NewScanner(ef)
		for eScanner.Scan() {
			line := eScanner.Text()
			lineSep := strings.Split(line, ",")
			if len(lineSep) != 2 {
				msg := fmt.Sprintf("Couldn't split line: %v. %s\n", line, err.Error())
				return nil, errors.New(msg)
			}
			m, err := StringToMoney(lineSep[1])
			if err != nil {
				msg := fmt.Sprintf("Couldn't convert money %v. %s\n", lineSep[1], err.Error())
				return nil, errors.New(msg)
			}
			i := m.MoneyToUSD().Float64()
			stat := FloatWeeklyStatValue{
				WeekEnding: lineSep[0],
				Value:      i,
			}
			eValues = append(eValues, stat)
		}

		sort.Slice(eValues, func(i, j int) bool {
			return eValues[i].WeekEnding < eValues[j].WeekEnding
		})

		gValues := make([]FloatWeeklyStatValue, 0)
		gScanner := bufio.NewScanner(gf)
		for gScanner.Scan() {
			line := gScanner.Text()
			lineSep := strings.Split(line, ",")
			if len(lineSep) != 2 {
				msg := fmt.Sprintf("Couldn't split line: %v. %s\n", line, err.Error())
				return nil, errors.New(msg)
			}
			m, err := StringToMoney(lineSep[1])
			if err != nil {
				msg := fmt.Sprintf("Couldn't convert money %v. %s\n", lineSep[1], err.Error())
				return nil, errors.New(msg)
			}
			i := m.MoneyToUSD().Float64()
			stat := FloatWeeklyStatValue{
				WeekEnding: lineSep[0],
				Value:      i,
			}
			gValues = append(gValues, stat)
		}

		sort.Slice(gValues, func(i, j int) bool {
			return gValues[i].WeekEnding < gValues[j].WeekEnding
		})
		profitValues := make([]FloatWeeklyStatValue, 0)
		for k, v := range eValues {
			if v.WeekEnding != gValues[k].WeekEnding {
				msg := fmt.Sprintf("Weekendings don't match between the expense weeks and the gi weeks: %s, %s\n", v.WeekEnding, gValues[k].WeekEnding)
				return nil, errors.New(msg)
			}

			//float64 gi to USD
			gUSD := ToUSD(gValues[k].Value)
			eUSD := ToUSD(v.Value)
			pUSD := gUSD - eUSD
			pFloat := pUSD.Float64()
			pValue := FloatWeeklyStatValue{
				WeekEnding: v.WeekEnding,
				Value:      pFloat,
			}
			profitValues = append(profitValues, pValue)
		}

		return profitValues, nil

	default:
		msg := fmt.Sprintf("Stat %s not found\n", statName)
		return nil, errors.New(msg)
	}

}

// Checks that the weekending date passed in is the correct format and that it is a Thursday. It returns nil upon success.
func checkIfValidWE(we string) error {
	t, err := time.Parse("2006-01-02", we)
	if err != nil || t.Weekday() != time.Thursday {
		return fmt.Errorf("The weekending date is invalid")
	}
	return nil

}

// USD represents US dollar amount in terms of cents
type USD int64

// ToUSD converts a float64 to USD
// e.g. 1.23 to $1.23, 1.345 to $1.35
func ToUSD(f float64) USD {
	return USD((f * 100) + 0.5)
}

// Float64 converts a USD to float64
func (m USD) Float64() float64 {
	x := float64(m)
	x = x / 100
	return x
}

// Multiply safely multiplies a USD value by a float64, rounding
// to the nearest cent.
func (m USD) Multiply(f float64) USD {
	x := (float64(m) * f) + 0.5
	return USD(x)
}

func (m USD) Divide(f float64) USD {
	x := (float64(m) / f) + 0.5
	return USD(x)
}

// String returns a formatted USD value
func (m USD) String() string {
	x := float64(m)
	x = x / 100
	return fmt.Sprintf("%.2f", x)
}

type Money struct {
	Dollars  int
	Cents    int
	Negative bool
}

func StringToMoney(s string) (Money, error) {
	if s == "" {
		s = "0.00"
	}
	fl, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return Money{}, err
	}

	var neg bool
	if fl < 0 {
		neg = true
	}
	str := fmt.Sprintf("%.2f", fl)

	parts := strings.Split(str, ".")
	if len(parts) != 2 {
		err := errors.New("couldn't split parts of money")
		return Money{}, err
	}
	d, err := strconv.Atoi(parts[0])
	if err != nil {
		return Money{}, err
	}

	c, err := strconv.Atoi(parts[1])
	if err != nil {
		return Money{}, err
	}
	m := Money{
		Dollars:  d,
		Cents:    c,
		Negative: neg,
	}

	return m, nil
}

func (m *Money) MoneyToUSD() USD {
	c := m.Dollars * 100
	c += m.Cents
	return USD(c)
}


// change this so it is simply a generic template loader:
func handleTemplates(w http.ResponseWriter, r *http.Request) {

	name := strings.TrimPrefix(r.URL.Path, "/tpl/")

	if name == "inputWeeklyStats" {
		RenderInputWeekly(w, r, name)
		return
	}

	if name == "inputDailyStats" {
		RenderInputDaily(w, r, name)
		return
	}

	if name == "editStatsView" {
		RenderEditWeekly(w, r, name)
		return
	}

	if name == "viewDailyStats" {
		RenderViewDaily(w, r, name)
		return
	}

	renderTemplate(w, r, name, nil)

	return
}

func RenderViewDaily(w http.ResponseWriter, r *http.Request, name string) {

	weeks := getWeeks(16)

	data := struct {
		Weeks []string
	}{weeks}

	renderTemplate(w, r, name, data)

	return
}

func RenderEditWeekly(w http.ResponseWriter, r *http.Request, name string) {

	f, err := os.Open("public/stats/weekly.csv")
	if err != nil {
		webFail("Couldn't open weekly.csv", w, err)
		return
	}

	//create single data cluster
	statSlice := make([]SingleWeeklyStat, 0)

	//unmarshal the file:
	err = gocsv.UnmarshalFile(f, &statSlice)
	if err != nil {
		webFail("Failed to unmarshal weekly.csv", w, err)
		return
	}

	sort.Slice(statSlice, func(i, j int) bool {
		return statSlice[i].WeekEnding < statSlice[j].WeekEnding
	})

	//build profit:
	for k, v := range statSlice {
		gUSD := ToUSD(v.GI)
		eUSD := ToUSD(v.Expenses)
		pUSD := gUSD - eUSD
		statSlice[k].Profit = pUSD.Float64()
	}

	data := struct {
		Stats []SingleWeeklyStat
	}{statSlice}

	renderTemplate(w, r, name, data)

	return
}

func getWeeks(n int) []string {
	now.WeekStartDay = time.Friday
	var week = now.EndOfWeek()
	year, month, day := week.Date()
	nextThursday := time.Date(year, time.Month(month), day, 14, 0, 0, 0, time.UTC)

	var weeks []string
	if time.Now().Format("Monday") == "Thursday" {
		weeks = append(weeks, nextThursday.Add(time.Hour*24*7).Format("2006-01-02"))
	}
	weeks = append(weeks, nextThursday.Format("2006-01-02"))
	for i := 0; i < n; i++ {
		nextThursday = nextThursday.Add(time.Hour * -24 * 7)
		weeks = append(weeks, nextThursday.Format("2006-01-02"))
	}

	return weeks
}

func copyFile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}

type DailyStat struct {
	Name      string `csv:"Stats" json:"Name"`
	Thursday  string `csv:"Thursday" json:"Thursday"`
	Friday    string `csv:"Friday" json:"Friday"`
	Monday    string `csv:"Monday" json:"Monday"`
	Tuesday   string `csv:"Tuesday" json:"Tuesday"`
	Wednesday string `csv:"Wednesday" json:"Wednesday"`
	Quota     string `csv:"Quotas" json:"Quota"`
}

func RenderInputDaily(w http.ResponseWriter, r *http.Request, name string) {

	weeks := getWeeks(16)

	r.ParseForm()
	_, hasMyParam := r.Form["thisWeek"]
	thisWeek := ""
	index := "0"
	if hasMyParam {
		q := r.URL.Query()
		thisWeek = q.Get("thisWeek")
		//we assume that if we got thisWeek we should also have index...
		index = q.Get("index")
	}

	if thisWeek == "" {
		thisWeek = weeks[0]
	}

	err := checkIfValidWE(thisWeek)
	if err != nil {
		msg := fmt.Sprintf("WE is not a weekending: %s", thisWeek)
		webFail(msg, w, err)
		return
	}

	fileName := fmt.Sprintf("public/dailyStats/%s.csv", thisWeek)
	file, err := os.Open(fileName)
	if errors.Is(err, os.ErrNotExist) {
		// handle the case where the file doesn't exist
		_, err = copyFile("public/dailyStats/template.csv", fileName)
		if err != nil {
			msg := "failed to create copy of the template.csv file"
			webFail(msg, w, err)
			return
		}
		file, err = os.Open(fileName)
		if err != nil {
			msg := fmt.Sprintf("Couldn't open %s", fileName)
			webFail(msg, w, err)
			return
		}
	} else if err != nil {
		msg := fmt.Sprintf("couldn't open the file: %s", fileName)
		webFail(msg, w, err)
		return
	}

	statGrid := make([]DailyStat, 0)
	err = gocsv.UnmarshalFile(file, &statGrid)
	if err != nil {
		msg := "Failed to unmarshal csv file"
		webFail(msg, w, err)
		return
	}

	parsedDate, _ := time.Parse("2006-01-02", thisWeek)
	thisWeekFormatted := parsedDate.Format("2 Jan 2006")

	data := struct {
		Weeks       []string
		ThisWeek    string
		ThisWeekRaw string
		Stats       []DailyStat
		Index       string
	}{weeks, thisWeekFormatted, thisWeek, statGrid, index}

	renderTemplate(w, r, name, data)

	return
}

func RenderInputWeekly(w http.ResponseWriter, r *http.Request, name string) {

	weeks := getWeeks(16)

	data := struct {
		Weeks []string
	}{weeks}

	renderTemplate(w, r, name, data)

	return
}

func renderTemplate(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	// parse templates
	tpl := template.New("").Funcs(template.FuncMap{
		// "abs":            Abs,
		// "MultPercent":    MultPercent,
		// "CheckSymbolId":  CheckSymbolId,
		// "GetTickerPrice": GetTickerPrice,
		// "Mul":            SimpleMult,
		// "Sub":            SimpleSub,
		// "Div":            SimpleDiv,
		// "RunningTotal":   RunningTotal,
	})
	tpl, err := tpl.ParseGlob("templates/*.gohtml")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// execute page
	var bufBody, bufHeader, bufFooter bytes.Buffer
	err = tpl.ExecuteTemplate(&bufHeader, "header", data)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	err = tpl.ExecuteTemplate(&bufBody, name, data)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	err = tpl.ExecuteTemplate(&bufFooter, "footer", data)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// execute layout
	type Model struct {
		Header template.HTML
		Body   template.HTML
		// LoggedIn bool
		PageName template.HTML
		Footer   template.HTML
	}
	model := Model{
		PageName: template.HTML(`<link rel="stylesheet" type="text/css" href="/public/css/` + name + `.css">`),
		Header:   template.HTML(bufHeader.String()),
		Body:     template.HTML(bufBody.String()),
		Footer:   template.HTML(bufBody.String()),
	}

	err = tpl.ExecuteTemplate(w, "index", model)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	return

}

// ... (rest of the code: CreateLog, FileExists, DailyStat, validateDailyStats, StringToMoney, getWeeks, checkIfValidWE, webFail, handleClassifications, handleDivisions, handleStats, handleDailyStatsRequest, handleWeeklyStatsRequest, handleSave7R, handleLogWeeklyStats, handleSaveWeeklyEdit remain unchanged from artifact version 54392640-368e-42c1-b319-d3e4ba07984e)
// Creates the log.txt which will log activity (mostly used for error logging). This appends all new data to the file.
func CreateLog() *os.File {

	//CREATE ERROR LOG:
	f, err := os.OpenFile("ErrorLog.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("ERROR: error opening file: %v", err)
	}

	log.SetOutput(f)

	return f

}

// ---------- POST /services/logWeeklyStats ----------
// Minimal StatID-based single write endpoint. Accepts form-encoded or JSON:
// - form: stat_id=<id>&date=<YYYY-MM-DD>&value=<string>
// - JSON: { "stat_id": <id>, "date": "YYYY-MM-DD", "value": "<string>" }
func handleLogWeeklyStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Support JSON or form body
	var statID int
	var date string
	var value string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		var payload struct {
			StatID int    `json:"stat_id"`
			Date   string `json:"date"`
			Value  string `json:"value"`
		}
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			webFail("Failed to read request body", w, err)
			return
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			webFail("Failed to parse JSON", w, err)
			return
		}
		statID = payload.StatID
		date = payload.Date
		value = payload.Value
	} else {
		// form
		if err := r.ParseForm(); err != nil {
			webFail("Failed to parse form", w, err)
			return
		}
		statID, _ = strconv.Atoi(r.FormValue("stat_id"))
		date = r.FormValue("date")
		value = r.FormValue("value")
	}

	if statID == 0 {
		webFail("stat_id is required", w, fmt.Errorf("stat_id required"))
		return
	}
	if err := checkIfValidWE(date); err != nil {
		webFail("The weekending date is not valid or is not Thursday", w, err)
		return
	}

	// Resolve stat metadata
	var shortID, valueType, statType string
	if err := DB.QueryRow(`SELECT short_id, value_type, type FROM stats WHERE id = ? LIMIT 1`, statID).Scan(&shortID, &valueType, &statType); err != nil {
		if err == sql.ErrNoRows {
			webFail("Stat not found", w, err)
			return
		}
		webFail("Failed to query stat metadata", w, err)
		return
	}
	if statType != "personal" {
		webFail("Only personal stats can be written via this endpoint", w, fmt.Errorf("invalid stat scope"))
		return
	}

	// validate value according to valueType
	if err := validateWeeklyValueByType(value, valueType); err != nil {
		webFail("Invalid value", w, err)
		return
	}

	// convert to storage integer
	var storeVal int64
	switch valueType {
	case "currency":
		m, err := StringToMoney(value)
		if err != nil {
			webFail("Invalid currency", w, err)
			return
		}
		storeVal = int64(m.MoneyToUSD())
	case "number":
		i, err := strconv.Atoi(value)
		if err != nil {
			webFail("Invalid integer", w, err)
			return
		}
		storeVal = int64(i)
	case "percentage":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			webFail("Invalid percentage", w, err)
			return
		}
		storeVal = int64((f * 100) + 0.5)
	default:
		webFail("Unknown value type", w, fmt.Errorf("value_type=%s", valueType))
		return
	}

	// Insert: remove existing personal row for this stat/date/user then insert
	tx, err := DB.Begin()
	if err != nil {
		webFail("Failed to start transaction", w, err)
		return
	}
	sessionUserID := r.Context().Value("user_id").(int)

	if _, err := tx.Exec(`DELETE FROM weekly_stats WHERE LOWER(name)=? AND week_ending = ? AND user_id = ?`, strings.ToLower(shortID), date, sessionUserID); err != nil {
		tx.Rollback()
		webFail("Failed to clear existing weekly row", w, err)
		return
	}
	if _, err := tx.Exec(`INSERT INTO weekly_stats (name, week_ending, value, user_id) VALUES (?, ?, ?, ?)`, strings.ToLower(shortID), date, storeVal, sessionUserID); err != nil {
		tx.Rollback()
		webFail("Failed to insert weekly row", w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		webFail("Failed to commit weekly row", w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message":"Weekly value saved"}`)
}

// ---------- POST /services/saveWeeklyEdit ----------
// Strict StatID-based bulk upsert for personal weekly stats.
// Payload: JSON array of { StatID:int, Weekending:"YYYY-MM-DD", Value:"string" }
func handleSaveWeeklyEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		webFail("Failed to read request body", w, err)
		return
	}

	var payload []struct {
		StatID    int    `json:"StatID"`
		Weekending string `json:"Weekending"`
		Value     string `json:"Value"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		webFail("Failed to unmarshal payload", w, err)
		return
	}
	if len(payload) == 0 {
		webFail("Empty payload", w, fmt.Errorf("no rows provided"))
		return
	}

	// Validate all weekending dates first
	for _, row := range payload {
		if err := checkIfValidWE(row.Weekending); err != nil {
			webFail(fmt.Sprintf("W/E date %s invalid", row.Weekending), w, err)
			return
		}
	}

	tx, err := DB.Begin()
	if err != nil {
		webFail("Failed to start transaction", w, err)
		return
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	sessionUserID := r.Context().Value("user_id").(int)

	// Collect unique weekendings from payload to remove existing personal rows for those weeks
	weSet := make(map[string]struct{})
	for _, row := range payload {
		weSet[row.Weekending] = struct{}{}
	}
	weList := make([]interface{}, 0, len(weSet))
	for we := range weSet {
		weList = append(weList, we)
	}
	placeholders := strings.Repeat("?,", len(weList))
	placeholders = placeholders[:len(placeholders)-1]

	// Clear existing personal rows for these week endings
	if _, err := tx.Exec(fmt.Sprintf("DELETE FROM weekly_stats WHERE user_id = ? AND week_ending IN (%s)", placeholders), append([]interface{}{sessionUserID}, weList...)...); err != nil {
		tx.Rollback()
		webFail("Failed to clear personal weekly_stats", w, err)
		return
	}

	// Insert each payload row (only personal stats allowed)
	for _, row := range payload {
		// Resolve stat metadata by id
		var shortID, valueType, statType string
		if err := DB.QueryRow(`SELECT short_id, value_type, type FROM stats WHERE id = ? LIMIT 1`, row.StatID).Scan(&shortID, &valueType, &statType); err != nil {
			tx.Rollback()
			if err == sql.ErrNoRows {
				webFail(fmt.Sprintf("Stat not found for StatID %d", row.StatID), w, err)
				return
			}
			webFail("Failed to query stat metadata", w, err)
			return
		}
		if statType != "personal" {
			tx.Rollback()
			webFail(fmt.Sprintf("Stat %s (id=%d) is not personal and cannot be written via this endpoint", shortID, row.StatID), w, fmt.Errorf("invalid stat scope"))
			return
		}

		// validate value
		if err := validateWeeklyValueByType(row.Value, valueType); err != nil {
			tx.Rollback()
			webFail(fmt.Sprintf("Invalid value for stat %s: %v", shortID, err), w, err)
			return
		}

		// convert to stored integer
		var storeVal int64
		switch valueType {
		case "currency":
			if strings.TrimSpace(row.Value) == "" {
				continue
			}
			m, err := StringToMoney(row.Value)
			if err != nil {
				tx.Rollback()
				webFail("Invalid currency", w, err)
				return
			}
			storeVal = int64(m.MoneyToUSD())
		case "number":
			if strings.TrimSpace(row.Value) == "" {
				continue
			}
			i, err := strconv.Atoi(row.Value)
			if err != nil {
				tx.Rollback()
				webFail("Invalid integer", w, err)
				return
			}
			storeVal = int64(i)
		case "percentage":
			if strings.TrimSpace(row.Value) == "" {
				continue
			}
			f, err := strconv.ParseFloat(row.Value, 64)
			if err != nil {
				tx.Rollback()
				webFail("Invalid percentage", w, err)
				return
			}
			storeVal = int64((f * 100) + 0.5)
		default:
			tx.Rollback()
			webFail("Unknown value type", w, fmt.Errorf("value_type=%s", valueType))
			return
		}

		// Insert user-scoped weekly row
		if _, err := tx.Exec(`INSERT INTO weekly_stats (name, week_ending, value, user_id) VALUES (?, ?, ?, ?)`, strings.ToLower(shortID), row.Weekending, storeVal, sessionUserID); err != nil {
			tx.Rollback()
			webFail("Failed to insert weekly row", w, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		webFail("Failed to commit weekly edits", w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"message":"Saved Weekly stat data"}`)
}

// ---------- GET /services/getWeeklyStats (DB-backed), supports stat_id ----------
// func handleGetWeeklyStats(w http.ResponseWriter, r *http.Request) {
// 	q := r.URL.Query()
// 	statIDStr := q.Get("stat_id")
// 	statShort := q.Get("stat")
// 	if statIDStr == "" && statShort == "" {
// 		webFail("stat_id or stat query param required", w, errors.New("missing stat"))
// 		return
// 	}

// 	// Resolve
// 	nameLower, statType, err := resolveStatIdentity(statIDStr, statShort)
// 	if err != nil {
// 		webFail("Failed to resolve stat", w, err)
// 		return
// 	}

// 	sessionUserID := r.Context().Value("user_id").(int)

// 	var rows *sql.Rows
// 	if statType == "personal" {
// 		rows, err = DB.Query(`SELECT week_ending, value FROM weekly_stats WHERE LOWER(name)=? AND user_id = ? ORDER BY week_ending`, nameLower, sessionUserID)
// 	} else {
// 		rows, err = DB.Query(`SELECT week_ending, value FROM weekly_stats WHERE LOWER(name)=? AND division_id IS NOT NULL ORDER BY week_ending`, nameLower)
// 	}
// 	if err != nil {
// 		webFail("Failed to query weekly_stats", w, err)
// 		return
// 	}
// 	defer rows.Close()

// 	type WeeklyValue struct {
// 		WeekEnding string  `json:"Weekending"`
// 		Value      float64 `json:"Value"`
// 	}
// 	out := []WeeklyValue{}
// 	for rows.Next() {
// 		var we string
// 		var v int
// 		if err := rows.Scan(&we, &v); err != nil {
// 			webFail("Failed to scan weekly_stats", w, err)
// 			return
// 		}
// 		out = append(out, WeeklyValue{WeekEnding: we, Value: float64(v) / 100.0})
// 	}
// 	w.Header().Set("Content-Type", "application/json")
// 	json.NewEncoder(w).Encode(out)
// }

// GET /services/getWeeklyStats - now supports optional user_id (admin-only) to fetch another user's personal series.
func handleGetWeeklyStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	statIDStr := q.Get("stat_id")
	statShort := q.Get("stat")
	userIDParam := q.Get("user_id") // optional numeric user id when admin is viewing other users
	if statIDStr == "" && statShort == "" {
		webFail("stat_id or stat query param required", w, errors.New("missing stat"))
		return
	}

	// Resolve stat identity and metadata
	nameLower, statType, err := resolveStatIdentity(statIDStr, statShort)
	if err != nil {
		webFail("Failed to resolve stat", w, err)
		return
	}

	// session user id & role
	sessionUser := r.Context().Value("user_id").(int)
	sessionIsAdmin := false
	if role, ok := r.Context().Value("role").(string); ok {
		sessionIsAdmin = role == "admin"
	}

	// determine which user_id to use for personal stats
	targetUserID := sessionUser
	if userIDParam != "" {
		// only allow if session user is admin
		if !sessionIsAdmin {
			webFail("Insufficient permissions to request other user's stats", w, errors.New("forbidden"))
			return
		}
		if uid, convErr := strconv.Atoi(userIDParam); convErr == nil {
			targetUserID = uid
		} else {
			webFail("Invalid user_id parameter", w, convErr)
			return
		}
	}

	var rows *sql.Rows
	if statType == "personal" {
		rows, err = DB.Query(`SELECT week_ending, value FROM weekly_stats WHERE LOWER(name)=? AND user_id = ? ORDER BY week_ending`, nameLower, targetUserID)
	} else {
		rows, err = DB.Query(`SELECT week_ending, value FROM weekly_stats WHERE LOWER(name)=? AND division_id IS NOT NULL ORDER BY week_ending`, nameLower)
	}
	if err != nil {
		webFail("Failed to query weekly_stats", w, err)
		return
	}
	defer rows.Close()

	type WeeklyValue struct {
		WeekEnding string  `json:"Weekending"`
		Value      float64 `json:"Value"`
	}
	out := []WeeklyValue{}
	for rows.Next() {
		var we string
		var v int
		if err := rows.Scan(&we, &v); err != nil {
			webFail("Failed to scan weekly_stats", w, err)
			return
		}
		// Convert stored integer to formatted number. For currency we assume DB stores cents; handle formatting elsewhere if needed.
		out = append(out, WeeklyValue{WeekEnding: we, Value: float64(v) / 100.0})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// validateDailyStatByType validates the daily row fields according to value_type.
// valueType must be "currency", "number", or "percentage".
func validateDailyStatByType(name, valueType string, row DailyStat) error {
    // helper to build messages
    fieldErr := func(field, val, msg string) error {
        return fmt.Errorf("Value %v on %s for stat %s is invalid: %s", val, field, msg)
    }

    switch valueType {
    case "currency":
        // parse each day and quota with StringToMoney
        days := map[string]string{
            "Thursday":  row.Thursday,
            "Friday":    row.Friday,
            "Monday":    row.Monday,
            "Tuesday":   row.Tuesday,
            "Wednesday": row.Wednesday,
            "Quota":     row.Quota,
        }
        for field, val := range days {
            if val == "" {
                // allow empty values (means not entered)
                continue
            }
            if _, err := StringToMoney(val); err != nil {
                return fieldErr(field, val, "not a valid money value (use plain decimal e.g. 1234.56)")
            }
        }
        return nil

    case "number":
        days := map[string]string{
            "Thursday":  row.Thursday,
            "Friday":    row.Friday,
            "Monday":    row.Monday,
            "Tuesday":   row.Tuesday,
            "Wednesday": row.Wednesday,
            "Quota":     row.Quota,
        }
        for field, val := range days {
            if val == "" {
                continue
            }
            if _, err := strconv.Atoi(val); err != nil {
                return fieldErr(field, val, "not a valid integer")
            }
        }
        return nil

    case "percentage":
        days := map[string]string{
            "Thursday":  row.Thursday,
            "Friday":    row.Friday,
            "Monday":    row.Monday,
            "Tuesday":   row.Tuesday,
            "Wednesday": row.Wednesday,
            "Quota":     row.Quota,
        }
        for field, val := range days {
            if val == "" {
                continue
            }
            f, err := strconv.ParseFloat(val, 64)
            if err != nil {
                return fieldErr(field, val, "not a valid number")
            }
            // optional: enforce 0 <= f <= 100
            if f < 0 || f > 100 {
                return fieldErr(field, val, "percentage out of range 0-100")
            }
        }
        return nil

    default:
        return fmt.Errorf("Unknown value_type %s for stat %s", valueType, name)
    }
}

// validateWeeklyValueByType validates a single value string according to the stat's value_type.
func validateWeeklyValueByType(valueStr, valueType string) error {
	valueStr = strings.TrimSpace(valueStr)
	if valueStr == "" {
		return nil // empty allowed (means no value)
	}
	switch valueType {
	case "currency":
		if _, err := StringToMoney(valueStr); err != nil {
			return fmt.Errorf("invalid currency value: %v", err)
		}
		return nil
	case "number":
		if _, err := strconv.Atoi(valueStr); err != nil {
			return fmt.Errorf("invalid integer value: %v", err)
		}
		return nil
	case "percentage":
		if _, err := strconv.ParseFloat(valueStr, 64); err != nil {
			return fmt.Errorf("invalid percentage value: %v", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown value_type: %s", valueType)
	}
}