package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"

	"xiaozhang/internal/ids"
)

// SessionTTL is the absolute session lifetime; sliding renewal can be
// added later without changing the cookie contract.
const SessionTTL = 30 * 24 * time.Hour

// Session is an authenticated server-side session.
type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

type contextKey string

// SessionContextKey is the request-context key for *Session.
const SessionContextKey contextKey = "xz.session"

// SessionFrom extracts the session attached by middleware.
func SessionFrom(ctx context.Context) *Session {
	s, _ := ctx.Value(SessionContextKey).(*Session)
	return s
}

// WithSession attaches a session to a request context.
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, SessionContextKey, s)
}

func sessionID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession persists a new session and returns the bearer token
// (only ever transported via the HttpOnly cookie).
func CreateSession(db *sql.DB, userID, userAgent string) (token string, err error) {
	token = ids.Token(32)
	exp := time.Now().UTC().Add(SessionTTL).Format("2006-01-02T15:04:05.000Z")
	_, err = db.Exec(`INSERT INTO sessions(id,user_id,expires_at,user_agent) VALUES(?,?,?,?)`,
		sessionID(token), userID, exp, userAgent)
	return token, err
}

// LookupSession resolves a token to a live session. Sessions of archived
// (revoked) members are rejected even if not yet explicitly revoked.
func LookupSession(db *sql.DB, token string) (*Session, error) {
	row := db.QueryRow(`SELECT s.id,s.user_id,s.expires_at FROM sessions s
		JOIN users u ON u.id=s.user_id
		WHERE s.id=? AND s.revoked_at IS NULL AND u.archived_at IS NULL`, sessionID(token))
	var s Session
	var exp string
	if err := row.Scan(&s.ID, &s.UserID, &exp); err != nil {
		return nil, err
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", exp)
	if err != nil || time.Now().After(t) {
		return nil, sql.ErrNoRows
	}
	s.ExpiresAt = t
	return &s, nil
}

// RevokeSession revokes one session (logout).
func RevokeSession(db *sql.DB, token string) error {
	_, err := db.Exec(`UPDATE sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, sessionID(token))
	return err
}

// RevokeUserSessions revokes all sessions of a user (password change,
// member revocation, restore).
func RevokeUserSessions(db *sql.DB, userID string) error {
	_, err := db.Exec(`UPDATE sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id=? AND revoked_at IS NULL`, userID)
	return err
}

// RevokeOtherUserSessions revokes all sessions of a user except the given
// (current) token — used after a self-service password change so the actor
// stays logged in while other devices are signed out.
func RevokeOtherUserSessions(db *sql.DB, userID, keepToken string) error {
	_, err := db.Exec(`UPDATE sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id=? AND revoked_at IS NULL AND id<>?`, userID, sessionID(keepToken))
	return err
}
