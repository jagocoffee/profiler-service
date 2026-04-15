package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func Init(dbPath string) error {
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS profiles (
		id INTEGER PRIMARY KEY,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		run_timestamp TIMESTAMP NOT NULL UNIQUE,
		window TEXT NOT NULL,
		sample_num INTEGER NOT NULL,
		cpu_profile_url TEXT,
		heap_profile_url TEXT,
		cpu_text_url TEXT,
		heap_text_url TEXT,
		summary TEXT,
		anomalies TEXT,
		metrics TEXT,
		yday_comparison TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_run_timestamp ON profiles(run_timestamp);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	log.Printf("SQLite initialized: %s", dbPath)
	return nil
}

type ProfileRecord struct {
	ID             int       `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	RunTimestamp   time.Time `json:"run_timestamp"`
	Window         string    `json:"window"`
	SampleNum      int       `json:"sample_num"`
	CPUProfileURL  string    `json:"cpu_profile_url"`
	HeapProfileURL string    `json:"heap_profile_url"`
	CPUTextURL     string    `json:"cpu_text_url"`
	HeapTextURL    string    `json:"heap_text_url"`
	Summary        string    `json:"summary"`
	Anomalies      string    `json:"anomalies"`
	Metrics        string    `json:"metrics"`
	YdayComparison string    `json:"yday_comparison"`
}

func SaveProfile(rec *ProfileRecord) error {
	query := `
	INSERT INTO profiles (run_timestamp, window, sample_num, cpu_profile_url, heap_profile_url,
		cpu_text_url, heap_text_url, summary, anomalies, metrics, yday_comparison)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.Exec(query,
		rec.RunTimestamp, rec.Window, rec.SampleNum,
		rec.CPUProfileURL, rec.HeapProfileURL,
		rec.CPUTextURL, rec.HeapTextURL,
		rec.Summary, rec.Anomalies, rec.Metrics, rec.YdayComparison)
	if err != nil {
		return fmt.Errorf("insert profile: %w", err)
	}
	log.Printf("Saved profile: %s sample %d", rec.Window, rec.SampleNum)
	return nil
}

func GetProfileByTimestamp(timestamp time.Time) (*ProfileRecord, error) {
	query := `
	SELECT id, created_at, run_timestamp, window, sample_num, cpu_profile_url, heap_profile_url,
		cpu_text_url, heap_text_url, summary, anomalies, metrics, yday_comparison
	FROM profiles
	WHERE run_timestamp = ?
	`

	var rec ProfileRecord
	err := db.QueryRow(query, timestamp).Scan(
		&rec.ID, &rec.CreatedAt, &rec.RunTimestamp, &rec.Window, &rec.SampleNum,
		&rec.CPUProfileURL, &rec.HeapProfileURL, &rec.CPUTextURL, &rec.HeapTextURL,
		&rec.Summary, &rec.Anomalies, &rec.Metrics, &rec.YdayComparison,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query by timestamp: %w", err)
	}

	return &rec, nil
}

func Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}
