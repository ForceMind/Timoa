package httpapi

import (
	"net/http"
	"time"

	"xiaozhang/internal/ledger"
)

// recurrence_handlers.go: 周期规则、实例与推荐。

func (s *server) listRules(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	rules, err := s.ledger.ListRules(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s *server) createRule(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Name         string `json:"name"`
		TxType       string `json:"tx_type"`
		CategoryID   string `json:"category_id"`
		AccountID    string `json:"account_id"`
		Frequency    string `json:"frequency"`
		IntervalDays int    `json:"interval_days"`
		ByWeekday    int    `json:"by_weekday"`
		MonthDay     int    `json:"month_day"`
		AnchorDate   string `json:"anchor_date"`
		StartDate    string `json:"start_date"`
		EndDate      string `json:"end_date"`
		AmountPolicy string `json:"amount_policy"`
		FixedCents   string `json:"fixed_amount_cents"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	rule, err := s.ledger.CreateRule(m.ledgerID, ledger.RecurrenceRule{
		Name: body.Name, TxType: body.TxType, CategoryID: body.CategoryID, AccountID: body.AccountID,
		Frequency: body.Frequency, IntervalDays: body.IntervalDays, ByWeekday: body.ByWeekday,
		MonthDay: body.MonthDay, AnchorDate: body.AnchorDate, StartDate: body.StartDate, EndDate: body.EndDate,
		AmountPolicy: body.AmountPolicy, FixedCents: body.FixedCents, Shared: true, SharedSet: true,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	// 立即扫描一次，让本期实例立刻可见
	_ = ledger.ScanRecurrence(s.db, time.Now())
	writeJSON(w, http.StatusCreated, rule)
}

func (s *server) enableRule(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Enabled     bool `json:"enabled"`
		BaseVersion int  `json:"base_version"` // T22 乐观并发：0 = 未做版本感知
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.ledger.SetRuleEnabled(m.ledgerID, r.PathValue("id"), body.Enabled, body.BaseVersion); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) pendingInstances(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	list, err := s.ledger.PendingInstances(m.ledgerID, time.Now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": list})
}

func (s *server) skipInstance(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if err := s.ledger.SkipInstance(m.ledgerID, r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) postponeInstance(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Date string `json:"date"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.ledger.PostponeInstance(m.ledgerID, r.PathValue("id"), body.Date); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) recommendations(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	now := time.Now()
	if d := r.URL.Query().Get("date"); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			now = t
		}
	}
	recs, err := s.ledger.Recommend(m.ledgerID, now)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recommendations": recs})
}
