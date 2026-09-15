package httpapi

import (
	"net/http"

	"xiaozhang/internal/auth"
)

// member_handlers.go: 邀请、成员、加入、预测、日历。

func (s *server) createInvite(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	token, inv, err := s.ledger.CreateInvite(m.ledgerID, actorID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	// 令牌明文仅此一次返回；分享方式由管理员自选（不要求邮件）
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "invite": inv})
}

func (s *server) listInvites(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	inv, err := s.ledger.ListInvites(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": inv})
}

func (s *server) revokeInvite(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if err := s.ledger.RevokeInvite(m.ledgerID, r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) listMembers(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	ms, err := s.ledger.ListMembers(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": ms})
}

func (s *server) revokeMember(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if err := s.ledger.RevokeMember(m.ledgerID, actorID(r), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// join 是唯一创建账号的入口（公开注册关闭）：凭一次性有效邀请令牌。
func (s *server) join(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
		Name     string `json:"display_name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Token == "" || body.Username == "" || body.Password == "" {
		writeErr(w, 400, "invalid_input", "token, username and password required")
		return
	}
	key := "join|" + clientIP(r)
	if !s.limiter.Allow(key) {
		writeErr(w, 429, "rate_limited", "too many attempts, try again later")
		return
	}
	userID, err := s.ledger.AcceptInvite(body.Token, body.Username, body.Name, body.Password)
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
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusCreated, map[string]any{"user_id": userID})
}

func (s *server) forecast(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	f, err := s.ledger.Forecast(m.ledgerID, timeNow())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *server) calendar(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	month := r.URL.Query().Get("month")
	if len(month) != 7 {
		writeErr(w, 400, "invalid_input", "month must be YYYY-MM")
		return
	}
	days, err := s.ledger.CalendarMonth(m.ledgerID, month)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days})
}
