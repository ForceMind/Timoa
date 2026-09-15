// Package httpapi wires the versioned HTTP API and health endpoints.
package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"xiaozhang/internal/config"
	"xiaozhang/internal/storage"
)

// NewServer builds the root handler: /healthz, /readyz, /api/v1/*.
func NewServer(cfg config.Config, db *sql.DB) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": cfg.Version})
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		mv, err := storage.MigrationVersion(db)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		sqliteVer, _ := storage.SQLiteVersion(db)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":            "ready",
			"version":           cfg.Version,
			"sqlite_version":    sqliteVer,
			"migration_version": mv,
		})
	})

	mux.HandleFunc("GET /api/v1/meta", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"name":    "小账 XiaoZhang",
			"version": cfg.Version,
			"api":     "v1",
		})
	})

	return securityHeaders(mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// securityHeaders applies baseline security headers. API responses are
// never cached; static asset caching is configured separately at the
// static file handler.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
