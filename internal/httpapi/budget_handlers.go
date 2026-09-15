package httpapi

import (
	"net/http"

	"xiaozhang/internal/money"
)

// budget_handlers.go: 预算、储蓄目标、资产负债、推荐记忆。

func (s *server) setBudget(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Month      string `json:"month"`
		CategoryID string `json:"category_id"`
		Amount     string `json:"amount"`
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
	b, err := s.ledger.SetBudget(m.ledgerID, actorID(r), body.Month, body.CategoryID, c)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *server) budgetStatus(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	month := r.URL.Query().Get("month")
	if month == "" {
		writeErr(w, 400, "invalid_input", "month is required")
		return
	}
	st, err := s.ledger.BudgetStatusFor(m.ledgerID, month)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"budgets": st})
}

func (s *server) deleteBudget(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	if err := s.ledger.DeleteBudget(m.ledgerID, r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) createGoal(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Name       string `json:"name"`
		Target     string `json:"target"`
		TargetDate string `json:"target_date"`
		AccountID  string `json:"account_id"`
		Note       string `json:"note"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	c, err := money.ParseYuanRequired(body.Target)
	if err != nil {
		writeErr(w, 400, "invalid_amount", err.Error())
		return
	}
	g, err := s.ledger.CreateSavingsGoal(m.ledgerID, body.Name, c, body.TargetDate, body.AccountID, body.Note)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *server) listGoals(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	gs, err := s.ledger.ListSavingsGoals(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": gs})
}

func (s *server) doneGoal(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Done bool `json:"done"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.ledger.SetSavingsGoalDone(m.ledgerID, r.PathValue("id"), body.Done); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) assetsOverview(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	a, err := s.ledger.AssetsOverview(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *server) dismissRecommendation(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Kind string `json:"kind"`
		Key  string `json:"key"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := s.ledger.DismissRecommendation(m.ledgerID, body.Kind, body.Key); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) merchantSuggest(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	sug, err := s.ledger.SuggestMerchant(m.ledgerID, r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestion": sug})
}
