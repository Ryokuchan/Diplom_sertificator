package db

import (
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func Init() {
	var err error
	DB, err = sql.Open("sqlite3", "./data/diplomas.db")
	if err != nil {
		log.Fatal("failed to open db:", err)
	}

	migrate()
}

func migrate() {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS upload_jobs (
			id          TEXT PRIMARY KEY,
			filename    TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'pending',
			error       TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS diplomas (
			hash           TEXT PRIMARY KEY,
			full_name      TEXT NOT NULL,
			diploma_number TEXT NOT NULL,
			university     TEXT NOT NULL,
			degree         TEXT NOT NULL,
			date           TEXT NOT NULL,
			upload_job_id  TEXT NOT NULL,
			created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, q := range queries {
		if _, err := DB.Exec(q); err != nil {
			log.Fatal("migration error:", err)
		}
	}
}
