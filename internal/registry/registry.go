package registry

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anthropics/uvame-relay/internal/protocol"
	_ "modernc.org/sqlite"
)

type UsernameRecord struct {
	Username     string
	InstallID    string
	RegisteredAt string
	LastSeenAt   string
}

type Registry struct {
	db *sql.DB
	mu sync.Mutex
}

// New creates a registry backed by a SQLite database at the given data directory.
func New(dataDir string) (*Registry, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "relay.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &Registry{db: db}, nil
}

// NewFromDB creates a registry from an existing database connection (for testing).
func NewFromDB(db *sql.DB) *Registry {
	return &Registry{db: db}
}

func initSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS usernames (
			username TEXT PRIMARY KEY,
			install_id TEXT NOT NULL,
			registered_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usernames_last_seen
		ON usernames(last_seen_at)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) CanRegister(username, installID string) bool {
	if !protocol.IsValidUsername(username) {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var existingID string
	err := r.db.QueryRow("SELECT install_id FROM usernames WHERE username = ?", username).Scan(&existingID)
	if err == sql.ErrNoRows {
		return true
	}
	if err != nil {
		return false
	}
	return existingID == installID
}

func (r *Registry) Register(username, installID string) bool {
	if !protocol.IsValidUsername(username) {
		return false
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	r.mu.Lock()
	defer r.mu.Unlock()

	var existingID string
	err := r.db.QueryRow("SELECT install_id FROM usernames WHERE username = ?", username).Scan(&existingID)
	if err == nil {
		if existingID != installID {
			return false
		}
		_, _ = r.db.Exec("UPDATE usernames SET last_seen_at = ? WHERE username = ?", now, username)
		return true
	}

	_, err = r.db.Exec(
		"INSERT INTO usernames (username, install_id, registered_at, last_seen_at) VALUES (?, ?, ?, ?)",
		username, installID, now, now,
	)
	return err == nil
}

func (r *Registry) IsRegistered(username string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	var dummy int
	err := r.db.QueryRow("SELECT 1 FROM usernames WHERE username = ?", username).Scan(&dummy)
	return err == nil
}

func (r *Registry) UpdateLastSeen(username string) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.db.Exec("UPDATE usernames SET last_seen_at = ? WHERE username = ?", now, username)
}

func (r *Registry) Get(username string) *UsernameRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	var rec UsernameRecord
	err := r.db.QueryRow("SELECT username, install_id, registered_at, last_seen_at FROM usernames WHERE username = ?", username).
		Scan(&rec.Username, &rec.InstallID, &rec.RegisteredAt, &rec.LastSeenAt)
	if err != nil {
		return nil
	}
	return &rec
}

func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	var count int
	_ = r.db.QueryRow("SELECT COUNT(*) FROM usernames").Scan(&count)
	return count
}

func (r *Registry) ReclaimInactive(days int) int {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)

	r.mu.Lock()
	defer r.mu.Unlock()

	result, err := r.db.Exec("DELETE FROM usernames WHERE last_seen_at < ?", cutoff)
	if err != nil {
		return 0
	}
	n, _ := result.RowsAffected()
	return int(n)
}

func (r *Registry) Delete(username string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	result, err := r.db.Exec("DELETE FROM usernames WHERE username = ?", username)
	if err != nil {
		return false
	}
	n, _ := result.RowsAffected()
	return n > 0
}

func (r *Registry) List() []UsernameRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	rows, err := r.db.Query("SELECT username, install_id, registered_at, last_seen_at FROM usernames ORDER BY username")
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var records []UsernameRecord
	for rows.Next() {
		var rec UsernameRecord
		if err := rows.Scan(&rec.Username, &rec.InstallID, &rec.RegisteredAt, &rec.LastSeenAt); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records
}

func (r *Registry) Close() error {
	return r.db.Close()
}
