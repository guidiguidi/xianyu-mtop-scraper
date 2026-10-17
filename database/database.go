package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Monitor represents an active scraping keyword alert task.
type Monitor struct {
	ID          int64
	UserID      int64
	Keyword     string
	PriceMin    *int
	PriceMax    *int
	IntervalSec int
	IsActive    int
	CreatedAt   string
}

// Database represents the SQLite database connection manager.
type Database struct {
	dbPath string
	db     *sql.DB
}

// NewDatabase initializes a new Database instance.
func NewDatabase(dbPath string) *Database {
	return &Database{
		dbPath: dbPath,
	}
}

// Connect opens a connection to the SQLite database.
func (d *Database) Connect() error {
	db, err := sql.Open("sqlite3", d.dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings to avoid busy locks in concurrent environments
	db.SetMaxOpenConns(1)
	d.db = db
	return nil
}

// Close closes the database connection.
func (d *Database) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// Initialize creates the necessary schema tables if they do not exist.
func (d *Database) Initialize() error {
	if d.db == nil {
		if err := d.Connect(); err != nil {
			return err
		}
	}

	// 1. Create Monitors table
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS monitors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			keyword TEXT NOT NULL,
			price_min INTEGER,
			price_max INTEGER,
			interval_sec INTEGER DEFAULT 300,
			is_active INTEGER DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create monitors table: %w", err)
	}

	// 2. Create Seen Items table
	_, err = d.db.Exec(`
		CREATE TABLE IF NOT EXISTS seen_items (
			item_id TEXT NOT NULL,
			monitor_id INTEGER NOT NULL,
			first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (item_id, monitor_id),
			FOREIGN KEY (monitor_id) REFERENCES monitors (id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create seen_items table: %w", err)
	}

	// 3. Create System Config table
	_, err = d.db.Exec(`
		CREATE TABLE IF NOT EXISTS system_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create system_config table: %w", err)
	}

	log.Printf("[Database] Database initialized and schema verified successfully at %s", d.dbPath)
	return nil
}

// --- Cookies Management ---

// GetCookies retrieves the persisted cookies string from system_config.
func (d *Database) GetCookies(defaultValue string) (string, error) {
	var cookies string
	err := d.db.QueryRow("SELECT value FROM system_config WHERE key = 'cookies'").Scan(&cookies)
	if err != nil {
		if err == sql.ErrNoRows {
			return defaultValue, nil
		}
		return "", fmt.Errorf("failed to query cookies: %w", err)
	}
	return cookies, nil
}

// SaveCookies saves or updates the cookies string in system_config.
func (d *Database) SaveCookies(cookiesStr string) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO system_config (key, value) VALUES ('cookies', ?)
	`, cookiesStr)
	if err != nil {
		return fmt.Errorf("failed to save cookies: %w", err)
	}
	return nil
}

// --- Monitors CRUD ---

// AddMonitor inserts a new keyword monitoring task.
func (d *Database) AddMonitor(userID int64, keyword string, priceMin *int, priceMax *int, intervalSec int) (int64, error) {
	result, err := d.db.Exec(`
		INSERT INTO monitors (user_id, keyword, price_min, price_max, interval_sec) 
		VALUES (?, ?, ?, ?, ?)
	`, userID, keyword, priceMin, priceMax, intervalSec)
	if err != nil {
		return 0, fmt.Errorf("failed to insert monitor: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert ID: %w", err)
	}
	return id, nil
}

// GetActiveMonitors fetches all monitors that are currently active.
func (d *Database) GetActiveMonitors() ([]Monitor, error) {
	rows, err := d.db.Query("SELECT id, user_id, keyword, price_min, price_max, interval_sec, is_active, created_at FROM monitors WHERE is_active = 1")
	if err != nil {
		return nil, fmt.Errorf("failed to query active monitors: %w", err)
	}
	defer rows.Close()

	var monitors []Monitor
	for rows.Next() {
		var m Monitor
		var priceMinVal, priceMaxVal sql.NullInt64
		err := rows.Scan(&m.ID, &m.UserID, &m.Keyword, &priceMinVal, &priceMaxVal, &m.IntervalSec, &m.IsActive, &m.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan active monitor row: %w", err)
		}

		if priceMinVal.Valid {
			v := int(priceMinVal.Int64)
			m.PriceMin = &v
		}
		if priceMaxVal.Valid {
			v := int(priceMaxVal.Int64)
			m.PriceMax = &v
		}

		monitors = append(monitors, m)
	}
	return monitors, nil
}

// GetUserMonitors fetches all monitoring tasks for a specific user ID.
func (d *Database) GetUserMonitors(userID int64) ([]Monitor, error) {
	rows, err := d.db.Query("SELECT id, user_id, keyword, price_min, price_max, interval_sec, is_active, created_at FROM monitors WHERE user_id = ?", userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query user monitors: %w", err)
	}
	defer rows.Close()

	var monitors []Monitor
	for rows.Next() {
		var m Monitor
		var priceMinVal, priceMaxVal sql.NullInt64
		err := rows.Scan(&m.ID, &m.UserID, &m.Keyword, &priceMinVal, &priceMaxVal, &m.IntervalSec, &m.IsActive, &m.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user monitor row: %w", err)
		}

		if priceMinVal.Valid {
			v := int(priceMinVal.Int64)
			m.PriceMin = &v
		}
		if priceMaxVal.Valid {
			v := int(priceMaxVal.Int64)
			m.PriceMax = &v
		}

		monitors = append(monitors, m)
	}
	return monitors, nil
}

// DeleteMonitor removes a monitoring task only if it belongs to the given user.
func (d *Database) DeleteMonitor(monitorID int64, userID int64) (bool, error) {
	res, err := d.db.Exec("DELETE FROM monitors WHERE id = ? AND user_id = ?", monitorID, userID)
	if err != nil {
		return false, fmt.Errorf("failed to delete monitor: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

// --- Seen Items Management ---

// IsItemSeen checks if a product ID was already processed and notified for a given monitor ID.
func (d *Database) IsItemSeen(itemID string, monitorID int64) (bool, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(1) FROM seen_items WHERE item_id = ? AND monitor_id = ?", itemID, monitorID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to query seen items: %w", err)
	}
	return count > 0, nil
}

// AddSeenItem caches a processed product item ID to prevent duplicate notifications.
func (d *Database) AddSeenItem(itemID string, monitorID int64) error {
	_, err := d.db.Exec(`
		INSERT OR IGNORE INTO seen_items (item_id, monitor_id) VALUES (?, ?)
	`, itemID, monitorID)
	if err != nil {
		return fmt.Errorf("failed to insert seen item: %w", err)
	}
	return nil
}

// CleanOldSeenItems removes cached items older than the specified number of days to manage database size.
func (d *Database) CleanOldSeenItems(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
	_, err := d.db.Exec("DELETE FROM seen_items WHERE first_seen < ?", cutoff)
	if err != nil {
		return fmt.Errorf("failed to clean stale seen items: %w", err)
	}
	return nil
}
