package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// Open the existing database
	db, err := sql.Open("sqlite3", "./stats.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Enable foreign keys
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		log.Printf("warning: failed to set PRAGMA foreign_keys: %v", err)
	}

	// Alter the stats table to add is_calculated column
	_, err = db.Exec(`ALTER TABLE stats ADD COLUMN is_calculated BOOLEAN NOT NULL DEFAULT 0`)
	if err != nil {
		log.Printf("Failed to add is_calculated column (might already exist): %v", err)
	} else {
		fmt.Println("Added is_calculated column to stats table")
	}

	// Create the stat_calculations table (idempotent)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS stat_calculations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			stat_id INTEGER NOT NULL,
			dependent_stat_id INTEGER NOT NULL,
			FOREIGN KEY (stat_id) REFERENCES stats(id) ON DELETE CASCADE,
			FOREIGN KEY (dependent_stat_id) REFERENCES stats(id) ON DELETE CASCADE,
			UNIQUE(stat_id, dependent_stat_id)
		)
	`)
	if err != nil {
		log.Fatal("Failed to create stat_calculations table:", err)
	} else {
		fmt.Println("Created stat_calculations table")
	}

	fmt.Println("Migration completed successfully")
}