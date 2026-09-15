// Package storage owns the SQLite handle, pragmas and migrations.
//
// Reliability contract (docs/ARCHITECTURE.md):
//   - database lives on a local disk path, never on a network share
//   - foreign_keys ON, busy_timeout, WAL, synchronous=FULL for durability
//   - short write transactions, no network calls inside transactions
package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open creates the data directory and opens the SQLite database with
// finance-grade pragmas.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "xiaozhang.db")

	// busy_timeout: wait instead of failing on lock contention.
	// foreign_keys: enforce cross-entity integrity.
	// journal_mode WAL + synchronous FULL: crash-safe persistence.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is single-writer; keep one connection so transactions are
	// serialized at the driver level and WAL is not treated as unlimited
	// concurrent write capacity.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// SQLiteVersion reports the embedded SQLite version for verification
// (spec 13.2 requires checking the actual embedded version).
func SQLiteVersion(db *sql.DB) (string, error) {
	var v string
	err := db.QueryRow("SELECT sqlite_version()").Scan(&v)
	return v, err
}
