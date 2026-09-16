package ledger

import (
	"time"

	"xiaozhang/internal/ids"
)

// amortize.go: 分摊计划（分期计提）——大额支出按期摊销为费用，
// 每期生成一条正式费用分录并关联原大额支出。
//
// 关键不变量：
// - 金额整数分，尾差落最后一期，累计分摊恰等于总额（不漂移）；
// - 每期独立 Post（复式分录 + 审计），与手工记账同一账务路径；
// - 计划取消不影响已计提期数（历史不可变）。

type AmortizationPlan struct {
	ID           string `json:"id"`
	TxID         string `json:"tx_id"`       // 原大额支出
	CategoryID   string `json:"category_id"` // 摊销费用分类
	CategoryName string `json:"category_name,omitempty"`
	AccountID    string `json:"account_id"` // 摊销费用出账账户
	AccountName  string `json:"account_name,omitempty"`
	TotalCents   string `json:"total_cents"`
	Periods      int    `json:"periods"`
	PeriodCents  string `json:"period_cents"`
	AnchorDate   string `json:"anchor_date"`
	DonePeriods  int    `json:"done_periods"`
	Status       string `json:"status"` // active | done | cancelled
	CreatedAt    string `json:"created_at"`
}

// CreateAmortization 创建分摊计划。
// totalCents 通常取原支出金额（允许小于原额，分期偿还本金等场景）；
// periods >= 2；periodCents = total/periods（整除），尾差 = total - period*periods 落最后一期。
func (s *Service) CreateAmortization(ledgerID, actorID, txID, categoryID, accountID string, totalCents int64, periods int, anchorDate string) (*AmortizationPlan, error) {
	if totalCents <= 0 || periods < 2 || int64(periods) > totalCents || anchorDate == "" {
		return nil, errf(400, "invalid_input", "total_cents >= periods >= 2 and anchor_date required")
	}
	if _, err := time.Parse("2006-01-02", anchorDate); err != nil {
		return nil, errf(400, "invalid_input", "anchor_date must be YYYY-MM-DD")
	}

	s.wm.Lock()
	defer s.wm.Unlock()

	// 校验原支出存在且为本账本费用单。
	var txType string
	var txAmount int64
	err := s.db.QueryRow(`SELECT type,amount_cents FROM transactions WHERE id=? AND ledger_id=? AND status='posted'`,
		txID, ledgerID).Scan(&txType, &txAmount)
	if err != nil {
		return nil, errf(400, "invalid_input", "original transaction not found")
	}
	if txType != "expense" {
		return nil, errf(400, "invalid_input", "amortization requires an expense transaction")
	}
	if totalCents > txAmount {
		return nil, errf(400, "invalid_input", "total_cents exceeds original transaction amount")
	}
	var exists int
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM amortization_plans WHERE ledger_id=? AND tx_id=?)`, ledgerID, txID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 0 {
		return nil, errf(409, "amortization_exists", "original transaction already has an amortization plan")
	}
	// 校验分类与账户
	if err := s.requireCategory(ledgerID, categoryID, "expense"); err != nil {
		return nil, err
	}
	if err := s.requireAccount(ledgerID, accountID); err != nil {
		return nil, err
	}

	period := totalCents / int64(periods)
	p := &AmortizationPlan{
		ID: ids.New(), TxID: txID, CategoryID: categoryID, AccountID: accountID,
		TotalCents: i64s(totalCents), Periods: periods, PeriodCents: i64s(period),
		AnchorDate: anchorDate, Status: "active",
	}
	_, err = s.db.Exec(`INSERT INTO amortization_plans
		(id,ledger_id,tx_id,category_id,account_id,total_cents,periods,period_cents,anchor_date,created_by)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		p.ID, ledgerID, txID, categoryID, accountID, totalCents, periods, period, anchorDate, actorID)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ListAmortizations 列出账本分摊计划（active 在前）。
