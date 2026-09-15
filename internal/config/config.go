// Package config holds process configuration sourced from flags and
// environment variables. Secrets never come from the command line in docs.
package config

import "os"

// Config is the runtime configuration for the xiaozhang server.
type Config struct {
	// Addr is the listen address. Production default: 127.0.0.1:8787
	// (behind a reverse proxy, not exposed publicly).
	Addr string
	// DataDir holds the SQLite database, attachments and backups.
	DataDir string
	// Version is the build version reported by /healthz.
	Version string
	// SecureCookies marks session cookies Secure (enable behind HTTPS).
	SecureCookies bool
}

// Getenv returns the environment variable value or a fallback.
func Getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
