package main

import (
	"bufio"
	"bytes"
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

// AuthMiddleware checks authentication and optionally role
func AuthMiddleware(requireRole string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := store.Get(r, "session-name")
		if err != nil {
			log.Printf("Session error: %v", err)
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		userID, ok := session.Values["user_id"].(int)
		if !ok || userID == 0 {
			log.Printf("No user_id in session for %s", r.URL.Path)
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		// Validate user and role
		var companyID string
		var username, role string
		err = DB.QueryRow("SELECT c.company_id, u.username, u.role FROM users u JOIN companies c ON u.company_id = c.id WHERE u.id = ?", userID).Scan(&companyID, &username, &role)
		if err != nil {
			log.Printf("User not found for id %d: %v", userID, err)
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		if requireRole != "" && role != requireRole {
			log.Printf("User %s (role %s) not authorized for %s (requires %s)", username, role, r.URL.Path, requireRole)
			http.Error(w, `{"message": "Unauthorized"}`, http.StatusForbidden)
			return
		}

		log.Printf("Authenticated user %s (company %s, role %s) accessing %s", username, companyID, role, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// UserInfoHandler returns the current user's information
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

	response := map[string]string{
		"company_id": companyID,
		"username":   username,
		"role":       role,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

func main() {
	f := CreateLog()
	defer f.Close()

	InitDB() // From db.go

	// Initialize session store (use a secure key in production)
	store = sessions.NewCookieStore([]byte("super-secret-key"))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   3600 * 8, // 8 hours
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
	}

	// Create router
	router := mux.NewRouter()

	// CORS middleware
	corsMiddleware := handlers.CORS(
		handlers.AllowedOrigins([]string{"http://localhost:3000"}),
		handlers.AllowedMethods([]string{"GET", "POST", "OPTIONS"}),
		handlers.AllowedHeaders([]string{"Content-Type"}),
		handlers.AllowCredentials(),
	)

	// Protected API endpoints
	// router.Handle("/api/classifications", AuthMiddleware("", http.HandlerFunc(handleClassifications)))
	// router.Handle("/api/divisions", AuthMiddleware("", http.HandlerFunc(handleDivisions)))
	// router.Handle("/api/stats", AuthMiddleware("", http.HandlerFunc(handleStats)))
	router.Handle("/services/getWeeklyStats", AuthMiddleware("", http.HandlerFunc(handleWeeklyStatsRequest)))
	router.Handle("/services/getDailyStats", AuthMiddleware("", http.HandlerFunc(handleDailyStatsRequest)))
	router.Handle("/services/save7R", AuthMiddleware("", http.HandlerFunc(handleSave7R)))
	router.Handle("/services/saveWeeklyEdit", AuthMiddleware("", http.HandlerFunc(handleSaveWeeklyEdit)))
	router.Handle("/services/logWeeklyStats", AuthMiddleware("", http.HandlerFunc(handleLogWeeklyStats)))

	// Admin-only endpoint
	router.Handle("/users", AuthMiddleware("admin", http.HandlerFunc(UserHandler)))

	// User info endpoint
	router.Handle("/api/user", AuthMiddleware("", http.HandlerFunc(UserInfoHandler)))

	// Auth endpoints (unprotected)
	router.HandleFunc("/login", LoginHandler)
	router.HandleFunc("/logout", LogoutHandler)
	router.HandleFunc("/register", RegisterHandler)

	// Static file handlers
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

	// Serve React app
	router.PathPrefix("/").HandlerFunc(handleIndex)

	// Apply CORS middleware to all routes
	http.Handle("/", corsMiddleware(router))

	port := ":9090"
	fmt.Printf("Running Stat HQ on %s\n", port)
	log.Fatal(http.ListenAndServe(port, nil))
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

func handleSave7R(w http.ResponseWriter, r *http.Request) {

	statGrid := make([]DailyStat, 3)

	err := json.NewDecoder(r.Body).Decode(&statGrid)
	if err != nil {
		msg := "Failed to decode body"
		webFail(msg, w, err)
		return
	}

	fmt.Println(statGrid)

	for _, v := range statGrid {
		err = validateDailyStats(v)
		if err != nil {
			webFail("", w, err)
			return
		}
	}

	q := r.URL.Query()
	thisWeek := q.Get("thisWeek")

	err = checkIfValidWE(thisWeek)
	if err != nil {
		msg := "The weekending date is not valid or is not Thursday"
		webFail(msg, w, err)
		return
	}

	fileName := fmt.Sprintf("public/dailyStats/%s.csv", thisWeek)

	err = os.Remove(fileName)
	if err != nil {
		msg := "Failed to delete a file which makes it impossible to save"
		webFail(msg, w, err)
		return
	}

	file, err := os.Create(fileName)
	if err != nil {
		msg := fmt.Sprintf("Failed to create file %s", fileName)
		webFail(msg, w, err)
		return
	}

	err = gocsv.MarshalFile(statGrid, file)
	if err != nil {
		msg := "Failed to marshal the file"
		webFail(msg, w, err)
		return
	}

	msg := "Saved 7R grid"
	io.WriteString(w, msg)

	return
}

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

func handleLogWeeklyStats(w http.ResponseWriter, r *http.Request) {

	// get stat values from the submitted form
	date := r.FormValue("date")
	err := checkIfValidWE(date)
	if err != nil {
		msg := "The weekending date is not valid or is not Thursday"
		webFail(msg, w, err)
		return
	}

	vsd := r.FormValue("vsd")
	vsdMoney, err := StringToMoney(vsd)
	if err != nil || vsd == "" {
		msg := "The VSD value entered does not appear to be a valid number or you didn't enter anything. Please do not use any other symbols other than a decimal."
		webFail(msg, w, err)
		return
	}
	vsdPennies := vsdMoney.MoneyToUSD()

	gi := r.FormValue("gi")
	giMoney, err := StringToMoney(gi)
	if err != nil || gi == "" {
		msg := "The GI value entered does not appear to be a valid number or you didn't enter anything. Please do not use any other symbols other than a decimal."
		webFail(msg, w, err)
		return
	}
	giPennies := giMoney.MoneyToUSD()

	sites := r.FormValue("sites")
	_, err = strconv.Atoi(sites)
	if err != nil || sites == "" {
		msg := "The Job Sites value entered does not appear to be a valid number or you didn't enter anything."
		webFail(msg, w, err)
		return
	}

	expenses := r.FormValue("expenses")
	expensesMoney, err := StringToMoney(expenses)
	if err != nil || expenses == "" {
		msg := "The Expenses value entered does not appear to be a valid number or you didn't enter anything. Please do not use any other symbols other than a decimal."
		webFail(msg, w, err)
		return
	}
	expensesPennies := expensesMoney.MoneyToUSD()

	scheduled := r.FormValue("scheduled")
	_, err = strconv.Atoi(sites)
	if err != nil || scheduled == "" {
		msg := "The Job Sites value entered does not appear to be a valid number or you didn't enter anything."
		webFail(msg, w, err)
		return
	}

	outstanding := r.FormValue("outstanding")
	outstandingMoney, err := StringToMoney(outstanding)
	if err != nil || outstanding == "" {
		msg := "The Outstanding value entered does not appear to be a valid number or you didn't enter anything. Please do not use any other symbols other than a decimal."
		webFail(msg, w, err)
		return
	}
	outstandingPennies := outstandingMoney.MoneyToUSD()

	b, err := FileExists("public/stats/weekly.csv")
	if err != nil {
		msg := "Couldn't verify if weekly.csv exists"
		webFail(msg, w, err)
		return
	}

	if !b {
		_, err := os.Create("public/stats/weekly.csv")
		if err != nil {
			msg := "Failed to create weekly.csv file"
			webFail(msg, w, err)
			return
		}

		_, err = copyFile("public/stats/template.csv", "public/stats/weekly.csv")
		if err != nil {
			webFail("Failed to copy template to weekly.csv", w, err)
			return
		}
	}

	f, err := os.OpenFile("public/stats/weekly.csv", os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("ERROR: error opening file: %v", err)
	}
	defer f.Close()

	line := fmt.Sprintf("%v,%v,%v,%v,%v,%v,%v\n", date, gi, vsd, expenses, scheduled, sites, outstanding)
	_, err = io.WriteString(f, line)
	if err != nil {
		webFail("Failed to write to weekly.csv", w, err)
		return
	}

	// err = errors.New("Custom error message")
	// webFail("testing a failed", w, err, nil)
	msg := fmt.Sprintf("The following data was logged:\r\nGI: %v\r\nVSD: %v\r\nJobs: %v\r\nExpenses: %v\r\nScheduled: %v\r\nOutstanding Collections: %v\r\n", giPennies.String(), vsdPennies.String(), sites, expensesPennies.String(), scheduled, outstandingPennies.String())
	io.WriteString(w, msg)

	return
}

// webFail will print to the standard logger as well as stdout the msg string. The arg 'data' will be appended
// to show the data that caused the error and the error will also be printed out to these.
// This call http.Error and sends only the msg back to the client.
func webFail(msg string, w http.ResponseWriter, err error, data ...interface{}) {
	fullMsg := msg
	if err != nil {
		fullMsg += ": " + err.Error()
	}
	log.Println(msg, " : ", data, " with error: ", err)
	fmt.Println(msg, " : ", data, " with error: ", err)
	http.Error(w, fullMsg, 500)
	return
}

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

func handleSaveWeeklyEdit(w http.ResponseWriter, r *http.Request) {

	statGrid := make([]SingleWeeklyStat, 0)

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		webFail("Failed to read r.Body", w, err)
		return
	}

	err = json.Unmarshal(body, &statGrid)
	if err != nil {
		webFail("Failed to unmarshal body", w, err)
		return
	}

	//verify all data:
	for _, v := range statGrid {
		date := strings.TrimSpace(v.WeekEnding)
		err = checkIfValidWE(date)
		if err != nil {
			msg := fmt.Sprintf("W/E date %s invalid", v.WeekEnding)
			webFail(msg, w, err)
			return
		}
	}

	err = os.Remove("public/stats/weekly.csv")
	if err != nil {
		webFail("Failed to remove weekly.csv as part of update. The update did not take place", w, err)
		return
	}

	f, err := os.Create("public/stats/weekly.csv")
	if err != nil {
		webFail("Failed to create weekly.csv", w, err)
		return
	}

	err = gocsv.MarshalFile(statGrid, f)
	if err != nil {
		webFail("Failed to marshal statGrid into weekly.csv", w, err)
		return
	}

	io.WriteString(w, "Saved Weekly stat data")

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