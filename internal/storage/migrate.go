package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies versioned migrations in order. Migrations never drop and
// recreate production data; each file is NNNN_name.sql applied in one
// transaction and recorded in schema_migrations.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") && !strings.HasPrefix(e.Name(), "._") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read migration version: %w", err)
	}

	for _, name := range names {
		version, err := parseVersion(name)
		if err != nil {
			return err
		}
		if version <= current {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		// A migration may opt out of the wrapping transaction with a
		// `-- +no_tx` first line (needed for SQLite table rebuilds that
		// toggle foreign_keys, which is a no-op inside a transaction).
		// Such files must manage their own BEGIN/COMMIT.
		if strings.HasPrefix(string(body), "-- +no_tx") {
			if _, err := db.Exec(string(body)); err != nil {
				return fmt.Errorf("apply migration %s: %w", name, err)
			}
			if _, err := db.Exec(`INSERT INTO schema_migrations(version,name) VALUES(?,?)`, version, name); err != nil {
				return fmt.Errorf("record migration %s: %w", name, err)
			}
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,name) VALUES(?,?)`, version, name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func parseVersion(name string) (int, error) {
	i := strings.Index(name, "_")
	if i <= 0 {
		return 0, fmt.Errorf("migration filename must be NNNN_name.sql, got %q", name)
	}
	v, err := strconv.Atoi(name[:i])
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("invalid migration version in %q", name)
	}
	return v, nil
}

// MigrationVersion reports the highest applied migration version.
func MigrationVersion(db *sql.DB) (int, error) {
	var v int
	err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&v)
	return v, err
}
