package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

var DB *sql.DB

func InitDB() {
	var err error
	DB, err = sql.Open("sqlite3", "./stats.db")
	if err != nil {
		log.Fatal(err)
	}

	// Create tables
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
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('admin', 'user')),
			FOREIGN KEY (company_id) REFERENCES companies(id)
		);

		-- Divisions
		CREATE TABLE IF NOT EXISTS divisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL
		);

		-- === STATS TABLE (only one!) ===
		CREATE TABLE IF NOT EXISTS stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			short_id TEXT NOT NULL,           -- e.g. "GI"
			full_name TEXT NOT NULL,                 -- e.g. "Gross Income"
			type TEXT NOT NULL CHECK(type IN ('personal','divisional','main')),
			value_type TEXT NOT NULL CHECK(value_type IN ('number','currency','percentage')),
			reversed BOOLEAN NOT NULL DEFAULT 0
		);

		-- Stat → User assignments
		CREATE TABLE IF NOT EXISTS stat_user_assignments (
			stat_id INTEGER,
			user_id INTEGER,
			PRIMARY KEY (stat_id, user_id),
			FOREIGN KEY(stat_id) REFERENCES stats(id) ON DELETE CASCADE,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		);

		-- Stat → Division assignments
		CREATE TABLE IF NOT EXISTS stat_division_assignments (
			stat_id INTEGER,
			division_id INTEGER,
			PRIMARY KEY (stat_id, division_id),
			FOREIGN KEY(stat_id) REFERENCES stats(id) ON DELETE CASCADE,
			FOREIGN KEY(division_id) REFERENCES divisions(id) ON DELETE CASCADE
		);

		-- Daily stats (user_id nullable; division_id nullable)
		CREATE TABLE IF NOT EXISTS daily_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			date TEXT NOT NULL,
			value INTEGER NOT NULL,
			division_id INTEGER,
			user_id INTEGER,
			FOREIGN KEY (division_id) REFERENCES divisions(id),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		-- Weekly stats (user_id nullable; division_id nullable)
		CREATE TABLE IF NOT EXISTS weekly_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			week_ending TEXT NOT NULL,
			value INTEGER NOT NULL,
			division_id INTEGER,
			user_id INTEGER,
			FOREIGN KEY (division_id) REFERENCES divisions(id),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
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