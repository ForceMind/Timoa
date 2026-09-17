package httpapi

import (
	"net/http"

	"xiaozhang/internal/auth"
)

// platform_handlers.go: 多租户 SaaS 形态——公开注册 + 平台超管后台 API。
//
// 隐私边界：超管 API 只返回跨用户统计/元数据，绝不返回任何账本明细；
// 明细接口仍走账本成员校验（requireAuth + mustMembership），超管不例外。

// register 公开注册（受 platform_settings.registration_open 开关控制）。
// 注册即创建独立账本并直接登录（与 join 一致：注册成功即建会话）。
func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Name     string `json:"display_name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Username == "" || body.Password == "" {
		writeErr(w, 400, "invalid_input", "username and password required")
		return
	}
	key := "register|" + clientIP(r)
	if !s.limiter.Allow(key) {
		writeErr(w, 429, "rate_limited", "too many attempts, try again later")
		return
	}
	userID, err := s.ledger.RegisterUser(body.Username, body.Name, body.Password)
	if err != nil {
		s.limiter.Fail(key)
		writeError(w, err)
		return
	}
	s.limiter.Success(key)
	token, err := auth.CreateSession(s.db, userID, r.UserAgent())
	if err != nil {
		writeError(w, err)
		return
	}
	s.setSessionCookie(w, r, token)
	writeJSON(w, http.StatusCreated, map[string]any{"user_id": userID})
}

// registrationStatus 报告注册是否开放（落地页据此显示/隐藏注册入口）。
func (s *server) registrationStatus(w http.ResponseWriter, r *http.Request) {
	open, err := s.ledger.RegistrationOpen()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"registration_open": open})
}

// requireSuperadmin 平台超管校验（platform_role='superadmin'，服务端强制）。
func (s *server) requireSuperadmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := auth.SessionFrom(r.Context())
		ok, err := s.ledger.IsSuperadmin(sess.UserID)
		if err != nil {
			writeError(w, err)
			return
		}
		if !ok {
			writeErr(w, 403, "permission_denied", "platform superadmin required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// platformOverview 全局统计 + 用户列表（仅元数据）。
func (s *server) platformOverview(w http.ResponseWriter, r *http.Request) {
	stats, users, err := s.ledger.PlatformOverview()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats, "users": users})
}

// platformSetUserArchived 冻结/解冻用户；冻结即吊销其全部会话。
func (s *server) platformSetUserArchived(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	var body struct {
		Archived bool `json:"archived"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	// 防止超管冻结自己导致失锁
	sess := auth.SessionFrom(r.Context())
	if userID == sess.UserID && body.Archived {
		writeErr(w, 400, "invalid_input", "cannot freeze yourself")
		return
	}
	if err := s.ledger.SetUserArchived(userID, body.Archived); err != nil {
		writeError(w, err)
		return
	}
	if body.Archived {
		_ = auth.RevokeUserSessions(s.db, userID)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// platformResetPassword 超管重置某用户密码并吊销其会话。
func (s *server) platformResetPassword(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.ledger.PlatformResetPassword(userID, body.Password); err != nil {
		writeError(w, err)
		return
	}
	_ = auth.RevokeUserSessions(s.db, userID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// platformSetRegistration 超管切换公开注册开关。
func (s *server) platformSetRegistration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Open bool `json:"open"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.ledger.SetRegistrationOpen(body.Open); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"registration_open": body.Open})
}
