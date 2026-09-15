package ledger

import (
	"database/sql"
	"fmt"
	"time"
)

// stats.go: 统一的服务端账务定义。报表、列表、导出与 UI 全部从这里
// 取数，不各自实现互不一致的金额算法。

// Overview 是收支概览（spec 9.2.1）：原收入、收入退回、净收入、
// 原费用、退款、净支出、收支结余。发生期口径。
type Overview struct {
	GrossIncomeCents   string `json:"gross_income_cents"`
	IncomeReturnsCents string `json:"income_returns_cents"`
	NetIncomeCents     string `json:"net_income_cents"`
	GrossExpenseCents  string `json:"gross_expense_cents"`
	RefundsCents       string `json:"refunds_cents"`
	NetExpenseCents    string `json:"net_expense_cents"`
	BalanceCents       string `json:"balance_cents"`
}

// Overview 口径：
//   - 原收入/原费用：有效收入/费用交易合计（拆分单按费用部分计入原费用）
//   - 收入退回/退款：区间内有效退回/退款（退款的应收部分不算费用退款）
//   - 净支出另体现已确认重分类（转出为应收减少费用，核销再转回费用）
func (s *Service) Overview(ledgerID, from, to string) (*Overview, error) {
	var grossInc, incRet, grossExp, refunds, reclassOut, writeoffIn int64

	if err := s.db.QueryRow(`SELECT COALESCE(SUM(t.amount_cents),0) FROM transactions t
		WHERE t.ledger_id=? AND t.type='income' AND `+effectiveClause+`
		AND t.business_date>=? AND t.business_date<?`, ledgerID, from, to).Scan(&grossInc); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(t.amount_cents),0) FROM transactions t
		WHERE t.ledger_id=? AND t.type='income_refund' AND `+effectiveClause+`
		AND t.business_date>=? AND t.business_date<?`, ledgerID, from, to).Scan(&incRet); err != nil {
		return nil, err
	}

	// 费用类金额走费用科目分录（拆分单只有费用部分进费用科目）
	expQ := func(dir string, types ...string) (int64, error) {
		q := `SELECT COALESCE(SUM(e.amount_cents),0) FROM entries e
			JOIN transactions t ON t.id=e.tx_id
			JOIN subjects su ON su.id=e.subject_id
			WHERE t.ledger_id=? AND su.kind='expense' AND e.direction=? AND ` + effectiveClause + `
			AND t.business_date>=? AND t.business_date<?`
		args := []any{ledgerID, dir, from, to}
		if len(types) > 0 {
			q += ` AND t.type IN (` + placeholders(len(types)) + `)`
			for _, ty := range types {
				args = append(args, ty)
			}
		}
		var n int64
		err := s.db.QueryRow(q, args...).Scan(&n)
		return n, err
	}
	var err error
	if grossExp, err = expQ("debit", "expense", "writeoff"); err != nil {
		return nil, err
	}
	// 原费用只含 expense；writeoff 是核销转回，单列进净支出
	var grossExpOnly int64
	if grossExpOnly, err = expQ("debit", "expense"); err != nil {
		return nil, err
	}
	if writeoffIn, err = expQ("debit", "writeoff"); err != nil {
		return nil, err
	}
	if refunds, err = expQ("credit", "refund"); err != nil {
		return nil, err
	}
	if reclassOut, err = expQ("credit", "reclass"); err != nil {
		return nil, err
	}
	grossExp = grossExpOnly

	netInc, err := money2Sub(grossInc, incRet)
	if err != nil {
		return nil, err
	}
	netExp, err := money2Sub(grossExp, refunds)
	if err != nil {
		return nil, err
	}
	if netExp, err = money2Sub(netExp, reclassOut); err != nil {
		return nil, err
	}
	if netExp, err = money2Add(netExp, writeoffIn); err != nil {
		return nil, err
	}
	bal, err := money2Sub(netInc, netExp)
	if err != nil {
		return nil, err
	}
	return &Overview{
		GrossIncomeCents:   i64s(grossInc),
		IncomeReturnsCents: i64s(incRet),
		NetIncomeCents:     i64s(netInc),
		GrossExpenseCents:  i64s(grossExp),
		RefundsCents:       i64s(refunds),
		NetExpenseCents:    i64s(netExp),
		BalanceCents:       i64s(bal),
	}, nil
}

// CategoryNet：两级分类净额（退款后）。basis:
//   - "accrual" 发生期：退款/重分类按自身发生日计入所在期间
//   - "origin"  原消费归属：退款/重分类归到原消费所在期间（截至 to）
type CategoryNet struct {
	CategoryID   string `json:"category_id"`
	CategoryName string `json:"category_name"`
	ParentID     string `json:"parent_id,omitempty"`
	NetCents     string `json:"net_cents"`
}

func (s *Service) CategoryNets(ledgerID, from, to, basis string) ([]CategoryNet, error) {
	var q string
	if basis == "origin" {
		// 原消费归属：费用按原单日；退款/重分类/核销归到其原单所在期间。
		// 调整计入条件为调整日不超过「统计截止日」（当天，口径为截至当前
		// 的更正后结果，不虚构过去报表快照）。
		asOf := nowUTC().Format("2006-01-02")
		q = `SELECT su.code, SUM(CASE WHEN e.direction='debit' THEN e.amount_cents ELSE -e.amount_cents END)
			FROM entries e
			JOIN transactions t ON t.id=e.tx_id
			JOIN subjects su ON su.id=e.subject_id AND su.kind='expense'
			LEFT JOIN transactions o ON o.id=t.link_id
			WHERE t.ledger_id=? AND ` + effectiveClause + `
			AND (
				(t.type='expense' AND substr(t.business_date,1,10)>=? AND substr(t.business_date,1,10)<?)
				OR (t.type IN ('refund','reclass','writeoff') AND substr(t.business_date,1,10)<=? AND substr(o.business_date,1,10)>=? AND substr(o.business_date,1,10)<?)
			)
			GROUP BY su.code`
		return s.categoryNetsQuery(ledgerID, q, ledgerID, from, to, asOf, from, to)
	}
	// 发生期
	q = `SELECT su.code, SUM(CASE WHEN e.direction='debit' THEN e.amount_cents ELSE -e.amount_cents END)
		FROM entries e
		JOIN transactions t ON t.id=e.tx_id
		JOIN subjects su ON su.id=e.subject_id AND su.kind='expense'
		WHERE t.ledger_id=? AND ` + effectiveClause + `
		AND t.business_date>=? AND t.business_date<?
		GROUP BY su.code`
	return s.categoryNetsQuery(ledgerID, q, ledgerID, from, to)
}

func (s *Service) categoryNetsQuery(ledgerID, q string, args ...any) ([]CategoryNet, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nets := map[string]int64{}
	for rows.Next() {
		var code string
		var n int64
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		nets[code] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cats, err := s.ListCategories(ledgerID, "expense")
	if err != nil {
		return nil, err
	}
	out := []CategoryNet{}
	for _, c := range cats {
		n, ok := nets["expense:cat:"+c.ID]
		if !ok {
			continue
		}
		out = append(out, CategoryNet{CategoryID: c.ID, CategoryName: c.Name, ParentID: c.ParentID, NetCents: i64s(n)})
	}
	return out, nil
}

// ReceivableView：一笔应收往来的状态（待收、已收、核销、账龄）。
type ReceivableView struct {
	OriginalID    string `json:"original_tx_id"`
	Counterparty  string `json:"counterparty"`
	BusinessDate  string `json:"business_date"`
	CreatedCents  string `json:"created_cents"`
	SettledCents  string `json:"settled_cents"`
	WrittenOff    string `json:"written_off_cents"`
	RefundedCents string `json:"refunded_cents"`
	Outstanding   string `json:"outstanding_cents"`
	AgeDays       int    `json:"age_days"`
	FullySettled  bool   `json:"fully_settled"`
}

// Receivables 列出账本的应收往来（含已结清，供账龄与部分结清展示）。
func (s *Service) Receivables(ledgerID string) ([]ReceivableView, error) {
	// 来源：应收拆分 或 重分类
	rows, err := s.db.Query(`
		SELECT id, business_date, COALESCE(counterparty,'') FROM (
			SELECT t.id, t.business_date,
				(SELECT sp.counterparty FROM transaction_splits sp WHERE sp.tx_id=t.id AND sp.part_type='receivable' LIMIT 1) AS counterparty
			FROM transactions t WHERE t.ledger_id=? AND t.type='expense' AND `+effectiveClause+`
			AND EXISTS (SELECT 1 FROM transaction_splits sp WHERE sp.tx_id=t.id AND sp.part_type='receivable')
			UNION
			SELECT o.id, o.business_date, (SELECT r.counterparty FROM transactions r WHERE r.link_id=o.id AND r.type='reclass' LIMIT 1)
			FROM transactions o WHERE o.ledger_id=? AND EXISTS (
				SELECT 1 FROM transactions r WHERE r.link_id=o.id AND r.type='reclass')
		) ORDER BY business_date DESC`, ledgerID, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReceivableView
	for rows.Next() {
		var v ReceivableView
		if err := rows.Scan(&v.OriginalID, &v.BusinessDate, &v.Counterparty); err != nil {
			return nil, err
		}
		sums, err := s.receivableSums(ledgerID, v.OriginalID)
		if err != nil {
			return nil, err
		}
		v.CreatedCents = i64s(sums.created)
		v.SettledCents = i64s(sums.settled)
		v.WrittenOff = i64s(sums.writtenOff)
		v.RefundedCents = i64s(sums.refunded)
		outst := sums.created - sums.settled - sums.writtenOff - sums.refunded
		v.Outstanding = i64s(outst)
		v.FullySettled = outst == 0
		if d, err := time.Parse("2006-01-02", v.BusinessDate[:min(10, len(v.BusinessDate))]); err == nil {
			v.AgeDays = int(time.Since(d).Hours() / 24)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type receivableSums struct{ created, settled, writtenOff, refunded int64 }

func (s *Service) receivableSums(ledgerID, originalID string) (receivableSums, error) {
	var r receivableSums
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transaction_splits
		WHERE tx_id=? AND part_type='receivable'`, originalID).Scan(&r.created); err != nil {
		return r, err
	}
	var reclassed int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transactions t
		WHERE t.link_id=? AND t.type='reclass' AND `+effectiveClause, originalID).Scan(&reclassed); err != nil {
		return r, err
	}
	r.created += reclassed
	if err := s.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN t.type='settlement' THEN t.amount_cents END),0),
		COALESCE(SUM(CASE WHEN t.type='writeoff' THEN t.amount_cents END),0)
		FROM transactions t WHERE t.link_id=? AND t.type IN ('settlement','writeoff') AND `+effectiveClause,
		originalID).Scan(&r.settled, &r.writtenOff); err != nil {
		return r, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(ra.amount_cents),0) FROM refund_allocations ra
		JOIN transactions t ON t.id=ra.refund_tx_id
		JOIN transaction_splits sp ON sp.id=ra.split_id
		WHERE t.link_id=? AND sp.part_type='receivable' AND `+effectiveClause, originalID).Scan(&r.refunded); err != nil {
		return r, err
	}
	return r, nil
}

// ---------------------------------------------------------------------------
// 账单详情
// ---------------------------------------------------------------------------

type SplitView struct {
	ID           string `json:"id"`
	PartType     string `json:"part_type"`
	CategoryID   string `json:"category_id,omitempty"`
	CategoryName string `json:"category_name,omitempty"`
	Counterparty string `json:"counterparty,omitempty"`
	AmountCents  string `json:"amount_cents"`
	Refunded     string `json:"refunded_cents"`
}

type RefundView struct {
	TxID          string `json:"tx_id"`
	BusinessDate  string `json:"business_date"`
	AmountCents   string `json:"amount_cents"`
	AccountName   string `json:"account_name,omitempty"`
	Effective     bool   `json:"effective"`
}

type TxDetail struct {
	TxView
	Splits          []SplitView  `json:"splits,omitempty"`
	Refunds         []RefundView `json:"refunds,omitempty"`
	RefundedTotal   string       `json:"refunded_total_cents"`
	Receivable      *ReceivableView `json:"receivable,omitempty"`
	Reversed        bool         `json:"reversed"`
	Revision        *Revision    `json:"revision,omitempty"`
}

func (s *Service) TxDetail(ledgerID, txID string) (*TxDetail, error) {
	var d TxDetail
	var amount int64
	var reversed int
	err := s.db.QueryRow(`SELECT t.id,t.type,t.status,t.business_date,t.date_precision,t.amount_cents,
		COALESCE(t.category_id,''),COALESCE(c.name,''),
		COALESCE(t.from_account_id,''),COALESCE(fa.name,''),
		COALESCE(t.to_account_id,''),COALESCE(ta.name,''),
		COALESCE(t.note,''),COALESCE(t.merchant,''),COALESCE(t.channel,''),
		t.created_by,t.created_at,
		CASE WHEN EXISTS(SELECT 1 FROM revisions rv WHERE rv.original_tx_id=t.id) THEN 1 ELSE 0 END
		FROM transactions t
		LEFT JOIN categories c ON c.id=t.category_id
		LEFT JOIN accounts fa ON fa.id=t.from_account_id
		LEFT JOIN accounts ta ON ta.id=t.to_account_id
		WHERE t.id=? AND t.ledger_id=?`, txID, ledgerID).
		Scan(&d.ID, &d.Type, &d.Status, &d.BusinessDate, &d.DatePrecision, &amount,
			&d.CategoryID, &d.CategoryName, &d.FromAccountID, &d.FromAccount,
			&d.ToAccountID, &d.ToAccount, &d.Note, &d.Merchant, &d.Channel,
			&d.CreatedBy, &d.CreatedAt, &reversed)
	if err != nil {
		return nil, ErrNotFound
	}
	d.Amount = fmt.Sprintf("%d", amount)
	d.Reversed = reversed == 1

	// 拆分与各项已退
	splitRows, err := s.db.Query(`SELECT sp.id,sp.part_type,COALESCE(sp.category_id,''),COALESCE(c.name,''),
		COALESCE(sp.counterparty,''),sp.amount_cents,
		COALESCE((SELECT SUM(ra.amount_cents) FROM refund_allocations ra
			JOIN transactions rt ON rt.id=ra.refund_tx_id
			WHERE ra.split_id=sp.id AND rt.status='posted' AND rt.type <> 'reversal'
			AND NOT EXISTS(SELECT 1 FROM revisions rv WHERE rv.original_tx_id=rt.id)),0)
		FROM transaction_splits sp LEFT JOIN categories c ON c.id=sp.category_id
		WHERE sp.tx_id=? ORDER BY sp.rowid`, txID)
	if err != nil {
		return nil, err
	}
	defer splitRows.Close()
	for splitRows.Next() {
		var sv SplitView
		var amt, ref int64
		if err := splitRows.Scan(&sv.ID, &sv.PartType, &sv.CategoryID, &sv.CategoryName, &sv.Counterparty, &amt, &ref); err != nil {
			return nil, err
		}
		sv.AmountCents = i64s(amt)
		sv.Refunded = i64s(ref)
		d.Splits = append(d.Splits, sv)
	}

	// 退款列表（含已被冲正的，标注 effective）
	rfRows, err := s.db.Query(`SELECT t.id,t.business_date,t.amount_cents,COALESCE(a.name,''),
		CASE WHEN EXISTS(SELECT 1 FROM revisions rv WHERE rv.original_tx_id=t.id) THEN 0 ELSE 1 END
		FROM transactions t LEFT JOIN accounts a ON a.id=t.to_account_id
		WHERE t.link_id=? AND t.type='refund' ORDER BY t.created_at`, txID)
	if err != nil {
		return nil, err
	}
	defer rfRows.Close()
	var refundedTotal int64
	for rfRows.Next() {
		var rv RefundView
		var amt int64
		var eff int
		if err := rfRows.Scan(&rv.TxID, &rv.BusinessDate, &amt, &rv.AccountName, &eff); err != nil {
			return nil, err
		}
		rv.AmountCents = i64s(amt)
		rv.Effective = eff == 1
		if rv.Effective {
			refundedTotal += amt
		}
		d.Refunds = append(d.Refunds, rv)
	}
	d.RefundedTotal = i64s(refundedTotal)

	// 应收状态
	if sums, err := s.receivableSums(ledgerID, txID); err == nil && sums.created > 0 {
		v := &ReceivableView{
			OriginalID: txID, CreatedCents: i64s(sums.created),
			SettledCents: i64s(sums.settled), WrittenOff: i64s(sums.writtenOff),
			RefundedCents: i64s(sums.refunded),
			Outstanding:  i64s(sums.created - sums.settled - sums.writtenOff - sums.refunded),
		}
		v.FullySettled = v.Outstanding == "0"
		for _, sp := range d.Splits {
			if sp.PartType == "receivable" {
				v.Counterparty = sp.Counterparty
				break
			}
		}
		d.Receivable = v
	}

	rev, err := s.RevisionOf(ledgerID, txID)
	if err != nil {
		return nil, err
	}
	d.Revision = rev
	return &d, nil
}

// ---------------------------------------------------------------------------

func placeholders(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += ","
		}
		s += "?"
	}
	return s
}

func i64s(n int64) string { return fmt.Sprintf("%d", n) }

func money2Add(a, b int64) (int64, error) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, errf(500, "amount_overflow", "aggregation overflow")
	}
	return s, nil
}

func money2Sub(a, b int64) (int64, error) { return money2Add(a, -b) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = sql.ErrNoRows
