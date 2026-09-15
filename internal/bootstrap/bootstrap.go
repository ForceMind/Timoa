// Package bootstrap provisions the first administrator and default
// ledger via a server-local command — never through public registration.
package bootstrap

import (
	"database/sql"
	"errors"
	"fmt"

	"xiaozhang/internal/auth"
	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
)

// InitAdmin creates the first admin user + default ledger + membership
// and seeds core categories. It refuses to run when an admin already
// exists, so a public first-comer can never claim the instance.
func InitAdmin(db *sql.DB, username, displayName, password, ledgerName string) error {
	if username == "" || password == "" {
		return errors.New("username and password are required")
	}
	var admins int
	if err := db.QueryRow(`SELECT COUNT(1) FROM ledger_members WHERE role='admin'`).Scan(&admins); err != nil {
		return err
	}
	if admins > 0 {
		return errors.New("an administrator already exists; use the password recovery command instead")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	userID := ids.New()
	if displayName == "" {
		displayName = username
	}
	if _, err := tx.Exec(`INSERT INTO users(id,username,display_name,password_hash) VALUES(?,?,?,?)`,
		userID, username, displayName, hash); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if ledgerName == "" {
		ledgerName = "家庭账本"
	}
	ledgerID := ids.New()
	if _, err := tx.Exec(`INSERT INTO ledgers(id,name) VALUES(?,?)`, ledgerID, ledgerName); err != nil {
		return fmt.Errorf("create ledger: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO ledger_members(ledger_id,user_id,role) VALUES(?,?,'admin')`,
		ledgerID, userID); err != nil {
		return fmt.Errorf("create membership: %w", err)
	}
	if err := ledger.SeedCoreCategories(tx, ledgerID); err != nil {
		return fmt.Errorf("seed categories: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO audit_log(id,ledger_id,actor_user_id,action,entity_type,entity_id,detail)
		VALUES(?,?,?,'admin.init','user',?,'{}')`, ids.New(), ledgerID, userID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// NeedsInit reports whether no admin exists (first-run state).
func NeedsInit(db *sql.DB) (bool, error) {
	var admins int
	err := db.QueryRow(`SELECT COUNT(1) FROM ledger_members WHERE role='admin'`).Scan(&admins)
	return admins == 0, err
}

// ResetPassword is the server-local recovery path: it requires local
// access to the machine, writes an audit entry and revokes old sessions.
// There is no backdoor.
func ResetPassword(db *sql.DB, username, newPassword string) error {
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID string
	if err := tx.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&userID); err != nil {
		return errors.New("user not found")
	}
	if _, err := tx.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id=? AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO audit_log(id,ledger_id,actor_user_id,action,entity_type,entity_id,detail)
		VALUES(?,?,?,'admin.password_reset','user',?,'{}')`, ids.New(), "", userID, userID); err != nil {
		return err
	}
	return tx.Commit()
}
