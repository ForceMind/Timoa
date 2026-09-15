package httpapi

import (
	"net/http"

	"xiaozhang/internal/ledger"
	"xiaozhang/internal/money"
)

// ops_handlers.go: 资金操作（借贷/房贷/理财/押金）、复制、模板、标签、搜索。

func (s *server) lend(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BusinessDate  string `json:"business_date"`
		FromAccountID string `json:"from_account_id"`
		Amount        string `json:"amount"`
		Counterparty  string `json:"counterparty"`
		Note          string `json:"note"`
		OperationID   string `json:"operation_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	c, err := money.ParseYuanRequired(body.Amount)
	if err != nil {
		writeErr(w, 400, "invalid_amount", err.Error())
		return
	}
	res, err := s.ledger.Lend(ledger.LendInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), BusinessDate: orToday(body.BusinessDate),
		FromAccountID: body.FromAccountID, AmountCents: c,
		Counterparty: body.Counterparty, Note: body.Note, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) borrow(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BusinessDate string `json:"business_date"`
		ToAccountID  string `json:"to_account_id"`
		Amount       string `json:"amount"`
		Counterparty string `json:"counterparty"`
		Note         string `json:"note"`
		OperationID  string `json:"operation_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	c, err := money.ParseYuanRequired(body.Amount)
	if err != nil {
		writeErr(w, 400, "invalid_amount", err.Error())
		return
	}
	res, err := s.ledger.Borrow(ledger.BorrowInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), BusinessDate: orToday(body.BusinessDate),
		ToAccountID: body.ToAccountID, AmountCents: c,
		Counterparty: body.Counterparty, Note: body.Note, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) repay(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		OriginalID    string `json:"original_id"`
		BusinessDate  string `json:"business_date"`
		FromAccountID string `json:"from_account_id"`
		Amount        string `json:"amount"`
		Note          string `json:"note"`
		OperationID   string `json:"operation_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	c, err := money.ParseYuanRequired(body.Amount)
	if err != nil {
		writeErr(w, 400, "invalid_amount", err.Error())
		return
	}
	res, err := s.ledger.Repay(ledger.RepayInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), OriginalID: body.OriginalID,
		BusinessDate: orToday(body.BusinessDate), FromAccountID: body.FromAccountID,
		AmountCents: c, Note: body.Note, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) loanRepay(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BusinessDate      string `json:"business_date"`
		FromAccountID     string `json:"from_account_id"`
		LoanAccountID     string `json:"loan_account_id"`
		Total             string `json:"total"`
		Principal         string `json:"principal"`
		InterestCategory  string `json:"interest_category_id"`
		Note              string `json:"note"`
		OperationID       string `json:"operation_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	total, err := money.ParseYuanRequired(body.Total)
	if err != nil {
		writeErr(w, 400, "invalid_amount", "total: "+err.Error())
		return
	}
	principal, err := money.ParseYuanRequired(body.Principal)
	if err != nil {
		writeErr(w, 400, "invalid_amount", "principal: "+err.Error())
		return
	}
	res, err := s.ledger.LoanRepay(ledger.LoanRepayInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), BusinessDate: orToday(body.BusinessDate),
		FromAccountID: body.FromAccountID, LoanAccountID: body.LoanAccountID,
		TotalCents: total, PrincipalCents: principal,
		InterestCategory: body.InterestCategory, Note: body.Note, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) redeem(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BusinessDate    string `json:"business_date"`
		ToAccountID     string `json:"to_account_id"`
		InvestAccountID string `json:"invest_account_id"`
		Total           string `json:"total"`
		Principal       string `json:"principal"`
		YieldCategory   string `json:"yield_category_id"`
		Note            string `json:"note"`
		OperationID     string `json:"operation_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	total, err := money.ParseYuanRequired(body.Total)
	if err != nil {
		writeErr(w, 400, "invalid_amount", "total: "+err.Error())
		return
	}
	principal, err := money.ParseYuanRequired(body.Principal)
	if err != nil {
		writeErr(w, 400, "invalid_amount", "principal: "+err.Error())
		return
	}
	res, err := s.ledger.Redeem(ledger.RedeemInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), BusinessDate: orToday(body.BusinessDate),
		ToAccountID: body.ToAccountID, InvestAccountID: body.InvestAccountID,
		TotalCents: total, PrincipalCents: principal,
		YieldCategory: body.YieldCategory, Note: body.Note, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) copyTx(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	res, err := s.ledger.CopyTransaction(m.ledgerID, actorID(r), r.PathValue("id"), newOpID())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// ---------------------------------------------------------------------------
// 模板 / 标签 / 搜索
// ---------------------------------------------------------------------------

func (s *server) listTemplates(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	tpls, err := ledger.ListTemplates(s.db, m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": tpls})
}

func (s *server) pinTemplate(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.ledger.SetTemplatePinned(m.ledgerID, r.PathValue("id"), body.Pinned); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) enableTemplate(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.ledger.SetTemplateEnabled(m.ledgerID, r.PathValue("id"), body.Enabled); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) listTags(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	tags, err := s.ledger.ListTags(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (s *server) createTag(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	t, err := s.ledger.CreateTag(m.ledgerID, body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *server) searchTx(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"transactions": []any{}})
		return
	}
	txs, err := s.ledger.SearchTransactions(m.ledgerID, q, 50)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": txs})
}
