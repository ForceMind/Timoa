package auth

import (
	"sync"
	"time"
)

// LoginLimiter is an in-memory per-key rate limiter for login attempts:
// after MaxFailures consecutive failures the key is locked for Lockout.
// It is deliberately simple; the single-process monolith restarts clear it.
type LoginLimiter struct {
	mu       sync.Mutex
	failures map[string]*failState
}

type failState struct {
	count    int
	lockedTo time.Time
}

const (
	MaxFailures = 5
	Lockout     = 15 * time.Minute
)

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{failures: make(map[string]*failState)}
}

// Allow reports whether an attempt may proceed.
func (l *LoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.failures[key]
	if !ok {
		return true
	}
	if st.count >= MaxFailures && time.Now().Before(st.lockedTo) {
		return false
	}
	return true
}

// Fail records a failed attempt.
func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.failures[key]
	if !ok {
		st = &failState{}
		l.failures[key] = st
	}
	if st.count >= MaxFailures && time.Now().After(st.lockedTo) {
		st.count = 0 // lockout elapsed, restart window
	}
	st.count++
	if st.count >= MaxFailures {
		st.lockedTo = time.Now().Add(Lockout)
	}
}

// Success clears the failure state.
func (l *LoginLimiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}