func (s *Service) ListAmortizations(ledgerID string) ([]AmortizationPlan, error) {
	rows, err := s.db.Query(`SELECT p.id,p.tx_id,p.category_id,COALESCE(c.name,''),p.account_id,COALESCE(a.name,''),
		p.total_cents,p.periods,p.period_cents,p.anchor_date,p.done_periods,p.status,p.created_at
		FROM amortization_plans p
		LEFT JOIN categories c ON c.id=p.category_id
		LEFT JOIN accounts a ON a.id=p.account_id
		WHERE p.ledger_id=? ORDER BY p.status='active' DESC, p.created_at DESC`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AmortizationPlan{}
	for rows.Next() {
		var p AmortizationPlan
		var total, period int64
		if err := rows.Scan(&p.ID, &p.TxID, &p.CategoryID, &p.CategoryName, &p.AccountID, &p.AccountName,
			&total, &p.Periods, &period, &p.AnchorDate, &p.DonePeriods, &p.Status, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.TotalCents = i64s(total)
		p.PeriodCents = i64s(period)
		out = append(out, p)
	}
	return out, rows.Err()
}

// RunAmortization 计提下一期：在一个数据库事务内生成正式费用分录、
// 记录计提关联并累计进度，绝不留下孤立分录。
func (s *Service) RunAmortization(ledgerID, actorID, planID string) (*AmortizationPlan, error) {
	s.wm.Lock()
	defer s.wm.Unlock()

	var p AmortizationPlan
	var total, period int64
	err := s.db.QueryRow(`SELECT id,tx_id,category_id,account_id,total_cents,periods,period_cents,anchor_date,done_periods,status
		FROM amortization_plans WHERE id=? AND ledger_id=?`, planID, ledgerID).
		Scan(&p.ID, &p.TxID, &p.CategoryID, &p.AccountID, &total, &p.Periods, &period, &p.AnchorDate, &p.DonePeriods, &p.Status)
	if err != nil {
		return nil, ErrNotFound
	}
	if p.Status != "active" {
		return nil, errf(400, "invalid_input", "plan is %s", p.Status)
	}
	if p.DonePeriods >= p.Periods {
		return nil, errf(400, "invalid_input", "all periods done")
	}

	// 本期金额：最后一期吃尾差。
	next := p.DonePeriods + 1
	amount := period
	if next == p.Periods {
		amount = total - period*int64(p.Periods-1)
	}
	anchor, _ := time.Parse("2006-01-02", p.AnchorDate)
	postDate := anchor.AddDate(0, next-1, 0).Format("2006-01-02")
	opID := ids.New()
	in := PostInput{
		LedgerID: ledgerID, ActorID: actorID, Type: "expense",
		BusinessDate: postDate, AmountCents: amount,
		CategoryID: p.CategoryID, FromAccountID: p.AccountID,
		Note:        "分摊计提 " + p.AnchorDate + " 第" + i64s(int64(next)) + "期",
		OperationID: opID,
	}
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.postInTx(tx, in); err != nil {
		return nil, err
	}
	var amortTxID string
	if err := tx.QueryRow(`SELECT id FROM transactions WHERE ledger_id=? AND operation_id=?`, ledgerID, opID).Scan(&amortTxID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO amortization_entries(id,plan_id,period_no,tx_id,amount_cents,posted_date)
		VALUES(?,?,?,?,?,?)`, ids.New(), planID, next, amortTxID, amount, postDate); err != nil {
		return nil, err
	}
	newStatus := "active"
	if next >= p.Periods {
		newStatus = "done"
	}
	if _, err := tx.Exec(`UPDATE amortization_plans SET done_periods=?, status=? WHERE id=?`, next, newStatus, planID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	p.DonePeriods = next
	p.Status = newStatus
	p.TotalCents = i64s(total)
	p.PeriodCents = i64s(period)
	return &p, nil
}

// CancelAmortization 取消计划（已计提期数保留，历史不可变）。
func (s *Service) CancelAmortization(ledgerID, planID string) error {
	res, err := s.db.Exec(`UPDATE amortization_plans SET status='cancelled'
		WHERE id=? AND ledger_id=? AND status='active'`, planID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
