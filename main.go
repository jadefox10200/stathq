package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

// Updated AuthMiddleware: put username and role into request context so handlers
// (e.g., handleGetWeeklyStats) can check role without extra DB lookups.
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
		ctx = context.WithValue(ctx, "username", username)
		ctx = context.WithValue(ctx, "role", role) // <-- added so handlers can check role from context
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
			s.assigned_user_id,
			u.username,
			s.assigned_division_id,
			d.name AS division_name
		FROM stats s
		LEFT JOIN users u ON s.assigned_user_id = u.id
		LEFT JOIN divisions d ON s.assigned_division_id = d.id
		WHERE s.assigned_user_id = ? OR s.id IN (SELECT stat_id FROM stat_user_assignments WHERE user_id = ?)
		ORDER BY s.short_id
	`, uid, uid)
	if err != nil {
		webFail("Failed to query assigned stats", w, err)
		return
	}
	defer rows.Close()

	type statOut struct {
		ID               int     `json:"id"`
		ShortID          string  `json:"short_id"`
		FullName         string  `json:"full_name"`
		Type             string  `json:"type"`
		ValueType        string  `json:"value_type"`
		Reversed         bool    `json:"reversed"`
		AssignedUserID   *int    `json:"user_id,omitempty"`
		AssignedUsername *string `json:"username,omitempty"`
		AssignedDivision *int    `json:"division_id,omitempty"`
		AssignedDivName  *string `json:"division_name,omitempty"`
	}
	out := []statOut{}
	for rows.Next() {
		var s statOut
		var assignedUID sqlNullInt64
		var assignedUsername sqlNullString
		var assignedDiv sqlNullInt64
		var divName sqlNullString
		if err := rows.Scan(&s.ID, &s.ShortID, &s.FullName, &s.Type, &s.ValueType, &s.Reversed,
			&assignedUID, &assignedUsername, &assignedDiv, &divName); err != nil {
			webFail("Failed to scan assigned stat row", w, err)
			return
		}
		if assignedUID.Valid {
			v := int(assignedUID.Int64)
			s.AssignedUserID = &v
		}
		if assignedUsername.Valid {
			u := assignedUsername.String
			s.AssignedUsername = &u
		}
		if assignedDiv.Valid {
			v := int(assignedDiv.Int64)
			s.AssignedDivision = &v
		}
		if divName.Valid {
			dn := divName.String
			s.AssignedDivName = &dn
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		webFail("Error iterating assigned stats", w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
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
	var userName, nameLower, statType, valueType string
	id, err := strconv.Atoi(statIDStr)
	if err != nil {
		webFail("Invalid stat_id", w, err)
		return
	}
	if err := DB.QueryRow(`SELECT s.short_id, s.type, u.username, s.value_type FROM stats s LEFT JOIN users u on s.assigned_user_id = u.id WHERE s.id = ? LIMIT 1`, id).Scan(&nameLower, &statType, &userName, &valueType); err != nil {
		if err == sql.ErrNoRows {
			webFail("Stat not found", w, err)
			return
		}
		webFail("Failed to query stat", w, err)
		return
	}
	nameLower = strings.ToLower(nameLower)

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

	for day, dateStr := range dates {
		// Query the stored integer value (assumed stored as integer; for currency it's cents)
		var v sql.NullInt64
		var err error
		err = DB.QueryRow(`SELECT value FROM daily_stats WHERE stat_id=? AND date=? LIMIT 1`, statIDStr, dateStr).Scan(&v)
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

		// Only support personal writes via this endpoint (safer). Reject divisional/main writes here.
		if statType != "personal" {
			tx.Rollback()
			webFail(fmt.Sprintf("Stat %s (id=%d) is not a personal stat and cannot be written via this endpoint", shortID, row.StatID), w, errors.New("invalid stat scope"))
			return
		}

		// Delete existing rows for this user and stat name for the week
		if _, err := tx.Exec(`DELETE FROM daily_stats WHERE stat_id=? AND date IN (?,?,?,?,?)`, row.StatID, dates["Thursday"], dates["Friday"], dates["Monday"], dates["Tuesday"], dates["Wednesday"]); err != nil {
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
			if _, err := tx.Exec(`INSERT INTO daily_stats (stat_id, date, value) VALUES (?, ?, ?)`, row.StatID, dateStr, valueInt); err != nil {
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
		handlers.AllowedOrigins([]string{"https://stat-hq.com", "http://localhost:3000"}),  // Add production domain
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
	router.Handle("/api/divisions/{id}", AuthMiddleware("admin", http.HandlerFunc(DeleteDivisionHandler))).Methods("DELETE")
	router.Handle("/api/divisions/{id}", AuthMiddleware("admin", http.HandlerFunc(UpdateDivisionHandler))).Methods("PATCH")
	router.Handle("/api/divisions", AuthMiddleware("", http.HandlerFunc(ListDivisionsHandler))).Methods("GET")
	router.Handle("/api/users", AuthMiddleware("", http.HandlerFunc(ListUsersHandler))).Methods("GET")
	router.Handle("/api/stats/{id}/series", AuthMiddleware("", http.HandlerFunc(GetStatSeriesHandler))).Methods("GET")
	router.Handle("/api/stats/view/all", AuthMiddleware("", http.HandlerFunc(ListAllStatsHandler))).Methods("GET")
	
	router.Handle("/api/public/stats/{id}/series", AuthMiddleware("", http.HandlerFunc(PublicGetStatSeriesHandler))).Methods("GET")
	router.Handle("/api/public/stats/view/all", AuthMiddleware("", http.HandlerFunc(PublicListAllStatsHandler))).Methods("GET")

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
	// Add after your other API routes:)

	router.Handle("/api/divisions", AuthMiddleware("admin", http.HandlerFunc(CreateDivisionHandler))).Methods("POST")
	// User info endpoint
	router.Handle("/api/user", AuthMiddleware("", http.HandlerFunc(UserInfoHandler)))

	// Change password endpoint (for any authenticated user)
	router.Handle("/api/change-password", AuthMiddleware("", http.HandlerFunc(ChangePasswordHandler)))

	// Auth endpoints (unprotected)
	router.HandleFunc("/login", LoginHandler)
	router.HandleFunc("/logout", LogoutHandler)
	// router.HandleFunc("/register", RegisterHandler)

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

// ---------- CREATE STAT ----------
func CreateStatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ShortID     string `json:"short_id"`
		FullName    string `json:"full_name"`
		Type        string `json:"type"`
		ValueType   string `json:"value_type"`
		Reversed    bool   `json:"reversed"`
		UserIDs     []int  `json:"user_ids"`     // compatibility: we accept array but use the first element
		DivisionIDs []int  `json:"division_ids"` // compatibility: accept array, use first
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webFail("Invalid JSON payload", w, err)
		return
	}

	// Validation
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

	res, err := tx.Exec(`
		INSERT INTO stats (short_id, full_name, type, value_type, reversed, assigned_user_id, assigned_division_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, req.ShortID, req.FullName, req.Type, req.ValueType, req.Reversed,
		nullIntPtr(req.UserIDs),
		nullIntPtr(req.DivisionIDs),
	)
	if err != nil {
		tx.Rollback()
		webFail("Failed to insert stat", w, err)
		return
	}
	statID, err := res.LastInsertId()
	if err != nil {
		tx.Rollback()
		webFail("Failed to get last insert id", w, err)
		return
	}

	// Keep compatibility: populate stat_user_assignments / stat_division_assignments
	for _, uid := range req.UserIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO stat_user_assignments (stat_id, user_id) VALUES (?, ?)`, statID, uid); err != nil {
			tx.Rollback()
			webFail("Failed to populate stat_user_assignments", w, err)
			return
		}
	}
	for _, did := range req.DivisionIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO stat_division_assignments (stat_id, division_id) VALUES (?, ?)`, statID, did); err != nil {
			tx.Rollback()
			webFail("Failed to populate stat_division_assignments", w, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		webFail("Failed to commit", w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "Stat created"})
}

