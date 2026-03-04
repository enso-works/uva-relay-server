package testutil

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// NewTestDB creates an in-memory SQLite database with the relay schema.
func NewTestDB() *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(err)
	}

	initSchema(db)
	return db
}

func initSchema(db *sql.DB) {
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
			panic(err)
		}
	}
}
