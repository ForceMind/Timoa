package httpapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xiaozhang/internal/auth"
	"xiaozhang/internal/ids"
)

// ops_admin_handlers.go: 运营面板扩充 —— 用户详情（超管可查看全部数据）、冻结/解冻、
// 重置密码、备份管理、审计日志。全部仅平台超管可用，关键操作写审计。

func (s *server) registerOpsAdminRoutes(mux *http.ServeMux, opsPath string) {
	base := "/" + opsPath + "/api/"
	mux.Handle("GET "+base+"user/{userID}", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsUserDetail))))
	mux.Handle("POST "+base+"user/{userID}/freeze", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsFreezeUser))))
	mux.Handle("POST "+base+"user/{userID}/reset-password", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsResetPassword))))
	mux.Handle("GET "+base+"backups", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsListBackups))))
	mux.Handle("POST "+base+"backups", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsCreateBackup))))
	mux.Handle("GET "+base+"audit", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsAuditLog))))
	s.registerOpsAdmin2Routes(mux, opsPath)
}

// opsAudit 记录平台级运维操作到 audit_log（挂在一个固定 system 账本行上不可行，
// audit_log 需要 ledger_id，这里复用目标用户的主账本；平台全局操作用 actor 的主账本）。
func (s *server) opsAudit(ledgerID, actorID, action, entityType, entityID string, detail any) {
	if ledgerID == "" {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	b, _ := json.Marshal(detail)
	if _, err := tx.Exec(`INSERT INTO audit_log(id,ledger_id,actor_user_id,action,entity_type,entity_id,detail)
		VALUES(?,?,?,?,?,?,?)`, ids.New(), ledgerID, actorID, action, entityType, entityID, string(b)); err != nil {
		_ = tx.Rollback()
		return
	}
	_ = tx.Commit()
}

// userPrimaryLedger 返回用户主账本 id（自己拥有的第一个账本）。
func (s *server) userPrimaryLedger(userID string) string {
	var id string
	_ = s.db.QueryRow(`SELECT l.id FROM ledgers l JOIN ledger_members m ON m.ledger_id=l.id
		WHERE m.user_id=? AND l.owner_user_id=? ORDER BY l.created_at LIMIT 1`, userID, userID).Scan(&id)
	if id == "" {
		_ = s.db.QueryRow(`SELECT ledger_id FROM ledger_members WHERE user_id=? LIMIT 1`, userID).Scan(&id)
	}
	return id
}

// opsUserDetail：超管查看指定用户的账户（含余额）与最近流水。每次查看写审计。
func (s *server) opsUserDetail(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFrom(r.Context())
	userID := r.PathValue("userID")

	var username, displayName, createdAt string
	var archivedAt sql.NullString
	err := s.db.QueryRow(`SELECT username,display_name,created_at,archived_at FROM users WHERE id=?`, userID).
		Scan(&username, &displayName, &createdAt, &archivedAt)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "not_found", "user not found")
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	archived := archivedAt.Valid

	ledgerID := s.userPrimaryLedger(userID)
	if ledgerID == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"user_id": userID, "username": username, "display_name": displayName,
			"created_at": createdAt, "archived": archived, "accounts": []any{}, "transactions": []any{},
		})
		return
	}

	accounts, err := s.ledger.ListAccounts(ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	txs, err := s.ledger.ListTransactions(ledgerID, 200)
	if err != nil {
		writeError(w, err)
		return
	}

	s.opsAudit(ledgerID, sess.UserID, "ops.view_user", "user", userID,
		map[string]any{"username": username})

	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": userID, "username": username, "display_name": displayName,
		"created_at": createdAt, "archived": archived, "ledger_id": ledgerID,
		"accounts": accounts, "transactions": txs,
	})
}

