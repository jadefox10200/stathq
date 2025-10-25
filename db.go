package main

import (
	"database/sql"
	"log"

	"golang.org/x/crypto/bcrypt"

	_ "github.com/mattn/go-sqlite3"
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
            name TEXT UNIQUE NOT NULL
        );

        -- Users (with company_id)
        CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            company_id INTEGER NOT NULL,
            username TEXT UNIQUE NOT NULL,
            password_hash TEXT NOT NULL,
            FOREIGN KEY (company_id) REFERENCES companies(id)
        );

        -- Classifications (e.g., Main, GDS, Personal, custom)
        CREATE TABLE IF NOT EXISTS classifications (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT UNIQUE NOT NULL
        );

        -- Divisions (e.g., Sales, Marketing)
        CREATE TABLE IF NOT EXISTS divisions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            company_id INTEGER NOT NULL,
            name TEXT UNIQUE NOT NULL,
            FOREIGN KEY (company_id) REFERENCES companies(id)
        );

        -- Stats (dynamic, per company)
        CREATE TABLE IF NOT EXISTS stats (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            company_id INTEGER NOT NULL,
            name TEXT UNIQUE NOT NULL,
            classification_id INTEGER NOT NULL,
            division_id INTEGER,  -- Nullable, required for GDS
            unit TEXT NOT NULL CHECK (unit IN ('money', 'number', 'percentage')),
            upside_down BOOLEAN NOT NULL DEFAULT 0,
            FOREIGN KEY (company_id) REFERENCES companies(id)
            FOREIGN KEY (classification_id) REFERENCES classifications(id),
            FOREIGN KEY (division_id) REFERENCES divisions(id)
        );

        -- Weekly stats entries (per stat, per week, per company)
        CREATE TABLE IF NOT EXISTS weekly_stats (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            week_ending DATE NOT NULL,
            stat_id INTEGER NOT NULL,
            value REAL NOT NULL,
            UNIQUE (week_ending, stat_id),
            FOREIGN KEY (stat_id) REFERENCES stats(id)
        );

        -- Daily stats entries (per stat, per week, per company)
        CREATE TABLE IF NOT EXISTS daily_stats (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            week_ending DATE NOT NULL,
            stat_id INTEGER NOT NULL,
            thursday TEXT,
            friday TEXT,
            monday TEXT,
            tuesday TEXT,
            wednesday TEXT,
            quota TEXT,
            UNIQUE (week_ending, stat_id),
            FOREIGN KEY (stat_id) REFERENCES stats(id)
        );
    `)
    if err != nil {
        log.Fatal(err)
    }

    // Seed defaults
    seedDefaults()
}

func seedDefaults() {
    defaults := []string{"Main", "GDS", "Personal"}
    for _, name := range defaults {
        _, err := DB.Exec("INSERT OR IGNORE INTO classifications (name) VALUES (?)", name)
        if err != nil {
            log.Println("Seeding error:", err)
        }
    }

    // Seed test company and user (for development)
    _, err := DB.Exec("INSERT OR IGNORE INTO companies (name) VALUES (?)", "TestCompany")
    if err != nil {
        log.Println("Seeding company error:", err)
    }
    var companyID int
    DB.QueryRow("SELECT id FROM companies WHERE name = ?", "TestCompany").Scan(&companyID)
    hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
    _, err = DB.Exec("INSERT OR IGNORE INTO users (company_id, username, password_hash) VALUES (?, ?, ?)", companyID, "admin", hash)
    if err != nil {
        log.Println("Seeding user error:", err)
    }
}