package httpapi

import (
	"net/http"

	"xiaozhang/internal/auth"
)

// ops_panel_handlers.go: 运营面板（纯 Web，随机路径入口 + 登录 + 平台超管）。
//
// 安全模型：面板页面与 API 都挂在随机路径 /{ops_path} 下（路径不可猜），
// 且每个 API 仍需登录会话 + platform_role='superadmin'（requireSuperadmin）。
// 随机路径只是「隐蔽入口」的一层，不是唯一认证——认证仍靠会话。
//
// 隐私边界：与 /api/v1/platform/* 一致，仅统计与元数据，不含任何账目明细。

// registerOpsRoutes 把运营面板 API 挂到随机路径下。opsPath 形如 "ops-x7k9p2qm"。
func (s *server) registerOpsRoutes(mux *http.ServeMux, opsPath string) {
	base := "/" + opsPath + "/api/"
	mux.Handle("GET "+base+"status", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsStatus))))
	mux.Handle("POST "+base+"make-superadmin", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsMakeSuperadmin))))
	mux.Handle("POST "+base+"regenerate-path", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.opsRegeneratePath))))
	mux.Handle("POST "+base+"registration", s.requireAuth(s.requireSuperadmin(http.HandlerFunc(s.platformSetRegistration))))
}

// opsStatus 面板首页状态：版本、监听地址、注册开关、当前后台路径、全局统计与用户列表。
func (s *server) opsStatus(w http.ResponseWriter, r *http.Request) {
	stats, users, err := s.ledger.PlatformOverview()
	if err != nil {
		writeError(w, err)
		return
	}
	regOpen, err := s.ledger.RegistrationOpen()
	if err != nil {
		writeError(w, err)
		return
	}
	opsPath, err := s.ledger.EnsureOpsPath()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":           s.cfg.Version,
		"addr":              s.cfg.Addr,
		"data_dir":          s.cfg.DataDir,
		"registration_open": regOpen,
		"ops_path":          opsPath,
		"stats":             stats,
		"users":             users,
	})
}

// opsMakeSuperadmin 把指定用户名的用户提升为平台超管（仅超管可操作）。
func (s *server) opsMakeSuperadmin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Username == "" {
		writeErr(w, 400, "invalid_input", "username required")
		return
	}
	res, err := s.db.Exec(`UPDATE users SET platform_role='superadmin' WHERE username=?`, body.Username)
	if err != nil {
		writeError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 404, "not_found", "user not found: "+body.Username)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": body.Username})
}

// opsRegeneratePath 重新生成后台随机路径（旧入口立即失效）。返回新路径。
func (s *server) opsRegeneratePath(w http.ResponseWriter, r *http.Request) {
	_ = auth.SessionFrom(r.Context()) // 已经过 requireSuperadmin；sess 为将来审计预留
	newPath, err := s.ledger.RegenerateOpsPath()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ops_path": newPath})
}