// opsFreezeUser 冻结/解冻用户（冻结后立即吊销其全部会话）。
func (s *server) opsFreezeUser(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFrom(r.Context())
	userID := r.PathValue("userID")
	var body struct {
		Freeze bool `json:"freeze"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if userID == sess.UserID && body.Freeze {
		writeErr(w, 400, "invalid_input", "不能冻结自己")
		return
	}
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM users WHERE id=?`, userID).Scan(&exists); err != nil {
		writeErr(w, 404, "not_found", "user not found")
		return
	}
	if err := s.ledger.SetUserArchived(userID, body.Freeze); err != nil {
		writeError(w, err)
		return
	}
	if body.Freeze {
		_, _ = s.db.Exec(`UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`,
			time.Now().UTC(), userID)
	}
	action := "ops.freeze_user"
	if !body.Freeze {
		action = "ops.unfreeze_user"
	}
	s.opsAudit(s.userPrimaryLedger(userID), sess.UserID, action, "user", userID, map[string]any{"freeze": body.Freeze})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "archived": body.Freeze})
}

// opsResetPassword 超管为用户重置密码（生成随机新密码，强制下次登录改密，吊销会话）。
func (s *server) opsResetPassword(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFrom(r.Context())
	userID := r.PathValue("userID")

	newPassword := ids.Token(8) // 16 位随机密码，仅本次返回给超管，不落日志
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		writeError(w, err)
		return
	}
	res, err := s.db.Exec(`UPDATE users SET password_hash=?, must_change_password=1 WHERE id=?`, hash, userID)
	if err != nil {
		writeError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 404, "not_found", "user not found")
		return
	}
	_, _ = s.db.Exec(`UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`,
		time.Now().UTC(), userID)
	s.opsAudit(s.userPrimaryLedger(userID), sess.UserID, "ops.reset_password", "user", userID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "new_password": newPassword})
}

// backupDir 备份目录：<data>/backups。
func (s *server) backupDir() string { return filepath.Join(s.cfg.DataDir, "backups") }

// opsListBackups 列出备份文件。
func (s *server) opsListBackups(w http.ResponseWriter, r *http.Request) {
	dir := s.backupDir()
	type item struct {
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		CreatedAt string `json:"created_at"`
	}
	out := []item{}
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, item{Name: e.Name(), Size: fi.Size(), CreatedAt: fi.ModTime().UTC().Format(time.RFC3339)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": out, "dir": dir})
}

// opsCreateBackup 立即备份数据库文件到 <data>/backups/xiaozhang-<时间>.db。
func (s *server) opsCreateBackup(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFrom(r.Context())
	dir := s.backupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeError(w, err)
		return
	}
	name := fmt.Sprintf("xiaozhang-%s.db", time.Now().UTC().Format("20060102-150405"))
	dst := filepath.Join(dir, name)

	// 用 SQLite 备份语句落盘，避免直接复制时的一致性问题。
	if _, err := s.db.Exec(fmt.Sprintf(`VACUUM INTO '%s'`, strings.ReplaceAll(dst, "'", "''"))); err != nil {
		writeError(w, err)
		return
	}
	fi, _ := os.Stat(dst)
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	s.opsAudit(s.userPrimaryLedger(sess.UserID), sess.UserID, "ops.backup", "backup", name, map[string]any{"size": size})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "size": size, "dir": dir})
}

// opsAuditLog 列出最近平台运维审计（ops.* 动作），跨账本。
func (s *server) opsAuditLog(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT a.created_at, a.action, a.entity_type, a.entity_id,
			u.username, a.detail
		FROM audit_log a LEFT JOIN users u ON u.id=a.actor_user_id
		WHERE a.action LIKE 'ops.%' ORDER BY a.created_at DESC LIMIT 100`)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	type item struct {
		CreatedAt  string `json:"created_at"`
		Action     string `json:"action"`
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
		Actor      string `json:"actor"`
		Detail     string `json:"detail"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		var detail sql.NullString
		if err := rows.Scan(&it.CreatedAt, &it.Action, &it.EntityType, &it.EntityID, &it.Actor, &detail); err != nil {
			writeError(w, err)
			return
		}
		it.Detail = detail.String
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": out})
}
