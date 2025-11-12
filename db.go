package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// DB is the global database handle used across the app.
var DB *sql.DB

// InitDB initializes the database schema for a clean start.
// Design decisions reflected here:
// - stats table contains canonical assignment: assigned_user_id and assigned_division_id.
// - weekly_stats and daily_stats reference stat_id (FK to stats.id) and store the value.
// - We keep an optional author_user_id on weekly_stats/daily_stats to record who wrote the row (audit/history).
//   This is NOT the "owner" of the stat; the canonical owner remains in stats.assigned_user_id.
// - We include optional explicit user_id/division_id on weekly_stats/daily_stats for explicit per-user or per-division
//   writes (these are the rows you might search for in special cases). Canonical rows are stored with user_id/division_id = NULL.
// - We keep stat_user_assignments and stat_division_assignments as optional history/compatibility tables.
func InitDB() {
	var err error
	DB, err = sql.Open("sqlite3", "./stats.db")
	if err != nil {
		log.Fatal(err)
	}

	// Enable foreign key enforcement in SQLite.
	if _, err := DB.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		log.Printf("warning: failed to set PRAGMA foreign_keys: %v", err)
	}

	// Create tables (idempotent)
	_, err = DB.Exec(`
	-- Companies
	CREATE TABLE IF NOT EXISTS companies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		company_id TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL
	);

	-- Users
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		company_id INTEGER NOT NULL,
		username TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL CHECK(role IN ('admin','user')),
		FOREIGN KEY (company_id) REFERENCES companies(id) ON DELETE CASCADE,
		UNIQUE(company_id, username)
	);

	-- Divisions
	CREATE TABLE IF NOT EXISTS divisions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL
	);

	-- Stats: canonical single-assignment fields for user and division
	CREATE TABLE IF NOT EXISTS stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		short_id TEXT NOT NULL,
		full_name TEXT NOT NULL,
		type TEXT NOT NULL CHECK(type IN ('personal','divisional','main')),
		value_type TEXT NOT NULL CHECK(value_type IN ('number','currency','percentage')),
		reversed BOOLEAN NOT NULL DEFAULT 0,
		assigned_user_id INTEGER,       -- canonical assigned user (nullable)
		assigned_division_id INTEGER,   -- canonical assigned division (nullable)
		is_calculated BOOLEAN NOT NULL DEFAULT 0,  -- true if this stat sums others
		FOREIGN KEY(assigned_user_id) REFERENCES users(id) ON DELETE SET NULL,
		FOREIGN KEY(assigned_division_id) REFERENCES divisions(id) ON DELETE SET NULL
	);

	-- New table for calculated stat relationships
	CREATE TABLE IF NOT EXISTS stat_calculations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		stat_id INTEGER NOT NULL,              -- the calculated stat (e.g., Total VSD)
		dependent_stat_id INTEGER NOT NULL,    -- a stat it depends on (e.g., Extinguisher VSD)
		FOREIGN KEY (stat_id) REFERENCES stats(id) ON DELETE CASCADE,
		FOREIGN KEY (dependent_stat_id) REFERENCES stats(id) ON DELETE CASCADE,
		UNIQUE(stat_id, dependent_stat_id)     -- prevent duplicate relationships
	);

	-- Optional historical assignment tables (compatibility)
	CREATE TABLE IF NOT EXISTS stat_user_assignments (
		stat_id INTEGER,
		user_id INTEGER,
		PRIMARY KEY (stat_id, user_id),
		FOREIGN KEY (stat_id) REFERENCES stats(id) ON DELETE CASCADE,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS stat_division_assignments (
		stat_id INTEGER,
		division_id INTEGER,
		PRIMARY KEY (stat_id, division_id),
		FOREIGN KEY (stat_id) REFERENCES stats(id) ON DELETE CASCADE,
		FOREIGN KEY (division_id) REFERENCES divisions(id) ON DELETE CASCADE
	);

	-- Daily stats: reference stat_id, store date/value.
	-- author_user_id records who wrote the row (audit) but does not change canonical assignment.
	CREATE TABLE IF NOT EXISTS daily_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		stat_id INTEGER NOT NULL,
		date TEXT NOT NULL,
		value INTEGER NOT NULL,
		author_user_id INTEGER, -- who wrote this row (nullable)
		FOREIGN KEY (stat_id) REFERENCES stats(id) ON DELETE CASCADE,
		FOREIGN KEY (author_user_id) REFERENCES users(id)
	);

	-- Weekly stats: reference stat_id and week_ending.
	-- If user_id/division_id are NULL this is the canonical stat row (ownership inferred from stats.assigned_*).
	-- author_user_id is the writer (audit).
	-- Create weekly_stats table with only the columns you requested
	CREATE TABLE IF NOT EXISTS weekly_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		stat_id INTEGER NOT NULL,
		week_ending TEXT NOT NULL,
		value INTEGER NOT NULL,
		author_user_id INTEGER,   -- who wrote this row (nullable)
		FOREIGN KEY (stat_id) REFERENCES stats(id) ON DELETE CASCADE,
		FOREIGN KEY (author_user_id) REFERENCES users(id)
	);

	-- Enforce a single canonical row per (stat_id, week_ending)
	CREATE UNIQUE INDEX IF NOT EXISTS uniq_weekly_stat_week ON weekly_stats(stat_id, week_ending);

	`)
	if err != nil {
		log.Fatalf("failed to create tables: %v", err)
	}

	// Log init complete
	log.Println("DB initialized (clean schema): stats, weekly_stats, daily_stats, assignments, users, divisions")
}

// RegisterCompany creates a company and its admin user
func RegisterCompany(companyID, companyName, adminUsername, adminPassword string) error {
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %v", err)
	}

	// Insert company
	res, err := tx.Exec(`
		INSERT INTO companies (company_id, name)
		VALUES (?, ?)
	`, companyID, companyName)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to insert company: %v", err)
	}

	companyDBID, err := res.LastInsertId()
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to get company ID: %v", err)
	}

	// Hash admin password
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to hash password: %v", err)
	}

	// Insert admin user
	_, err = tx.Exec(`
		INSERT INTO users (company_id, username, password_hash, role)
		VALUES (?, ?, ?, 'admin')
	`, companyDBID, adminUsername, hash)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to insert admin user: %v", err)
	}

	return tx.Commit()
}

// RegisterUser adds a new user to an existing company
func RegisterUser(companyID, username, password, role string) error {
	// Validate role
	if role != "admin" && role != "user" && role != "manager" {
		return fmt.Errorf("invalid role: %s", role)
	}

	// Get company database ID
	var companyDBID int
	err := DB.QueryRow("SELECT id FROM companies WHERE company_id = ?", companyID).Scan(&companyDBID)
	if err != nil {
		return fmt.Errorf("company not found: %v", err)
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %v", err)
	}

	// Insert user
	_, err = DB.Exec(`
		INSERT INTO users (company_id, username, password_hash, role)
		VALUES (?, ?, ?, ?)
	`, companyDBID, username, hash, role)
	if err != nil {
		return fmt.Errorf("failed to insert user: %v", err)
	}
	return nil
}