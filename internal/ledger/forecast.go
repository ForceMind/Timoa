package ledger

import (
	"time"
)

// forecast.go: 现金流预测（spec 9.4）。固定计划、已确认未来事项与
// 历史估计分开显示，来源可追溯；数据不足显示不足，不伪造精确预测。

type Forecast struct {
	Month              string `json:"month"`
	ElapsedDays        int    `json:"elapsed_days"`
	TotalDays          int    `json:"total_days"`
	SpentCents         string `json:"spent_cents"`          // 本期已确认净支出
	RemainingFixed     string `json:"remaining_fixed_cents"` // 剩余固定费用（本月待确认/部分已记周期）
	VariableEstimate   string `json:"variable_estimate_cents"` // 剩余可变费用估计（剔除固定项的日均 × 剩余天）
	ExpectedEndExpense string `json:"expected_end_expense_cents"` // 预计月底净支出
	AvailableFunds     string `json:"available_funds_cents"`      // 当前已记录可用资金
	ExpectedIncome     string `json:"expected_income_cents"`      // 已确认未来收入（本月未来日期）
	ExpectedEndFunds   string `json:"expected_end_funds_cents"`   // 预计月底可用资金
	Gap                bool   `json:"gap"`                          // 预计缺口
	Insufficient       bool   `json:"insufficient"`                 // 数据不足（<7 天样本）
	Basis              string `json:"basis"`
}

func (s *Service) Forecast(ledgerID string, now time.Time) (*Forecast, error) {
	y, mo, _ := now.Date()
	first := time.Date(y, mo, 1, 0, 0, 0, 0, time.UTC)
	next := first.AddDate(0, 1, 0)
	totalDays := int(next.Sub(first).Hours() / 24)
	elapsed := now.Day()
	from := first.Format("2006-01-02")
	to := next.Format("2006-01-02")
	today := now.Format("2006-01-02")

	f := &Forecast{Month: first.Format("2006-01"), ElapsedDays: elapsed, TotalDays: totalDays}

	ov, err := s.Overview(ledgerID, from, to)
	if err != nil {
		return nil, err
	}
	f.SpentCents = ov.NetExpenseCents

	// 剩余固定费用：本月周期实例（待确认/部分已记/延后）的计划余额
	var remainingFixed int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(i.planned_amount_cents - i.confirmed_amount_cents),0)
		FROM recurrence_instances i JOIN recurrence_rules r ON r.id=i.rule_id
		WHERE i.ledger_id=? AND i.status IN ('pending','partial','postponed')
		AND i.planned_amount_cents IS NOT NULL AND r.tx_type='expense' AND r.include_in_forecast=1
		AND i.planned_date>=? AND i.planned_date<?`,
		ledgerID, from, to).Scan(&remainingFixed); err != nil {
		return nil, err
	}
	f.RemainingFixed = i64s(remainingFixed)

	// 已发生的固定支出（关联周期实例的已入账金额），用于从已花中剔除固定项
	var fixedSpent int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transactions t
		WHERE t.ledger_id=? AND t.recurrence_instance_id IS NOT NULL AND t.type='expense' AND `+effectiveClause+`
		AND t.business_date>=? AND t.business_date<?`, ledgerID, from, to).Scan(&fixedSpent); err != nil {
		return nil, err
	}

	spent, _ := parseI64(ov.NetExpenseCents)
	variableSpent := spent - fixedSpent
	if variableSpent < 0 {
		variableSpent = 0
	}
	remainingDays := totalDays - elapsed
	var estimate int64
	if elapsed >= 7 {
		// 简单、确定、可解释：变动日均 × 剩余天数
		estimate = variableSpent * int64(remainingDays) / int64(elapsed)
	} else {
		f.Insufficient = true // 数据不足：只展示已知计划合计
	}
	f.VariableEstimate = i64s(estimate)

	expectedEnd := spent + remainingFixed + estimate
	f.ExpectedEndExpense = i64s(expectedEnd)

	// 当前可用资金：纳入可用资金的资产账户 - 负债账户余额
	var avail int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN su.kind='asset' THEN
			a.opening_balance_cents + COALESCE((SELECT SUM(CASE WHEN e.direction='debit' THEN e.amount_cents ELSE -e.amount_cents END)
				FROM entries e JOIN transactions t ON t.id=e.tx_id WHERE e.account_id=a.id AND t.status='posted'),0)
			ELSE 0 END),0)
		FROM accounts a JOIN subjects su ON su.id=a.subject_id
		WHERE a.ledger_id=? AND a.include_in_funds=1 AND a.archived_at IS NULL`, ledgerID).Scan(&avail); err != nil {
		return nil, err
	}
	var debts int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(a.opening_balance_cents - COALESCE((SELECT SUM(CASE WHEN e.direction='debit' THEN e.amount_cents ELSE -e.amount_cents END)
			FROM entries e JOIN transactions t ON t.id=e.tx_id WHERE e.account_id=a.id AND t.status='posted'),0)),0)
		FROM accounts a JOIN subjects su ON su.id=a.subject_id
		WHERE a.ledger_id=? AND a.include_in_funds=1 AND a.archived_at IS NULL AND su.kind='liability'`, ledgerID).Scan(&debts); err != nil {
		return nil, err
	}
	avail -= debts
	f.AvailableFunds = i64s(avail)

	// 已确认未来收入（本月未来日期的有效收入）
	var futureIncome int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transactions t
		WHERE t.ledger_id=? AND t.type='income' AND `+effectiveClause+`
		AND substr(t.business_date,1,10)>? AND t.business_date<?`, ledgerID, today, to).Scan(&futureIncome); err != nil {
		return nil, err
	}
	f.ExpectedIncome = i64s(futureIncome)

	endFunds := avail + futureIncome - remainingFixed - estimate
	f.ExpectedEndFunds = i64s(endFunds)
	f.Gap = endFunds < 0
	f.Basis = "预计月底净支出 = 已确认净支出 + 剩余固定费用 + 剩余可变估计（变动日均×剩余天）；固定计划与历史估计分开显示"
	return f, nil
}
