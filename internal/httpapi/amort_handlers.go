package httpapi

import (
	"net/http"
	"strconv"

	"xiaozhang/internal/ledger"
)

// amort_handlers.go: 分摊计划（分期计提）HTTP 接口。

func (s *server) listAmortizations(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	plans, err := s.ledger.ListAmortizations(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"plans": plans})
}

func (s *server) createAmortization(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		TxID       string `json:"tx_id"`
		CategoryID string `json:"category_id"`
		AccountID  string `json:"account_id"`
		TotalCents string `json:"total_cents"`
		Periods    int    `json:"periods"`
		AnchorDate string `json:"anchor_date"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	total, err := strconv.ParseInt(body.TotalCents, 10, 64)
	if err != nil {
		writeError(w, &ledger.Error{Code: "invalid_input", Message: "total_cents must be integer string", HTTP: 400})
		return
	}
	p, err := s.ledger.CreateAmortization(m.ledgerID, sessionOf(r).UserID, body.TxID, body.CategoryID, body.AccountID, total, body.Periods, body.AnchorDate)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *server) runAmortization(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	p, err := s.ledger.RunAmortization(m.ledgerID, sessionOf(r).UserID, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *server) cancelAmortization(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if err := s.ledger.CancelAmortization(m.ledgerID, r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
