// Package httpapi wires the versioned HTTP API and health endpoints.
package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"xiaozhang/internal/auth"
	"xiaozhang/internal/bootstrap"
	"xiaozhang/internal/config"
	"xiaozhang/internal/ledger"
	"xiaozhang/internal/money"
	"xiaozhang/internal/storage"
)

type server struct {
	cfg     config.Config
	db      *sql.DB
	ledger  *ledger.Service
	limiter *auth.LoginLimiter
}

// NewServer builds the root handler: /healthz, /readyz, /api/v1/*.
func NewServer(cfg config.Config, db *sql.DB) http.Handler {
	s := &server{cfg: cfg, db: db, ledger: ledger.NewService(db), limiter: auth.NewLoginLimiter()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("GET /api/v1/setup/status", s.setupStatus)

	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.Handle("POST /api/v1/auth/logout", s.requireAuth(http.HandlerFunc(s.logout)))
	mux.Handle("GET /api/v1/auth/me", s.requireAuth(http.HandlerFunc(s.me)))

	mux.Handle("GET /api/v1/accounts", s.requireAuth(http.HandlerFunc(s.listAccounts)))
	mux.Handle("POST /api/v1/accounts", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.createAccount))))
	mux.Handle("POST /api/v1/accounts/{id}/archive", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.archiveAccount))))
	mux.Handle("POST /api/v1/accounts/{id}/sub", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.createSubAccount))))
	mux.Handle("POST /api/v1/accounts/{id}/stored-value-meta", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.setStoredValueMeta))))

	mux.Handle("GET /api/v1/categories", s.requireAuth(http.HandlerFunc(s.listCategories)))
	mux.Handle("POST /api/v1/categories", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.createCategory))))

	mux.Handle("GET /api/v1/transactions", s.requireAuth(http.HandlerFunc(s.listTransactions)))
	mux.Handle("POST /api/v1/transactions", s.requireAuth(http.HandlerFunc(s.postTransaction)))
	mux.Handle("GET /api/v1/transactions/{id}", s.requireAuth(http.HandlerFunc(s.txDetail)))
	mux.Handle("POST /api/v1/transactions/{id}/refund", s.requireAuth(http.HandlerFunc(s.refund)))
	mux.Handle("POST /api/v1/transactions/{id}/income-refund", s.requireAuth(http.HandlerFunc(s.incomeRefund)))
	mux.Handle("POST /api/v1/transactions/{id}/settle", s.requireAuth(http.HandlerFunc(s.settle)))
	mux.Handle("POST /api/v1/transactions/{id}/reclass", s.requireAuth(http.HandlerFunc(s.reclass)))
	mux.Handle("POST /api/v1/transactions/{id}/writeoff", s.requireAuth(http.HandlerFunc(s.writeoff)))
	mux.Handle("POST /api/v1/transactions/{id}/revise", s.requireAuth(http.HandlerFunc(s.revise)))

	mux.Handle("GET /api/v1/receivables", s.requireAuth(http.HandlerFunc(s.receivables)))

	mux.Handle("POST /api/v1/money/lend", s.requireAuth(http.HandlerFunc(s.lend)))
	mux.Handle("POST /api/v1/money/borrow", s.requireAuth(http.HandlerFunc(s.borrow)))
	mux.Handle("POST /api/v1/money/repay", s.requireAuth(http.HandlerFunc(s.repay)))
	mux.Handle("POST /api/v1/money/loan-repay", s.requireAuth(http.HandlerFunc(s.loanRepay)))
	mux.Handle("POST /api/v1/money/redeem", s.requireAuth(http.HandlerFunc(s.redeem)))
	mux.Handle("POST /api/v1/transactions/{id}/copy", s.requireAuth(http.HandlerFunc(s.copyTx)))

	mux.Handle("GET /api/v1/templates", s.requireAuth(http.HandlerFunc(s.listTemplates)))
	mux.Handle("POST /api/v1/templates/{id}/pin", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.pinTemplate))))
	mux.Handle("POST /api/v1/templates/{id}/enable", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.enableTemplate))))
	mux.Handle("GET /api/v1/tags", s.requireAuth(http.HandlerFunc(s.listTags)))
	mux.Handle("POST /api/v1/tags", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.createTag))))
	mux.Handle("GET /api/v1/search", s.requireAuth(http.HandlerFunc(s.searchTx)))

	mux.Handle("GET /api/v1/notes", s.requireAuth(http.HandlerFunc(s.listNotes)))
	mux.Handle("POST /api/v1/notes", s.requireAuth(http.HandlerFunc(s.createNote)))
	mux.Handle("POST /api/v1/notes/{id}", s.requireAuth(http.HandlerFunc(s.updateNote)))
	mux.Handle("DELETE /api/v1/notes/{id}", s.requireAuth(http.HandlerFunc(s.deleteNote)))

	mux.Handle("GET /api/v1/recurrence/rules", s.requireAuth(http.HandlerFunc(s.listRules)))
	mux.Handle("POST /api/v1/recurrence/rules", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.createRule))))
	mux.Handle("POST /api/v1/recurrence/rules/{id}/enable", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.enableRule))))
	mux.Handle("GET /api/v1/recurrence/pending", s.requireAuth(http.HandlerFunc(s.pendingInstances)))
	mux.Handle("POST /api/v1/recurrence/instances/{id}/skip", s.requireAuth(http.HandlerFunc(s.skipInstance)))
	mux.Handle("POST /api/v1/recurrence/instances/{id}/postpone", s.requireAuth(http.HandlerFunc(s.postponeInstance)))
	mux.Handle("GET /api/v1/recommendations", s.requireAuth(http.HandlerFunc(s.recommendations)))

	mux.Handle("GET /api/v1/amortizations", s.requireAuth(http.HandlerFunc(s.listAmortizations)))
	mux.Handle("POST /api/v1/amortizations", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.createAmortization))))
	mux.Handle("POST /api/v1/amortizations/{id}/run", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.runAmortization))))
	mux.Handle("POST /api/v1/amortizations/{id}/cancel", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.cancelAmortization))))

	mux.Handle("POST /api/v1/budgets", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.setBudget))))
	mux.Handle("GET /api/v1/budgets/status", s.requireAuth(http.HandlerFunc(s.budgetStatus)))
	mux.Handle("POST /api/v1/budgets/{id}/delete", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.deleteBudget))))
	mux.Handle("POST /api/v1/goals", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.createGoal))))
	mux.Handle("GET /api/v1/goals", s.requireAuth(http.HandlerFunc(s.listGoals)))
	mux.Handle("POST /api/v1/goals/{id}/done", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.doneGoal))))
	mux.Handle("GET /api/v1/stats/assets", s.requireAuth(http.HandlerFunc(s.assetsOverview)))
	mux.Handle("POST /api/v1/recommendations/dismiss", s.requireAuth(http.HandlerFunc(s.dismissRecommendation)))
	mux.Handle("GET /api/v1/suggest/merchant", s.requireAuth(http.HandlerFunc(s.merchantSuggest)))

	mux.Handle("POST /api/v1/import/preview", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.importPreview))))
	mux.Handle("POST /api/v1/import/confirm", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.importConfirm))))
	mux.Handle("GET /api/v1/import/batches", s.requireAuth(http.HandlerFunc(s.importBatches)))
	mux.Handle("POST /api/v1/import/batches/{id}/undo", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.importUndo))))
	mux.Handle("GET /api/v1/export/transactions", s.requireAuth(http.HandlerFunc(s.exportTx)))
	mux.Handle("POST /api/v1/transactions/{id}/attachments", s.requireAuth(http.HandlerFunc(s.uploadAttachment)))
	mux.Handle("GET /api/v1/transactions/{id}/attachments", s.requireAuth(http.HandlerFunc(s.listAttachments)))
	mux.Handle("GET /api/v1/attachments/{id}", s.requireAuth(http.HandlerFunc(s.serveAttachment)))

	mux.Handle("POST /api/v1/invites", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.createInvite))))
	mux.Handle("GET /api/v1/invites", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.listInvites))))
	mux.Handle("POST /api/v1/invites/{id}/revoke", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.revokeInvite))))
	mux.Handle("GET /api/v1/members", s.requireAuth(http.HandlerFunc(s.listMembers)))
	mux.Handle("POST /api/v1/members/{id}/revoke", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.revokeMember))))
	mux.HandleFunc("POST /api/v1/auth/join", s.join)
	mux.Handle("GET /api/v1/stats/forecast", s.requireAuth(http.HandlerFunc(s.forecast)))
	mux.Handle("GET /api/v1/calendar", s.requireAuth(http.HandlerFunc(s.calendar)))

	mux.Handle("POST /api/v1/admin/backup", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.adminBackupNow))))
	mux.Handle("GET /api/v1/admin/backups", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.adminBackups))))
	mux.Handle("POST /api/v1/admin/backups/{name}/download", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.adminDownloadBackup))))
	mux.Handle("GET /api/v1/admin/diagnostics", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.adminDiagnostics))))

	mux.Handle("POST /api/v1/sync/push", s.requireAuth(http.HandlerFunc(s.syncPush)))
	mux.Handle("GET /api/v1/sync/pull", s.requireAuth(http.HandlerFunc(s.syncPull)))

	// 同源托管前端（生产）；/api 与 /healthz 已在上方优先匹配
	mux.Handle("/", staticHandler())

	mux.Handle("GET /api/v1/summary", s.requireAuth(http.HandlerFunc(s.summary)))
	mux.Handle("GET /api/v1/stats/daily", s.requireAuth(http.HandlerFunc(s.statsDaily)))
	mux.Handle("GET /api/v1/stats/overview", s.requireAuth(http.HandlerFunc(s.statsOverview)))
	mux.Handle("GET /api/v1/stats/categories", s.requireAuth(http.HandlerFunc(s.statsCategories)))

	return securityHeaders(sameOrigin(mux))
}

