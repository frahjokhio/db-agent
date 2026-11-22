// Package storage opens the agent's own SQLite file (.db-agent/agent.db).
// It is separate from the app's business database.
package storage

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure Go SQLite: no C compiler, works in a single binary
)

const migrations = `
CREATE TABLE IF NOT EXISTS schema_snapshots (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	database TEXT NOT NULL,
	fingerprint TEXT NOT NULL,
	captured_at TEXT NOT NULL,
	schema_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS query_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at TEXT NOT NULL,
	user TEXT NOT NULL,
	source TEXT NOT NULL,
	sql TEXT NOT NULL,
	status TEXT NOT NULL,
	duration_ms INTEGER,
	row_count INTEGER,
	error TEXT
);`

// Open creates the folder and tables if needed.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: one writer at a time keeps it simple and safe
	if _, err := db.Exec(migrations); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