// Helper: return first element pointer or nil
func nullIntPtr(arr []int) interface{} {
	if len(arr) > 0 {
		return arr[0]
	}
	return nil
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webFail("Invalid JSON payload", w, err)
		return
	}

	if strings.TrimSpace(req.ShortID) == "" || strings.TrimSpace(req.FullName) == "" {
		webFail("Short ID and Full Name are required", w, nil)
		return
	}
	req.ShortID = strings.ToUpper(strings.TrimSpace(req.ShortID))
	req.FullName = strings.TrimSpace(req.FullName)

	tx, err := DB.Begin()
	if err != nil {
		webFail("Failed to start transaction", w, err)
		return
	}

	_, err = tx.Exec(`UPDATE stats SET short_id=?, full_name=?, type=?, value_type=?, reversed=?, assigned_user_id=?, assigned_division_id=? WHERE id = ?`,
		req.ShortID, req.FullName, req.Type, req.ValueType, req.Reversed,
		nullIntPtr(req.UserIDs), nullIntPtr(req.DivisionIDs), id)
	if err != nil {
		tx.Rollback()
		webFail("Failed to update stat", w, err)
		return
	}

	// Rebuild assignment tables for compatibility
	if _, err := tx.Exec(`DELETE FROM stat_user_assignments WHERE stat_id = ?`, id); err != nil {
		tx.Rollback()
		webFail("Failed to clear stat_user_assignments", w, err)
		return
	}
	for _, uid := range req.UserIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO stat_user_assignments (stat_id, user_id) VALUES (?, ?)`, id, uid); err != nil {
			tx.Rollback()
			webFail("Failed to insert stat_user_assignment", w, err)
			return
		}
	}

	if _, err := tx.Exec(`DELETE FROM stat_division_assignments WHERE stat_id = ?`, id); err != nil {
		tx.Rollback()
		webFail("Failed to clear stat_division_assignments", w, err)
		return
	}
	for _, did := range req.DivisionIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO stat_division_assignments (stat_id, division_id) VALUES (?, ?)`, id, did); err != nil {
			tx.Rollback()
			webFail("Failed to insert stat_division_assignment", w, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		webFail("Failed to commit update", w, err)
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
			s.assigned_user_id,
			u.username,
			s.assigned_division_id,
			d.name AS division_name
		FROM stats s
		LEFT JOIN users u ON s.assigned_user_id = u.id
		LEFT JOIN divisions d ON s.assigned_division_id = d.id
		ORDER BY u.username, s.type
	`)
	if err != nil {
		webFail("Failed to query stats", w, err)
		return
	}
	defer rows.Close()

	type statOut struct {
		ID                int     `json:"id"`
		ShortID           string  `json:"short_id"`
		FullName          string  `json:"full_name"`
		Type              string  `json:"type"`
		ValueType         string  `json:"value_type"`
		Reversed          bool    `json:"reversed"`
		AssignedUserID    *int    `json:"user_id,omitempty"`
		AssignedUsername  *string `json:"username,omitempty"`
		AssignedDivision  *int    `json:"division_id,omitempty"`
		AssignedDivName   *string `json:"division_name,omitempty"`
	}
	out := []statOut{}
	for rows.Next() {
		var s statOut
		var assignedUID sqlNullInt64
		var assignedUsername sqlNullString
		var assignedDiv sqlNullInt64
		var divName sqlNullString
		if err := rows.Scan(&s.ID, &s.ShortID, &s.FullName, &s.Type, &s.ValueType, &s.Reversed,
			&assignedUID, &assignedUsername, &assignedDiv, &divName); err != nil {
			webFail("Failed to scan stat row", w, err)
			return
		}
		if assignedUID.Valid {
			v := int(assignedUID.Int64)
			s.AssignedUserID = &v
		}
		if assignedUsername.Valid {
			u := assignedUsername.String
			s.AssignedUsername = &u
		}
		if assignedDiv.Valid {
			v := int(assignedDiv.Int64)
			s.AssignedDivision = &v
		}
		if divName.Valid {
			dn := divName.String
			s.AssignedDivName = &dn
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		webFail("Error iterating stats", w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
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
	    if r.Method == http.MethodGet {
        handleIndex(w, r)  // Serve the React app for GET requests
        return
    }
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
	if r.Method == http.MethodGet {
        handleIndex(w, r)  // Serve the React app for GET requests
        return
    }
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
func handleLogWeeklyStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ct := r.Header.Get("Content-Type")
	var payload struct {
		StatID int    `json:"stat_id"`
		Date   string `json:"date"`
		Value  string `json:"value"`
		// These are accepted only for intent: if admin wants to reassign the stat permanently,
		// they should call UpdateStatHandler instead. We'll ignore these for matching.
		UserID *int `json:"user_id,omitempty"`
		DivID  *int `json:"division_id,omitempty"`
	}
	if strings.HasPrefix(ct, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			webFail("Failed to parse JSON", w, err)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			webFail("Failed to parse form", w, err)
			return
		}
		payload.StatID, _ = strconv.Atoi(r.FormValue("stat_id"))
		payload.Date = r.FormValue("date")
		payload.Value = r.FormValue("value")
		// parse but do not use for matching
		if v := r.FormValue("user_id"); v != "" {
			if id, err := strconv.Atoi(v); err == nil {
				payload.UserID = &id
			}
		}
		if v := r.FormValue("division_id"); v != "" {
			if id, err := strconv.Atoi(v); err == nil {
				payload.DivID = &id
			}
		}
	}

	if payload.StatID == 0 {
		webFail("stat_id is required", w, fmt.Errorf("stat_id required"))
		return
	}
	if err := checkIfValidWE(payload.Date); err != nil {
		webFail("Invalid weekending date", w, err)
		return
	}

	// get session user id for audit
	sessUID := r.Context().Value("user_id")
	var authorID interface{} = nil
	if sessUID != nil {
		authorID = sessUID
	}

	// Resolve stat type and value_type for validation
	var statType, valueType string
	if err := DB.QueryRow(`SELECT type, value_type FROM stats WHERE id = ? LIMIT 1`, payload.StatID).Scan(&statType, &valueType); err != nil {
		if err == sql.ErrNoRows {
			webFail("Stat not found", w, err)
			return
		}
		webFail("Failed to query stat metadata", w, err)
		return
	}

	// validate and convert the provided value into storage form
	if err := validateWeeklyValueByType(payload.Value, valueType); err != nil {
		webFail("Invalid value", w, err)
		return
	}

	var storeVal int64
	switch valueType {
	case "currency":
		m, err := StringToMoney(payload.Value)
		if err != nil {
			webFail("Invalid currency", w, err)
			return
		}
		storeVal = int64(m.MoneyToUSD())
	case "number":
		i, err := strconv.Atoi(strings.TrimSpace(payload.Value))
		if err != nil {
			webFail("Invalid integer", w, err)
			return
		}
		storeVal = int64(i)
	case "percentage":
		f, err := strconv.ParseFloat(strings.TrimSpace(payload.Value), 64)
		if err != nil {
			webFail("Invalid percentage", w, err)
			return
		}
		storeVal = int64((f * 100) + 0.5)
	default:
		webFail("Unknown value type", w, fmt.Errorf("value_type=%s", valueType))
		return
	}

	// Upsert by stat_id + week_ending (single canonical row)
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

	var existingID int64
	err = tx.QueryRow(`SELECT id FROM weekly_stats WHERE stat_id = ? AND week_ending = ? LIMIT 1`, payload.StatID, payload.Date).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		tx.Rollback()
		webFail("Failed to query weekly_stats", w, err)
		return
	}

	if err == nil {
		// update existing single canonical row
		if _, err = tx.Exec(`UPDATE weekly_stats SET value = ?, author_user_id = ? WHERE id = ?`, storeVal, authorID, existingID); err != nil {
			tx.Rollback()
			webFail("Failed to update weekly_stats", w, err)
			return
		}
	} else {
		// insert new canonical row (we do NOT set user_id/division_id here)
		if _, err = tx.Exec(`INSERT INTO weekly_stats (stat_id, week_ending, value, author_user_id) VALUES (?, ?, ?, ?)`, payload.StatID, payload.Date, storeVal, authorID); err != nil {
			tx.Rollback()
			webFail("Failed to insert weekly_stats", w, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		webFail("Failed to commit weekly_stats", w, err)
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

// GET /services/getWeeklyStats - now supports optional user_id (admin-only) to fetch another user's personal series.
func handleGetWeeklyStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	statIDStr := q.Get("stat_id")
	if statIDStr == "" {
		webFail("stat_id is required", w, errors.New("missing stat_id"))
		return
	}
	statID, err := strconv.Atoi(statIDStr)
	if err != nil {
		webFail("Invalid stat_id", w, err)
		return
	}

	// Resolve stat and value_type
	var statType, valueType string
	if err := DB.QueryRow(`SELECT type, value_type FROM stats WHERE id = ? LIMIT 1`, statID).Scan(&statType, &valueType); err != nil {
		if err == sql.ErrNoRows {
			webFail("Stat not found", w, err)
			return
		}
		webFail("Failed to query stat metadata", w, err)
		return
	}

	type WeeklyValue struct {
		WeekEnding   string `json:"Weekending"`
		Value        float64 `json:"Value"`
		AuthorUserID *int   `json:"author_user_id,omitempty"`
	}

	out := []WeeklyValue{}

	rows, err := DB.Query(`
		SELECT week_ending, value, author_user_id
		FROM weekly_stats
		WHERE stat_id = ?
		ORDER BY week_ending
	`, statID)
	if err != nil {
		webFail("Failed to query weekly_stats", w, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var we string
		var v int64
		var author sql.NullInt64
		if err := rows.Scan(&we, &v, &author); err != nil {
			webFail("Failed to scan weekly_stats", w, err)
			return
		}
		var val float64
		switch valueType {
		case "currency":
			val = float64(v) / 100.0
		case "number":
			val = float64(v)
		case "percentage":
			val = float64(v) / 100.0
		default:
			val = float64(v) / 100.0
		}
		var auth *int
		if author.Valid {
			t := int(author.Int64)
			auth = &t
		}
		out = append(out, WeeklyValue{WeekEnding: we, Value: val, AuthorUserID: auth})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// Note: sqlNullInt64 and sqlNullString helpers to avoid importing database/sql types widely in this file.
type sqlNullInt64 struct {
	sql.NullInt64
}

type sqlNullString struct {
	sql.NullString
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

// GetStatSeriesHandler returns time series for a stat.
// Route: GET /api/stats/{id}/series?view=weekly[&user_id=...]
// Currently implements only view=weekly and returns JSON:
// [{ "Weekending":"YYYY-MM-DD", "Value": <number>, "author_user_id": <int|null> }, ...]
func GetStatSeriesHandler(w http.ResponseWriter, r *http.Request) {
	// require auth (router will wrap via AuthMiddleware)
	vars := mux.Vars(r)
	idStr := vars["id"]
	if idStr == "" {
		http.Error(w, `{"message":"stat id required"}`, http.StatusBadRequest)
		return
	}
	statID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"message":"invalid stat id"}`, http.StatusBadRequest)
		return
	}

	// view param (only weekly supported now)
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "weekly"
	}
	if view != "weekly" {
		http.Error(w, `{"message":"only 'weekly' view is implemented"}`, http.StatusNotImplemented)
		return
	}

	// (optional) allow admin to pass user_id for future per-user logic (ignored now)
	userIDParam := r.URL.Query().Get("user_id")
	if userIDParam != "" {
		// You can validate admin here if you want to restrict; currently we just accept and ignore.
		if _, err := strconv.Atoi(userIDParam); err != nil {
			http.Error(w, `{"message":"invalid user_id"}`, http.StatusBadRequest)
			return
		}
	}

	// get stat value_type for conversion
	var valueType string
	if err := DB.QueryRow(`SELECT value_type FROM stats WHERE id = ? LIMIT 1`, statID).Scan(&valueType); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"message":"stat not found"}`, http.StatusNotFound)
			return
		}
		webFail("Failed to query stat metadata", w, err)
		return
	}

	// Query canonical weekly rows for the stat
	rows, err := DB.Query(`SELECT week_ending, value, author_user_id FROM weekly_stats WHERE stat_id = ? ORDER BY week_ending`, statID)
	if err != nil {
		webFail("Failed to query weekly series", w, err)
		return
	}
	defer rows.Close()

	type seriesRow struct {
		Weekending   string   `json:"Weekending"`
		Value        float64  `json:"Value"`
		AuthorUserID *int     `json:"author_user_id,omitempty"`
	}

	out := make([]seriesRow, 0)
	for rows.Next() {
		var we string
		var v sql.NullInt64
		var author sql.NullInt64
		if err := rows.Scan(&we, &v, &author); err != nil {
			webFail("Failed to scan weekly row", w, err)
			return
		}
		if !v.Valid {
			// skip null values (shouldn't happen for weekly_stats)
			continue
		}

		var value float64
		switch valueType {
		case "currency":
			// stored as cents -> return dollars float
			value = float64(v.Int64) / 100.0
		case "number":
			value = float64(v.Int64)
		case "percentage":
			// stored as percent * 100 (e.g., 1234 -> 12.34)
			value = float64(v.Int64) / 100.0
		default:
			value = float64(v.Int64)
		}

		var au *int
		if author.Valid {
			t := int(author.Int64)
			au = &t
		}
		out = append(out, seriesRow{Weekending: we, Value: value, AuthorUserID: au})
	}
	if err := rows.Err(); err != nil {
		webFail("Error iterating series rows", w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ---------- UPDATE DIVISION ----------
func UpdateDivisionHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPatch {
        http.Error(w, `{"message":"Method not allowed"}`, http.StatusMethodNotAllowed)
        return
    }

    idStr := mux.Vars(r)["id"]
    id, err := strconv.Atoi(idStr)
    if err != nil {
        webFail("Invalid division ID", w, err)
        return
    }

    var req struct {
        Name string `json:"name"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        webFail("Invalid JSON payload", w, err)
        return
    }

    if strings.TrimSpace(req.Name) == "" {
        webFail("Division name is required", w, nil)
        return
    }

    _, err = DB.Exec(`UPDATE divisions SET name=? WHERE id = ?`, req.Name, id)
    if err != nil {
        webFail("Failed to update division", w, err)
        return
    }

    json.NewEncoder(w).Encode(map[string]string{"message": "Division updated"})
}

