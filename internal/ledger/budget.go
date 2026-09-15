package ledger

import (
	"xiaozhang/internal/ids"
)

// budget.go: 预算与储蓄目标。预算按标明的费用口径计算（净支出，退款
// 调整可让本期剩余增加）；父分类预算覆盖其子分类，父子不机械相加。

type Budget struct {
	ID           string `json:"id"`
	Month        string `json:"month"`
	CategoryID   string `json:"category_id,omitempty"`
	CategoryName string `json:"category_name,omitempty"`
	AmountCents  string `json:"amount_cents"`
}

type BudgetStatus struct {
	Budget
	SpentCents    string `json:"spent_cents"`
	Remaining     string `json:"remaining_cents"`
	Percent       int    `json:"percent"` // 0..999（可超 100 表示超支）
	Over          bool   `json:"over"`
	CoversChildren bool  `json:"covers_children"`
}

// SetBudget 设置某月预算（categoryID 为空 = 总预算）。
func (s *Service) SetBudget(ledgerID, actorID, month, categoryID string, amountCents int64) (*Budget, error) {
	if amountCents <= 0 {
		return nil, ErrInvalidAmount
	}
	if len(month) != 7 {
		return nil, errf(400, "invalid_input", "month must be YYYY-MM")
	}
	var cat any
	if categoryID != "" {
		if err := s.requireCategory(ledgerID, categoryID, "expense"); err != nil {
			return nil, err
		}
		cat = categoryID
	}
	id := ids.New()
	if _, err := s.db.Exec(`INSERT INTO budgets(id,ledger_id,month,category_id,amount_cents,created_by)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(ledger_id,month,category_id) DO UPDATE SET amount_cents=excluded.amount_cents, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		id, ledgerID, month, cat, amountCents, actorID); err != nil {
		return nil, err
	}
	return &Budget{ID: id, Month: month, CategoryID: categoryID, AmountCents: i64s(amountCents)}, nil
}

func (s *Service) DeleteBudget(ledgerID, budgetID string) error {
	res, err := s.db.Exec(`DELETE FROM budgets WHERE id=? AND ledger_id=?`, budgetID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// BudgetStatusFor 返回某月全部预算的使用状态。
func (s *Service) BudgetStatusFor(ledgerID, month string) ([]BudgetStatus, error) {
	rows, err := s.db.Query(`SELECT b.id,b.month,COALESCE(b.category_id,''),COALESCE(c.name,''),b.amount_cents,
		EXISTS(SELECT 1 FROM categories ch WHERE ch.parent_id=b.category_id)
		FROM budgets b LEFT JOIN categories c ON c.id=b.category_id
		WHERE b.ledger_id=? AND b.month=? ORDER BY b.category_id IS NOT NULL, c.name`, ledgerID, month)
	if err != nil {
		return nil, err
	}
	// 单连接数据库：先读完并关闭游标，再做后续统计查询
	type budgetRow struct {
		BudgetStatus
		amount int64
	}
	var brows []budgetRow
	for rows.Next() {
		var br budgetRow
		var covers int
		if err := rows.Scan(&br.ID, &br.Month, &br.CategoryID, &br.CategoryName, &br.amount, &covers); err != nil {
			rows.Close()
			return nil, err
		}
		br.CoversChildren = covers == 1
		brows = append(brows, br)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	from := month + "-01"
	to := nextMonth(month)
	nets, err := s.CategoryNets(ledgerID, from, to, "accrual")
	if err != nil {
		return nil, err
	}
	byCat := map[string]int64{}
	childOf := map[string]string{}
	var totalSpent int64
	for _, n := range nets {
		v, _ := parseI64(n.NetCents)
		byCat[n.CategoryID] += v
		if n.ParentID != "" {
			childOf[n.CategoryID] = n.ParentID
		}
		totalSpent += v
	}
	// 父分类汇总（父子不双算：子分类净额归入父分类一次）
	for c, p := range childOf {
		byCat[p] += byCat[c]
	}

	out := []BudgetStatus{}
	for _, br := range brows {
		st := br.BudgetStatus
		amount := br.amount
		st.AmountCents = i64s(amount)
		spent := totalSpent
		if st.CategoryID != "" {
			spent = byCat[st.CategoryID]
		}
		st.SpentCents = i64s(spent)
		st.Remaining = i64s(amount - spent) // 退款调整可让剩余增加（可为负=超支）
		if amount > 0 {
			st.Percent = int(spent * 100 / amount)
		}
		st.Over = spent > amount
		out = append(out, st)
	}
	return out, nil
}

func nextMonth(month string) string {
	y := int(month[0]-'0')*1000 + int(month[1]-'0')*100 + int(month[2]-'0')*10 + int(month[3]-'0')
	m := int(month[5]-'0')*10 + int(month[6]-'0')
	if m == 12 {
		y, m = y+1, 1
	} else {
		m++
	}
	return sqlMonth(y, m)
}

func sqlMonth(y, m int) string {
	mm := []byte{'0' + byte(m/10), '0' + byte(m%10)}
	yy := []byte{'0' + byte(y/1000), '0' + byte((y/100)%10), '0' + byte((y/10)%10), '0' + byte(y%10)}
	return string(yy) + "-" + string(mm) + "-01"
}

// ---------------------------------------------------------------------------
// 储蓄目标（存款制度）
// ---------------------------------------------------------------------------

type SavingsGoal struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TargetCents string `json:"target_cents"`
	TargetDate  string `json:"target_date,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
	AccountName string `json:"account_name,omitempty"`
	Note        string `json:"note,omitempty"`
	Done        bool   `json:"done"`
	SavedCents  string `json:"saved_cents"`
	Percent     int    `json:"percent"`
}

func (s *Service) CreateSavingsGoal(ledgerID, name string, targetCents int64, targetDate, accountID, note string) (*SavingsGoal, error) {
	if name == "" || targetCents <= 0 {
		return nil, errf(400, "invalid_input", "name and positive target required")
	}
	g := &SavingsGoal{ID: ids.New(), Name: name, TargetCents: i64s(targetCents), TargetDate: targetDate, AccountID: accountID, Note: note}
	var td, acct, nt any
	if targetDate != "" {
		td = targetDate
	}
	if accountID != "" {
		if err := s.requireAccount(ledgerID, accountID); err != nil {
			return nil, err
		}
		acct = accountID
	}
	if note != "" {
		nt = note
	}
	_, err := s.db.Exec(`INSERT INTO savings_goals(id,ledger_id,name,target_cents,target_date,account_id,note)
		VALUES(?,?,?,?,?,?,?)`, g.ID, ledgerID, name, targetCents, td, acct, nt)
	return g, err
}

func (s *Service) ListSavingsGoals(ledgerID string) ([]SavingsGoal, error) {
	rows, err := s.db.Query(`SELECT g.id,g.name,g.target_cents,COALESCE(g.target_date,''),
		COALESCE(g.account_id,''),COALESCE(a.name,''),COALESCE(g.note,''),g.done,
		COALESCE(a.opening_balance_cents,0)
		FROM savings_goals g LEFT JOIN accounts a ON a.id=g.account_id
		WHERE g.ledger_id=? ORDER BY g.done, g.created_at`, ledgerID)
	if err != nil {
		return nil, err
	}
	// 单连接数据库：先读完关闭游标，再逐条查余额
	out := []SavingsGoal{}
	for rows.Next() {
		var g SavingsGoal
		var target, opening int64
		var done int
		if err := rows.Scan(&g.ID, &g.Name, &target, &g.TargetDate, &g.AccountID, &g.AccountName, &g.Note, &done, &opening); err != nil {
			rows.Close()
			return nil, err
		}
		g.TargetCents = i64s(target)
		g.Done = done == 1
		g.SavedCents = "0"
		out = append(out, g)
		_ = opening
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for i := range out {
		g := &out[i]
		if g.AccountID == "" {
			continue
		}
		acct, err := s.GetAccount(ledgerID, g.AccountID)
		if err != nil {
			continue
		}
		bal, _ := parseI64(acct.Balance)
		opening, _ := parseI64(acct.OpeningBalance)
		saved := bal - opening
		if saved < 0 {
			saved = 0
		}
		g.SavedCents = i64s(saved)
		if target, _ := parseI64(g.TargetCents); target > 0 {
			g.Percent = int(saved * 100 / target)
		}
	}
	return out, nil
}

func (s *Service) SetSavingsGoalDone(ledgerID, goalID string, done bool) error {
	res, err := s.db.Exec(`UPDATE savings_goals SET done=? WHERE id=? AND ledger_id=?`, boolToInt(done), goalID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func parseI64(s string) (int64, error) {
	var n int64
	for _, r := range s {
		if r == '-' {
			continue
		}
		if r < '0' || r > '9' {
			return 0, errf(400, "invalid_amount", "bad integer %q", s)
		}
		n = n*10 + int64(r-'0')
	}
	if len(s) > 0 && s[0] == '-' {
		n = -n
	}
	return n, nil
}
