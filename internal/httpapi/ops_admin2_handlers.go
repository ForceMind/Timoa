package httpapi

import (
	"database/sql"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"xiaozhang/internal/auth"
)

// ops_admin2_handlers.go: 运营面板第二扩充 —— 访问记录、在线会话、服务器状态、服务控制。

func (s *server) registerOpsAdmin2Routes(mux *http.ServeMux, opsPath string) {
	base := "/" + opsPath + "/api/"
	mux.Handle("GET "+base+"user/{userID}/logins", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsUserLogins))))
	mux.Handle("GET "+base+"user/{userID}/sessions", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsUserSessions))))
	mux.Handle("POST "+base+"session/{sessionID}/revoke", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsRevokeSession))))
	mux.Handle("GET "+base+"server", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsServerStatus))))
	mux.Handle("POST "+base+"service/{action}", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsServiceAction))))
}

// opsUserLogins：某用户的登录/登出历史（IP、设备、系统、浏览器）。
func (s *server) opsUserLogins(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFrom(r.Context())
	userID := r.PathValue("userID")
	rows, err := s.db.Query(`SELECT action,ip,user_agent,device,os,browser,created_at
		FROM login_history WHERE user_id=? ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	type item struct {
		Action    string `json:"action"`
		IP        string `json:"ip"`
		UserAgent string `json:"user_agent"`
		Device    string `json:"device"`
		OS        string `json:"os"`
		Browser   string `json:"browser"`
		CreatedAt string `json:"created_at"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		var ip, ua, d, o, b sql.NullString
		if err := rows.Scan(&it.Action, &ip, &ua, &d, &o, &b, &it.CreatedAt); err != nil {
			writeError(w, err)
			return
		}
		it.IP, it.UserAgent, it.Device, it.OS, it.Browser = ip.String, ua.String, d.String, o.String, b.String
		out = append(out, it)
	}
	s.opsAudit(s.userPrimaryLedger(userID), sess.UserID, "ops.view_logins", "user", userID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"logins": out})
}

// opsUserSessions：某用户的会话列表（含 IP、UA、是否在线、过期时间）。
func (s *server) opsUserSessions(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	rows, err := s.db.Query(`SELECT id,ip,user_agent,created_at,expires_at,revoked_at
		FROM sessions WHERE user_id=? ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	now := time.Now().UTC()
	type item struct {
		ID        string `json:"id"`
		IP        string `json:"ip"`
		UserAgent string `json:"user_agent"`
		Device    string `json:"device"`
		OS        string `json:"os"`
		Browser   string `json:"browser"`
		CreatedAt string `json:"created_at"`
		ExpiresAt string `json:"expires_at"`
		Revoked   bool   `json:"revoked"`
		Live      bool   `json:"live"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		var ip, ua, rev sql.NullString
		var exp string
		if err := rows.Scan(&it.ID, &ip, &ua, &it.CreatedAt, &exp, &rev); err != nil {
			writeError(w, err)
			return
		}
		it.IP, it.UserAgent, it.ExpiresAt = ip.String, ua.String, exp
		it.Revoked = rev.Valid
		it.Device, it.OS, it.Browser = auth.ParseUA(it.UserAgent)
		if t, err := time.Parse("2006-01-02T15:04:05.000Z", exp); err == nil {
			it.Live = !it.Revoked && now.Before(t)
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// opsRevokeSession 强制下线单个会话（按会话 id，即 sha256(token)）。
func (s *server) opsRevokeSession(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFrom(r.Context())
	sessionID := r.PathValue("sessionID")
	if _, err := s.db.Exec(`UPDATE sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND revoked_at IS NULL`, sessionID); err != nil {
		writeError(w, err)
		return
	}
	s.opsAudit("", sess.UserID, "ops.revoke_session", "session", sessionID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// opsServerStatus：服务器运行状态（进程、系统、磁盘、Go 运行时、数据库大小）。
func (s *server) opsServerStatus(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	dbPath := s.cfg.DataDir + "/xiaozhang.db"
	var dbSize int64
	if fi, err := os.Stat(dbPath); err == nil {
		dbSize = fi.Size()
	}

	// 磁盘用量（数据目录所在分区）。优先 statfs via syscall 太平台相关，用 df 命令兜底。
	diskTotal, diskAvail := diskUsage(s.cfg.DataDir)

	// 系统负载与运行时长（Linux /proc 可读；失败则留空）。
	loadAvg := readFileTrim("/proc/loadavg")
	uptime := readFileTrim("/proc/uptime")

	writeJSON(w, http.StatusOK, map[string]any{
		"go_version":    runtime.Version(),
		"goroutines":    runtime.NumGoroutine(),
		"num_cpu":       runtime.NumCPU(),
		"goos":          runtime.GOOS,
		"goarch":        runtime.GOARCH,
		"mem_alloc_mb":  ms.Alloc / 1024 / 1024,
		"mem_sys_mb":    ms.Sys / 1024 / 1024,
		"db_size_mb":    dbSize / 1024 / 1024,
		"db_path":       dbPath,
		"disk_total_gb": diskTotal,
		"disk_avail_gb": diskAvail,
		"load_avg":      loadAvg,
		"uptime":        uptime,
		"time":          time.Now().UTC().Format(time.RFC3339),
	})
}

func readFileTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// diskUsage 返回 (totalGB, availGB)。用 df -k 解析数据目录所在分区。
func diskUsage(dir string) (float64, float64) {
	out, err := exec.Command("df", "-k", dir).Output()
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, 0
	}
	total, _ := strconv.ParseFloat(fields[1], 64)
	avail, _ := strconv.ParseFloat(fields[3], 64)
	return total / 1024 / 1024, avail / 1024 / 1024
}

// opsServiceAction：重启/停止/查看服务状态（仅当 systemd 可用时）。
// action ∈ restart/stop/status。restart 会中断当前连接，前端需提示稍后刷新。
func (s *server) opsServiceAction(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFrom(r.Context())
	action := r.PathValue("action")
	if action != "restart" && action != "stop" && action != "status" {
		writeErr(w, 400, "invalid_input", "action must be restart|stop|status")
		return
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		writeErr(w, 500, "unsupported", "systemd 不可用，无法在服务内控制服务")
		return
	}
	s.opsAudit("", sess.UserID, "ops.service_"+action, "service", "xiaozhang", nil)

	if action == "status" {
		out, _ := exec.Command("systemctl", "show", "xiaozhang",
			"--property=ActiveState,SubState,MainPID,ExecMainStartTimestamp,MemoryCurrent").Output()
		writeJSON(w, http.StatusOK, map[string]any{"status": strings.TrimSpace(string(out))})
		return
	}

	// restart/stop 异步执行，避免中断自身响应。
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = exec.Command("systemctl", action, "xiaozhang").Run()
	}()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": action, "note": "指令已下发，服务将" + map[string]string{"restart": "重启", "stop": "停止"}[action] + "，请稍后刷新"})
}