// ---------------------------------------------------------------------------
// health
// ---------------------------------------------------------------------------

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.cfg.Version})
}

func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	mv, err := storage.MigrationVersion(s.db)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	sqliteVer, _ := storage.SQLiteVersion(s.db)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ready",
		"version":           s.cfg.Version,
		"sqlite_version":    sqliteVer,
		"migration_version": mv,
	})
}

func (s *server) setupStatus(w http.ResponseWriter, r *http.Request) {
	needs, err := bootstrap.NeedsInit(s.db)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needs_init": needs})
}

// ---------------------------------------------------------------------------
// auth
// ---------------------------------------------------------------------------

const sessionCookie = "xz_session"

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	key := body.Username + "|" + clientIP(r)
	if !s.limiter.Allow(key) {
		writeErr(w, 429, "rate_limited", "too many failed attempts, try again later")
		return
	}

	var userID, hash, displayName string
	var archived sql.NullString
	err := s.db.QueryRow(`SELECT id,password_hash,display_name,archived_at FROM users WHERE username=?`,
		body.Username).Scan(&userID, &hash, &displayName, &archived)
	if err != nil || archived.Valid || !auth.VerifyPassword(body.Password, hash) {
		s.limiter.Fail(key)
		writeErr(w, 401, "invalid_credentials", "username or password incorrect")
		return
	}
	s.limiter.Success(key)

	token, err := auth.CreateSession(s.db, userID, r.UserAgent())
	if err != nil {
		writeError(w, err)
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"user_id": userID, "display_name": displayName})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = auth.RevokeSession(s.db, c.Value)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFrom(r.Context())
	m, err := s.membership(sess.UserID)
	if err != nil {
		writeError(w, err)
		return
	}
	var displayName, username string
	if err := s.db.QueryRow(`SELECT username,display_name FROM users WHERE id=?`, sess.UserID).Scan(&username, &displayName); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": sess.UserID, "username": username, "display_name": displayName,
		"ledger_id": m.ledgerID, "ledger_name": m.ledgerName, "role": m.role,
	})
}

