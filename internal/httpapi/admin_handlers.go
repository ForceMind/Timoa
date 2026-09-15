package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xiaozhang/internal/auth"
	"xiaozhang/internal/backup"
	"xiaozhang/internal/storage"
)

// admin_handlers.go: 手动备份、备份下载、诊断（全部管理员专属，
// 备份下载需重新输入密码确认）。

func (s *server) adminBackupNow(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if !s.reauth(w, r) {
		return
	}
	path, err := backup.Create(s.db, s.cfg.DataDir, os.Getenv("XIAOZHANG_BACKUP_KEY"), time.Now())
	if err != nil {
		writeErr(w, 500, "backup_failed", err.Error())
		return
	}
	_ = backup.Prune(s.cfg.DataDir, 14)
	writeJSON(w, http.StatusOK, map[string]string{"file": filepath.Base(path)})
}

func (s *server) adminBackups(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	st := backup.LatestStatus(s.cfg.DataDir)
	dir := filepath.Join(s.cfg.DataDir, "backups")
	entries, _ := os.ReadDir(dir)
	var files []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "xiaozhang-") && !strings.HasSuffix(e.Name(), ".part") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	writeJSON(w, http.StatusOK, map[string]any{"status": st, "files": files})
}

func (s *server) adminDownloadBackup(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if !s.reauth(w, r) {
		return
	}
	name := r.PathValue("name")
	if strings.Contains(name, "/") || strings.Contains(name, "..") || !strings.HasPrefix(name, "xiaozhang-") {
		writeErr(w, 400, "invalid_input", "bad file name")
		return
	}
	path := filepath.Join(s.cfg.DataDir, "backups", name)
	if _, err := os.Stat(path); err != nil {
		writeErr(w, 404, "not_found", "backup not found")
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	http.ServeFile(w, r, path)
}

func (s *server) adminDiagnostics(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	mv, _ := storage.MigrationVersion(s.db)
	sqliteVer, _ := storage.SQLiteVersion(s.db)
	writeJSON(w, http.StatusOK, map[string]any{
		"version":           s.cfg.Version,
		"sqlite_version":    sqliteVer,
		"migration_version": mv,
		"backup":            backup.LatestStatus(s.cfg.DataDir),
	})
}

// reauth 敏感操作要求重新输入密码确认。
func (s *server) reauth(w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Password == "" {
		writeErr(w, 403, "reauth_required", "password confirmation required")
		return false
	}
	sess := sessionOf(r)
	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, sess.UserID).Scan(&hash); err != nil {
		writeError(w, err)
		return false
	}
	if !auth.VerifyPassword(body.Password, hash) {
		writeErr(w, 403, "reauth_failed", "password incorrect")
		return false
	}
	return true
}
