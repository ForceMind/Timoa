package httpapi

import (
	"net/http"
	"strings"

	"xiaozhang/internal/ledger"
	"xiaozhang/internal/money"
)

// adjust_handlers.go: 退款、收入退回、结算、重分类、核销、更正/作废与统计接口。

func (s *server) txDetail(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	d, err := s.ledger.TxDetail(m.ledgerID, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type allocIn struct {
	SplitID string `json:"split_id"`
	Amount  string `json:"amount"`
}

func (s *server) refund(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BusinessDate string    `json:"business_date"`
		AccountID    string    `json:"account_id"`
		Allocations  []allocIn `json:"allocations"`
		Note         string    `json:"note"`
		OperationID  string    `json:"operation_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	var allocs []ledger.RefundAlloc
	for _, a := range body.Allocations {
		c, err := money.ParseYuanRequired(a.Amount)
		if err != nil {
			writeErr(w, 400, "invalid_amount", "allocation: "+err.Error())
			return
		}
		allocs = append(allocs, ledger.RefundAlloc{SplitID: a.SplitID, AmountCents: c})
	}
	res, err := s.ledger.Refund(ledger.RefundInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), OriginalID: r.PathValue("id"),
		BusinessDate: orToday(body.BusinessDate), AccountID: body.AccountID,
		Allocations: allocs, Note: body.Note, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) incomeRefund(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BusinessDate string `json:"business_date"`
		AccountID    string `json:"account_id"`
		Amount       string `json:"amount"`
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
	res, err := s.ledger.IncomeRefund(ledger.IncomeRefundInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), OriginalID: r.PathValue("id"),
		BusinessDate: orToday(body.BusinessDate), AccountID: body.AccountID,
		AmountCents: c, Note: body.Note, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) settle(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BusinessDate string `json:"business_date"`
		AccountID    string `json:"account_id"`
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
	res, err := s.ledger.Settle(ledger.SettlementInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), OriginalID: r.PathValue("id"),
		BusinessDate: orToday(body.BusinessDate), AccountID: body.AccountID,
		AmountCents: c, Counterparty: body.Counterparty, Note: body.Note, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) reclass(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BusinessDate string `json:"business_date"`
		Amount       string `json:"amount"`
		Counterparty string `json:"counterparty"`
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
	res, err := s.ledger.Reclass(ledger.ReclassInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), OriginalID: r.PathValue("id"),
		BusinessDate: orToday(body.BusinessDate), AmountCents: c,
		Counterparty: body.Counterparty, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) writeoff(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		BusinessDate string `json:"business_date"`
		Amount       string `json:"amount"`
		Reason       string `json:"reason"`
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
	res, err := s.ledger.Writeoff(ledger.WriteoffInput{
		LedgerID: m.ledgerID, ActorID: actorID(r), OriginalID: r.PathValue("id"),
		BusinessDate: orToday(body.BusinessDate), AmountCents: c,
		Reason: body.Reason, OperationID: body.OperationID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *server) revise(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Reason      string `json:"reason"`
		Replacement *struct {
			Type          string `json:"type"`
			BusinessDate  string `json:"business_date"`
			Amount        string `json:"amount"`
			CategoryID    string `json:"category_id"`
			FromAccountID string `json:"from_account_id"`
			ToAccountID   string `json:"to_account_id"`
			Note          string `json:"note"`
		} `json:"replacement"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	var repl *ledger.PostInput
	if body.Replacement != nil {
		c, err := money.ParseYuanRequired(body.Replacement.Amount)
		if err != nil {
			writeErr(w, 400, "invalid_amount", err.Error())
			return
		}
		repl = &ledger.PostInput{
			Type: body.Replacement.Type, BusinessDate: body.Replacement.BusinessDate,
			AmountCents: c, CategoryID: body.Replacement.CategoryID,
			FromAccountID: body.Replacement.FromAccountID, ToAccountID: body.Replacement.ToAccountID,
			Note: body.Replacement.Note, OperationID: newOpID(),
		}
	}
	res, err := s.ledger.Revise(m.ledgerID, actorID(r), r.PathValue("id"), body.Reason, repl)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// ---------------------------------------------------------------------------
// 统计
// ---------------------------------------------------------------------------

func (s *server) statsOverview(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeErr(w, 400, "invalid_input", "from and to are required")
		return
	}
	ov, err := s.ledger.Overview(m.ledgerID, from, to)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

func (s *server) statsCategories(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	q := r.URL.Query()
	from, to := q.Get("from"), q.Get("to")
	basis := q.Get("basis")
	if basis == "" {
		basis = "accrual"
	}
	if basis != "accrual" && basis != "origin" {
		writeErr(w, 400, "invalid_input", "basis must be accrual or origin")
		return
	}
	nets, err := s.ledger.CategoryNets(m.ledgerID, from, to, basis)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"basis": basis, "categories": nets})
}

func (s *server) receivables(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	list, err := s.ledger.Receivables(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"receivables": list})
}

// ---------------------------------------------------------------------------

func actorID(r *http.Request) string {
	if sess := sessionOf(r); sess != nil {
		return sess.UserID
	}
	return ""
}

func orToday(s string) string {
	if strings.TrimSpace(s) != "" {
		return s
	}
	return timeNow().Format("2006-01-02")
}