func (s *server) setSessionCookie(w http.ResponseWriter, token string) {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.SecureCookies,
		MaxAge:   int(auth.SessionTTL.Seconds()),
	}
	http.SetCookie(w, c)
}

func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.cfg.SecureCookies, MaxAge: -1,
	})
}

// requireAuth attaches the session or rejects with 401.
func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeErr(w, 401, "unauthenticated", "login required")
			return
		}
		sess, err := auth.LookupSession(s.db, c.Value)
		if err != nil {
			writeErr(w, 401, "unauthenticated", "session expired or revoked")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithSession(r.Context(), sess)))
	})
}

// requireAdmin restricts a route to ledger admins (server-side check; the
// frontend hiding buttons is never the enforcement point).
func (s *server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := auth.SessionFrom(r.Context())
		m, err := s.membership(sess.UserID)
		if err != nil {
			writeError(w, err)
			return
		}
		if m.role != "admin" {
			writeErr(w, 403, "permission_denied", "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type memberInfo struct{ ledgerID, ledgerName, role string }

func (s *server) membership(userID string) (*memberInfo, error) {
	var m memberInfo
	err := s.db.QueryRow(`SELECT m.ledger_id,l.name,m.role FROM ledger_members m
		JOIN ledgers l ON l.id=m.ledger_id WHERE m.user_id=? ORDER BY m.created_at LIMIT 1`, userID).
		Scan(&m.ledgerID, &m.ledgerName, &m.role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &ledger.Error{Code: "no_ledger", Message: "user has no ledger membership", HTTP: 409}
	}
	return &m, err
}

// ---------------------------------------------------------------------------
// accounts / categories
// ---------------------------------------------------------------------------

func (s *server) listAccounts(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	accts, err := s.ledger.ListAccounts(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accts})
}

func (s *server) createAccount(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	sess := auth.SessionFrom(r.Context())
	var body struct {
		Name             string `json:"name"`
		Type             string `json:"type"`
		OpeningYuan      string `json:"opening_balance"` // yuan string, may be ""
		OpeningDate      string `json:"opening_date"`
		BalanceConfirmed bool   `json:"balance_confirmed"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	var opening int64
	if strings.TrimSpace(body.OpeningYuan) != "" {
		c, err := money.ParseYuan(body.OpeningYuan)
		if err != nil {
			writeErr(w, 400, "invalid_amount", "opening balance: "+err.Error())
			return
		}
		opening = c
	}
	var od *string
	if body.OpeningDate != "" {
		od = &body.OpeningDate
	}
	acct, err := s.ledger.CreateAccount(m.ledgerID, sess.UserID, body.Name, body.Type, nil, opening, od, body.BalanceConfirmed)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, acct)
}

func (s *server) archiveAccount(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if err := s.ledger.ArchiveAccount(m.ledgerID, auth.SessionFrom(r.Context()).UserID, r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// createSubAccount：在资产类账户下创建二类子账户（活期/定期/理财）。
func (s *server) createSubAccount(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	sess := auth.SessionFrom(r.Context())
	var body struct {
		Name             string `json:"name"`
		SubKind          string `json:"sub_kind"` // current/deposit/investment
		OpeningYuan      string `json:"opening_balance"`
		OpeningDate      string `json:"opening_date"`
		BalanceConfirmed bool   `json:"balance_confirmed"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	var opening int64
	if strings.TrimSpace(body.OpeningYuan) != "" {
		c, err := money.ParseYuan(body.OpeningYuan)
		if err != nil {
			writeErr(w, 400, "invalid_amount", "opening balance: "+err.Error())
			return
		}
		opening = c
	}
	var od *string
	if body.OpeningDate != "" {
		od = &body.OpeningDate
	}
	acct, err := s.ledger.CreateSubAccount(m.ledgerID, sess.UserID, r.PathValue("id"), body.Name, body.SubKind, opening, od, body.BalanceConfirmed)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, acct)
}

// setStoredValueMeta：储值卡面额与到期日。
func (s *server) setStoredValueMeta(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		FaceValueCents string `json:"face_value"` // yuan string, may be ""
		ExpiresOn      string `json:"expires_on"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	var face int64
	if strings.TrimSpace(body.FaceValueCents) != "" {
		c, err := money.ParseYuan(body.FaceValueCents)
		if err != nil {
			writeErr(w, 400, "invalid_amount", "face value: "+err.Error())
			return
		}
		face = c
	}
	if err := s.ledger.SetStoredValueMeta(m.ledgerID, r.PathValue("id"), face, body.ExpiresOn); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) listCategories(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	cats, err := s.ledger.ListCategories(m.ledgerID, r.URL.Query().Get("kind"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": cats})
}

func (s *server) createCategory(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		ParentID string `json:"parent_id"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	var parent *string
	if body.ParentID != "" {
		parent = &body.ParentID
	}
	cat, err := s.ledger.CreateCategory(m.ledgerID, auth.SessionFrom(r.Context()).UserID, parent, body.Kind, body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, cat)
}

// ---------------------------------------------------------------------------
// transactions
// ---------------------------------------------------------------------------

func (s *server) listTransactions(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	txs, err := s.ledger.ListTransactions(m.ledgerID, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": txs})
}

func (s *server) postTransaction(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	sess := auth.SessionFrom(r.Context())
	var body struct {
		Type          string `json:"type"`
		BusinessDate  string `json:"business_date"`
		DatePrecision string `json:"date_precision"`
		AmountYuan    string `json:"amount"` // yuan string, exactly two decimals max
		CategoryID    string `json:"category_id"`
		Splits        []struct {
			PartType     string `json:"part_type"`
			CategoryID   string `json:"category_id"`
			Counterparty string `json:"counterparty"`
			Amount       string `json:"amount"`
		} `json:"splits"`
		FromAccountID string `json:"from_account_id"`
		ToAccountID   string `json:"to_account_id"`
		Note          string `json:"note"`
		Merchant      string `json:"merchant"`
		Channel       string `json:"channel"`
		OperationID   string `json:"operation_id"`
		RecurrenceInstanceID string `json:"recurrence_instance_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	cents, err := money.ParseYuanRequired(body.AmountYuan)
	if err != nil {
		writeErr(w, 400, "invalid_amount", err.Error())
		return
	}
	var splits []ledger.SplitInput
	for _, sp := range body.Splits {
		sc, err := money.ParseYuanRequired(sp.Amount)
		if err != nil {
			writeErr(w, 400, "invalid_amount", "split: "+err.Error())
			return
		}
		splits = append(splits, ledger.SplitInput{
			PartType: sp.PartType, CategoryID: sp.CategoryID,
			Counterparty: sp.Counterparty, AmountCents: sc,
		})
	}
	if body.DatePrecision == "day" && len(body.BusinessDate) > 10 {
		body.BusinessDate = body.BusinessDate[:10]
	}
	res, err := s.ledger.Post(ledger.PostInput{
		LedgerID:      m.ledgerID,
		ActorID:       sess.UserID,
		Type:          body.Type,
		BusinessDate:  body.BusinessDate,
		DatePrecision: body.DatePrecision,
		AmountCents:   cents,
		CategoryID:    body.CategoryID,
		Splits:        splits,
		FromAccountID: body.FromAccountID,
		ToAccountID:   body.ToAccountID,
		Note:          body.Note,
		Merchant:      body.Merchant,
		Channel:       body.Channel,
		OperationID:   body.OperationID,
		RecurrenceInstanceID: body.RecurrenceInstanceID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) summary(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeErr(w, 400, "invalid_input", "from and to are required")
		return
	}
	sum, err := s.ledger.PeriodSummary(m.ledgerID, from, to)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *server) statsDaily(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeErr(w, 400, "invalid_input", "from and to are required")
		return
	}
	rows, err := s.ledger.DailySums(m.ledgerID, from, to)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": rows})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (s *server) mustMembership(w http.ResponseWriter, r *http.Request) *memberInfo {
	sess := auth.SessionFrom(r.Context())
	m, err := s.membership(sess.UserID)
	if err != nil {
		writeError(w, err)
		return nil
	}
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeError(w http.ResponseWriter, err error) {
	var le *ledger.Error
	if errors.As(err, &le) {
		writeErr(w, le.HTTP, le.Code, le.Message)
		return
	}
	writeErr(w, 500, "internal_error", "internal error")
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	// 业务层逐字段校验；不在此处拒绝未知字段（离线队列操作会携带
	// created_at/device 等客户端元数据）
	if err := dec.Decode(v); err != nil {
		return &ledger.Error{Code: "invalid_input", Message: "invalid JSON body", HTTP: 400}
	}
	return nil
}

func clientIP(r *http.Request) string {
	// Only the direct peer is trusted; forwarded headers are honored only
	// when a trusted proxy is explicitly configured (not yet implemented).
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

// securityHeaders applies baseline security headers. API responses carry
// no-store; static asset caching is configured at the static handler.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects cross-site mutating requests (CSRF defense together
// with SameSite=Lax cookies). Production is same-origin; additional
// origins (e.g. a dev server proxying the API) must be explicitly
// configured via XIAOZHANG_ALLOWED_ORIGINS.
func sameOrigin(next http.Handler) http.Handler {
	extra := map[string]bool{}
	for _, o := range strings.Split(config.Getenv("XIAOZHANG_ALLOWED_ORIGINS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			extra[o] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if origin != "" && !strings.HasSuffix(origin, "://"+r.Host) && !extra[origin] {
				writeErr(w, 403, "forbidden_origin", "cross-origin request rejected")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
