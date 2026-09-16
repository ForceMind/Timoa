package ledger

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"xiaozhang/internal/ids"
)

// recurrence.go: 周期规则与实例。规则负责推荐，预测/待确认与真实账单
// 是不同实体，未经确认不入账。稳定唯一键 = 账本 + 规则 + 周期键；
// 重复扫描与重启回补不重复创建。

type RecurrenceRule struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	TxType       string `json:"tx_type"`
	CategoryID   string `json:"category_id,omitempty"`
	CategoryName string `json:"category_name,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	Frequency    string `json:"frequency"`
	IntervalDays int    `json:"interval_days,omitempty"`
	ByWeekday    int    `json:"by_weekday,omitempty"`
	MonthDay     int    `json:"month_day,omitempty"`
	AnchorDate   string `json:"anchor_date"`
	StartDate    string `json:"start_date"`
	EndDate      string `json:"end_date,omitempty"`
	AmountPolicy string `json:"amount_policy"`
	FixedCents   string `json:"fixed_amount_cents,omitempty"`
	Shared       bool   `json:"shared"`
	SharedSet    bool   `json:"-"` // 区分「未传」与「显式 false」
	Enabled      bool   `json:"enabled"`
	Version      int    `json:"version"`
}

type RecurrenceInstance struct {
	ID             string `json:"id"`
	RuleID         string `json:"rule_id"`
	RuleName       string `json:"rule_name,omitempty"`
	PeriodKey      string `json:"period_key"`
	PlannedDate    string `json:"planned_date"`
	PlannedCents   string `json:"planned_amount_cents,omitempty"`
	Status         string `json:"status"`
	ConfirmedCents string `json:"confirmed_amount_cents"`
	TxType         string `json:"tx_type,omitempty"`
	CategoryID     string `json:"category_id,omitempty"`
	AccountID      string `json:"account_id,omitempty"`
}

// CreateRule 创建周期规则（共享规则一本账一份，不按成员重复）。
func (s *Service) CreateRule(ledgerID string, r RecurrenceRule) (*RecurrenceRule, error) {
	if r.Name == "" || r.AnchorDate == "" || r.StartDate == "" {
		return nil, errf(400, "invalid_input", "name, anchor_date, start_date required")
	}
	if _, err := time.Parse("2006-01-02", r.AnchorDate); err != nil {
		return nil, errf(400, "invalid_input", "invalid anchor_date")
	}
	valid := map[string]bool{"daily": true, "weekly": true, "biweekly": true, "monthly": true, "month_end": true, "quarterly": true, "yearly": true, "every_n_days": true}
	if !valid[r.Frequency] {
		return nil, errf(400, "invalid_input", "unknown frequency")
	}
	if r.Frequency == "every_n_days" && r.IntervalDays <= 0 {
		return nil, errf(400, "invalid_input", "interval_days required for every_n_days")
	}
	if (r.Frequency == "weekly" || r.Frequency == "biweekly") && (r.ByWeekday < 1 || r.ByWeekday > 7) {
		return nil, errf(400, "invalid_input", "by_weekday must be 1..7")
	}
	if r.Frequency == "monthly" && (r.MonthDay < 1 || r.MonthDay > 31) {
		return nil, errf(400, "invalid_input", "month_day must be 1..31")
	}
	if r.TxType == "" {
		r.TxType = "expense"
	}
	r.ID = ids.New()
	r.Enabled = true
	var cat, acct any
	if r.CategoryID != "" {
		cat = r.CategoryID
	}
	if r.AccountID != "" {
		acct = r.AccountID
	}
	var fixed any
	if r.FixedCents != "" {
		var n int64
		if _, err := fmt.Sscan(r.FixedCents, &n); err == nil && n > 0 {
			fixed = n
		}
	}
	shared := 1
	if r.SharedSet && !r.Shared {
		shared = 0
	}
	_, err := s.db.Exec(`INSERT INTO recurrence_rules
		(id,ledger_id,name,tx_type,category_id,account_id,frequency,interval_days,by_weekday,month_day,anchor_date,start_date,end_date,amount_policy,fixed_amount_cents,shared,enabled)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`,
		r.ID, ledgerID, r.Name, r.TxType, cat, acct, r.Frequency, r.IntervalDays, r.ByWeekday, r.MonthDay,
		r.AnchorDate, r.StartDate, nullIfEmpty(r.EndDate), r.AmountPolicy, fixed, shared)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Service) ListRules(ledgerID string) ([]RecurrenceRule, error) {
	rows, err := s.db.Query(`SELECT r.id,r.name,r.tx_type,COALESCE(r.category_id,''),COALESCE(c.name,''),
		COALESCE(r.account_id,''),r.frequency,COALESCE(r.interval_days,0),COALESCE(r.by_weekday,0),COALESCE(r.month_day,0),
		r.anchor_date,r.start_date,COALESCE(r.end_date,''),r.amount_policy,COALESCE(r.fixed_amount_cents,0),
		r.shared,r.enabled,r.version
		FROM recurrence_rules r LEFT JOIN categories c ON c.id=r.category_id
		WHERE r.ledger_id=? ORDER BY r.created_at`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecurrenceRule{}
	for rows.Next() {
		var r RecurrenceRule
		var fixed int64
		var shared, enabled int
		if err := rows.Scan(&r.ID, &r.Name, &r.TxType, &r.CategoryID, &r.CategoryName, &r.AccountID,
			&r.Frequency, &r.IntervalDays, &r.ByWeekday, &r.MonthDay, &r.AnchorDate, &r.StartDate, &r.EndDate,
			&r.AmountPolicy, &fixed, &shared, &enabled, &r.Version); err != nil {
			return nil, err
		}
		if fixed > 0 {
			r.FixedCents = i64s(fixed)
		}
		r.Shared = shared == 1
		r.Enabled = enabled == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetRuleEnabled 启用/停用周期规则（T22 乐观并发）：
// baseVersion > 0 时要求客户端持有的版本与当前一致，否则返回
// version_conflict（409，携带当前版本），不做 last-write-wins 静默覆盖；
// baseVersion = 0 表示调用方未做版本感知（旧客户端兼容路径）。
// 停用保留历史实例，不再生成新实例。
func (s *Service) SetRuleEnabled(ledgerID, ruleID string, enabled bool, baseVersion int) error {
	if baseVersion > 0 {
		var cur int
		err := s.db.QueryRow(`SELECT version FROM recurrence_rules WHERE id=? AND ledger_id=?`, ruleID, ledgerID).Scan(&cur)
		if err != nil {
			return ErrNotFound
		}
		if cur != baseVersion {
			return errf(409, "version_conflict", "rule was changed by someone else (current version %d); refresh and retry", cur)
		}
	}
	res, err := s.db.Exec(`UPDATE recurrence_rules SET enabled=?, version=version+1 WHERE id=? AND ledger_id=?`,
		boolToInt(enabled), ruleID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// 日期推进（每月按日历，不用固定 30 天；短月落月末、下月恢复锚定不漂移；
// 2 月 29 日年度规则在非闰年落 2 月 28 日）
// ---------------------------------------------------------------------------

func clampDay(year, month, day int) time.Time {
	last := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// occurrencesAfter 列出 (after, until] 内的计划日期（含端点 until）。
func occurrencesAfter(r *RecurrenceRule, after, until time.Time) []time.Time {
	anchor, err := time.Parse("2006-01-02", r.AnchorDate)
	if err != nil {
		return nil
	}
	var out []time.Time
	add := func(d time.Time) {
		if d.After(after) && !d.After(until) {
			if r.StartDate != "" {
				if st, _ := time.Parse("2006-01-02", r.StartDate); d.Before(st) {
					return
				}
			}
			if r.EndDate != "" {
				if en, _ := time.Parse("2006-01-02", r.EndDate); d.After(en) {
					return
				}
			}
			out = append(out, d)
		}
	}

	switch r.Frequency {
	case "daily":
		for d := anchor; !d.After(until); d = d.AddDate(0, 0, 1) {
			add(d)
		}
	case "every_n_days":
		for d := anchor; !d.After(until); d = d.AddDate(0, 0, r.IntervalDays) {
			add(d)
		}
	case "weekly", "biweekly":
		step := 7
		if r.Frequency == "biweekly" {
			step = 14
		}
		// 找到锚定后第一个符合星期几的日期
		d := anchor
		want := int(r.ByWeekday % 7) // Go: Sunday=0；我们 1=周一..7=周日
		for int(d.Weekday()) != want {
			d = d.AddDate(0, 0, 1)
		}
		for ; !d.After(until); d = d.AddDate(0, 0, step) {
			add(d)
		}
	case "monthly":
		// 从锚定月起逐月推进：每月独立按锚定日 clamp，短月落月末、下月恢复
		base := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.UTC)
		for m := 0; ; m++ {
			mo := base.AddDate(0, m, 0)
			d := clampDay(mo.Year(), int(mo.Month()), r.MonthDay)
			if d.After(until) {
				break
			}
			add(d)
		}
	case "month_end":
		base := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.UTC)
		for m := 0; ; m++ {
			mo := base.AddDate(0, m+1, 0)
			d := time.Date(mo.Year(), mo.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
			if d.After(until) {
				break
			}
			add(d)
		}
	case "quarterly":
		base := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.UTC)
		day := anchor.Day()
		for m := 0; ; m += 3 {
			mo := base.AddDate(0, m, 0)
			d := clampDay(mo.Year(), int(mo.Month()), day)
			if d.After(until) {
				break
			}
			add(d)
		}
	case "yearly":
		day, mo := anchor.Day(), int(anchor.Month())
		for y := anchor.Year(); ; y++ {
			d := clampDay(y, mo, day) // 2/29 非闰年落 2/28
			if d.After(until) {
				break
			}
			add(d)
		}
	}
	return out
}

// periodKey：稳定周期键。
func periodKey(freq string, d time.Time) string {
	switch freq {
	case "monthly", "month_end":
		return d.Format("2006-01")
	case "quarterly":
		return fmt.Sprintf("%d-Q%d", d.Year(), (int(d.Month())-1)/3+1)
	case "yearly":
		return d.Format("2006")
	default:
		return d.Format("2006-01-02")
	}
}

const (
	scanHorizonDays = 35            // 向前生成窗口
	backfillDays    = 93            // 回补上限：停机久也不一次生成无穷历史
	maxScanPerRule  = 200           // 单规则单次扫描上限
)

// ScanRecurrence 为所有启用规则生成实例（启动时与定时调度调用）。
// 幂等：唯一键 INSERT OR IGNORE，重复扫描不重复创建。
func ScanRecurrence(db *sql.DB, now time.Time) error {
	svc := NewService(db)
	rows, err := db.Query(`SELECT id,ledger_id,name,tx_type,COALESCE(category_id,''),COALESCE(account_id,''),
		frequency,COALESCE(interval_days,0),COALESCE(by_weekday,0),COALESCE(month_day,0),
		anchor_date,start_date,COALESCE(end_date,''),amount_policy,COALESCE(fixed_amount_cents,0),version
		FROM recurrence_rules WHERE enabled=1`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type ruleRow struct {
		RecurrenceRule
		LedgerID string
		Fixed    int64
	}
	var rules []ruleRow
	for rows.Next() {
		var r ruleRow
		var enabled int
		if err := rows.Scan(&r.ID, &r.LedgerID, &r.Name, &r.TxType, &r.CategoryID, &r.AccountID,
			&r.Frequency, &r.IntervalDays, &r.ByWeekday, &r.MonthDay, &r.AnchorDate, &r.StartDate, &r.EndDate,
			&r.AmountPolicy, &r.Fixed, &r.Version); err != nil {
			return err
		}
		_ = enabled
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	until := today.AddDate(0, 0, scanHorizonDays)

	for _, r := range rules {
		rule := r.RecurrenceRule
		// 回补起点：今天 - backfillDays 与 start_date 取较晚
		after := today.AddDate(0, 0, -backfillDays)
		if st, err := time.Parse("2006-01-02", r.StartDate); err == nil && st.After(after) {
			after = st.AddDate(0, 0, -1)
		} else {
			after = after.AddDate(0, 0, -1)
		}
		occs := occurrencesAfter(&rule, after, until)
		if len(occs) > maxScanPerRule {
			occs = occs[len(occs)-maxScanPerRule:]
		}
		for _, d := range occs {
			var planned any
			if r.Fixed > 0 {
				planned = r.Fixed
			}
			if _, err := db.Exec(`INSERT OR IGNORE INTO recurrence_instances
				(id,ledger_id,rule_id,period_key,planned_date,planned_amount_cents,rule_version)
				VALUES(?,?,?,?,?,?,?)`,
				ids.New(), r.LedgerID, r.ID, periodKey(rule.Frequency, d), d.Format("2006-01-02"), planned, r.Version); err != nil {
				return err
			}
		}
	}
	_ = svc
	return nil
}

// PendingInstances 列出待确认/部分已记实例（应用内基础提醒）。
func (s *Service) PendingInstances(ledgerID string, today time.Time) ([]RecurrenceInstance, error) {
	rows, err := s.db.Query(`SELECT i.id,i.rule_id,r.name,i.period_key,i.planned_date,
		COALESCE(i.planned_amount_cents,0),i.status,i.confirmed_amount_cents,r.tx_type,
		COALESCE(r.category_id,''),COALESCE(r.account_id,'')
		FROM recurrence_instances i JOIN recurrence_rules r ON r.id=i.rule_id
		WHERE i.ledger_id=? AND i.status IN ('pending','partial','postponed')
		AND i.planned_date<=? AND r.enabled=1
		ORDER BY i.planned_date`, ledgerID, today.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecurrenceInstance{}
	for rows.Next() {
		var v RecurrenceInstance
		var planned, confirmed int64
		if err := rows.Scan(&v.ID, &v.RuleID, &v.RuleName, &v.PeriodKey, &v.PlannedDate,
			&planned, &v.Status, &confirmed, &v.TxType, &v.CategoryID, &v.AccountID); err != nil {
			return nil, err
		}
		if planned > 0 {
			v.PlannedCents = i64s(planned)
		}
		v.ConfirmedCents = i64s(confirmed)
		out = append(out, v)
	}
	return out, rows.Err()
}

// ConfirmInstance 在记账过账后由系统调用（同事务）：关联金额累计；
// 固定金额分次支付按关联金额减少剩余计划；未知金额需明确确认完成。
func confirmInstanceTx(tx *sql.Tx, instanceID string, amount int64) error {
	var planned sql.NullInt64
	var status string
	var confirmed int64
	if err := tx.QueryRow(`SELECT planned_amount_cents,status,confirmed_amount_cents
		FROM recurrence_instances WHERE id=?`, instanceID).Scan(&planned, &status, &confirmed); err != nil {
		return err
	}
	if status == "done" || status == "skipped" {
		return errf(409, "recurrence_already_processed", "instance already processed")
	}
	confirmed += amount
	newStatus := "partial"
	if !planned.Valid {
		newStatus = "done" // 未知金额：一次关联即确认完成
	} else if confirmed >= planned.Int64 {
		newStatus = "done"
	}
	_, err := tx.Exec(`UPDATE recurrence_instances SET confirmed_amount_cents=?, status=? WHERE id=?`,
		confirmed, newStatus, instanceID)
	return err
}

// SkipInstance 本期跳过（明确动作，不当作已付）。
func (s *Service) SkipInstance(ledgerID, instanceID string) error {
	res, err := s.db.Exec(`UPDATE recurrence_instances SET status='skipped'
		WHERE id=? AND ledger_id=? AND status IN ('pending','partial','postponed')`, instanceID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errf(409, "recurrence_already_processed", "instance not pending")
	}
	return nil
}

// PostponeInstance 延后：修改计划日期，不生成新的重复实例。
func (s *Service) PostponeInstance(ledgerID, instanceID, newDate string) error {
	if _, err := time.Parse("2006-01-02", newDate); err != nil {
		return errf(400, "invalid_input", "invalid date")
	}
	res, err := s.db.Exec(`UPDATE recurrence_instances SET status='postponed', planned_date=?
		WHERE id=? AND ledger_id=? AND status IN ('pending','partial')`, newDate, instanceID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errf(409, "recurrence_already_processed", "instance not pending")
	}
	return nil
}

var _ = errors.Is