// ---------- PUBLIC LIST ALL STATS (divisional only for Home.js) ----------
func PublicListAllStatsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := DB.Query(`
		SELECT 
			s.id,
			s.short_id,
			s.full_name,
			s.type,
			s.value_type,
			s.reversed,
			s.assigned_user_id,
			u.username,
			s.assigned_division_id,
			d.name AS division_name
		FROM stats s
		LEFT JOIN users u ON s.assigned_user_id = u.id
		LEFT JOIN divisions d ON s.assigned_division_id = d.id
		WHERE s.type = 'divisional'
		ORDER BY s.short_id
	`)
	if err != nil {
		webFail("Failed to query stats", w, err)
		return
	}
	defer rows.Close()

	type statOut struct {
		ID                int     `json:"id"`
		ShortID           string  `json:"short_id"`
		FullName          string  `json:"full_name"`
		Type              string  `json:"type"`
		ValueType         string  `json:"value_type"`
		Reversed          bool    `json:"reversed"`
		AssignedUserID    *int    `json:"user_id,omitempty"`
		AssignedUsername  *string `json:"username,omitempty"`
		AssignedDivision  *int    `json:"division_id,omitempty"`
		AssignedDivName   *string `json:"division_name,omitempty"`
	}
	out := []statOut{}
	for rows.Next() {
		var s statOut
		var assignedUID sqlNullInt64
		var assignedUsername sqlNullString
		var assignedDiv sqlNullInt64
		var divName sqlNullString
		if err := rows.Scan(&s.ID, &s.ShortID, &s.FullName, &s.Type, &s.ValueType, &s.Reversed,
			&assignedUID, &assignedUsername, &assignedDiv, &divName); err != nil {
			webFail("Failed to scan stat row", w, err)
			return
		}
		if assignedUID.Valid {
			v := int(assignedUID.Int64)
			s.AssignedUserID = &v
		}
		if assignedUsername.Valid {
			u := assignedUsername.String
			s.AssignedUsername = &u
		}
		if assignedDiv.Valid {
			v := int(assignedDiv.Int64)
			s.AssignedDivision = &v
		}
		if divName.Valid {
			dn := divName.String
			s.AssignedDivName = &dn
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		webFail("Error iterating stats", w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ---------- PUBLIC GET STAT SERIES ----------
func PublicGetStatSeriesHandler(w http.ResponseWriter, r *http.Request) {
	// require auth (router will wrap via AuthMiddleware)
	vars := mux.Vars(r)
	idStr := vars["id"]
	if idStr == "" {
		http.Error(w, `{"message":"stat id required"}`, http.StatusBadRequest)
		return
	}
	statID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"message":"invalid stat id"}`, http.StatusBadRequest)
		return
	}

	// view param (only weekly supported now)
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "weekly"
	}
	if view != "weekly" {
		http.Error(w, `{"message":"only 'weekly' view is implemented"}`, http.StatusNotImplemented)
		return
	}

	// get stat value_type for conversion
	var valueType string
	if err := DB.QueryRow(`SELECT value_type FROM stats WHERE id = ? LIMIT 1`, statID).Scan(&valueType); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"message":"stat not found"}`, http.StatusNotFound)
			return
		}
		webFail("Failed to query stat metadata", w, err)
		return
	}

	// Query canonical weekly rows for the stat
	rows, err := DB.Query(`SELECT week_ending, value, author_user_id FROM weekly_stats WHERE stat_id = ? ORDER BY week_ending`, statID)
	if err != nil {
		webFail("Failed to query weekly series", w, err)
		return
	}
	defer rows.Close()

	type seriesRow struct {
		Weekending   string   `json:"Weekending"`
		Value        float64  `json:"Value"`
		AuthorUserID *int     `json:"author_user_id,omitempty"`
	}

	out := make([]seriesRow, 0)
	for rows.Next() {
		var we string
		var v sql.NullInt64
		var author sql.NullInt64
		if err := rows.Scan(&we, &v, &author); err != nil {
			webFail("Failed to scan weekly row", w, err)
			return
		}
		if !v.Valid {
			// skip null values (shouldn't happen for weekly_stats)
			continue
		}

		var value float64
		switch valueType {
		case "currency":
			// stored as cents -> return dollars float
			value = float64(v.Int64) / 100.0
		case "number":
			value = float64(v.Int64)
		case "percentage":
			// stored as percent * 100 (e.g., 1234 -> 12.34)
			value = float64(v.Int64) / 100.0
		default:
			value = float64(v.Int64)
		}

		var au *int
		if author.Valid {
			t := int(author.Int64)
			au = &t
		}
		out = append(out, seriesRow{Weekending: we, Value: value, AuthorUserID: au})
	}
	if err := rows.Err(); err != nil {
		webFail("Error iterating series rows", w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}