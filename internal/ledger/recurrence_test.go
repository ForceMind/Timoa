package ledger_test

import (
	"testing"
	"time"

	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
)

func mustRule(t *testing.T, e *testEnv, r ledger.RecurrenceRule) *ledger.RecurrenceRule {
	t.Helper()
	r.Shared = true
	r.SharedSet = true
	rule, err := e.svc.CreateRule(e.ledgerID, r)
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func scan(t *testing.T, e *testEnv, now string) {
	t.Helper()
	n, err := time.Parse("2006-01-02", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.ScanRecurrence(e.db, n); err != nil {
		t.Fatal(err)
	}
}

func pendingOn(t *testing.T, e *testEnv, date string) []ledger.RecurrenceInstance {
	t.Helper()
	d, _ := time.Parse("2006-01-02", date)
	list, err := e.svc.PendingInstances(e.ledgerID, d)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// T25（部分）: 1 月 31 日锚定的月规则，2 月落月末、3 月恢复 31 日不漂移；闰年 2/29。
func TestT25_MonthAnchorNoDrift(t *testing.T) {
	e := newTestEnv(t)
	cat := e.categoryID(t, "expense", "居住")
	rule := mustRule(t, e, ledger.RecurrenceRule{
		Name: "房租", TxType: "expense", CategoryID: cat, Frequency: "monthly",
		MonthDay: 31, AnchorDate: "2026-01-31", StartDate: "2026-01-01",
	})
	scan(t, e, "2026-01-15")

	var dates []string
	rows, err := e.db.Query(`SELECT planned_date FROM recurrence_instances WHERE rule_id=? ORDER BY planned_date`, rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		rows.Scan(&d)
		dates = append(dates, d)
	}
	// 窗口：2026-01-15 起 35 天内 → 只有 1/31
	if len(dates) != 1 || dates[0] != "2026-01-31" {
		t.Fatalf("jan scan = %v", dates)
	}
	// 推进到 2 月：2 月无 31 日 → 落 2/28；3 月恢复 3/31
	scan(t, e, "2026-02-01")
	scan(t, e, "2026-03-01")
	rows2, _ := e.db.Query(`SELECT planned_date FROM recurrence_instances WHERE rule_id=? ORDER BY planned_date`, rule.ID)
	defer rows2.Close()
	dates = dates[:0]
	for rows2.Next() {
		var d string
		rows2.Scan(&d)
		dates = append(dates, d)
	}
	want := []string{"2026-01-31", "2026-02-28", "2026-03-31"}
	if len(dates) != 3 {
		t.Fatalf("dates = %v, want %v", dates, want)
	}
	for i := range want {
		if dates[i] != want[i] {
			t.Fatalf("dates = %v, want %v（短月落月末且下月恢复锚定）", dates, want)
		}
	}

	// 闰年 2/29 年度规则，非闰年落 2/28
	rule2 := mustRule(t, e, ledger.RecurrenceRule{
		Name: "年度纪念日", TxType: "expense", Frequency: "yearly",
		AnchorDate: "2024-02-29", StartDate: "2024-01-01",
	})
	scan(t, e, "2027-02-01")
	var ydates []string
	rows3, _ := e.db.Query(`SELECT planned_date FROM recurrence_instances WHERE rule_id=? ORDER BY planned_date`, rule2.ID)
	defer rows3.Close()
	for rows3.Next() {
		var d string
		rows3.Scan(&d)
		ydates = append(ydates, d)
	}
	if len(ydates) == 0 || ydates[len(ydates)-1] != "2027-02-28" {
		t.Fatalf("yearly = %v, want 2027-02-28（非闰年策略）", ydates)
	}
}

// T27: 手工记账关联周期后不重复生成；固定金额分次支付扣剩余计划。
func TestT27_ConfirmAndPartial(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	cat := e.categoryID(t, "expense", "居住")
	rule := mustRule(t, e, ledger.RecurrenceRule{
		Name: "房租", TxType: "expense", CategoryID: cat, Frequency: "monthly",
		MonthDay: 5, AnchorDate: "2026-09-05", StartDate: "2026-09-01",
		AmountPolicy: "fixed", FixedCents: "200000",
	})
	scan(t, e, "2026-09-06")
	p := pendingOn(t, e, "2026-09-06")
	if len(p) != 1 || p[0].PlannedCents != "200000" {
		t.Fatalf("pending = %+v", p)
	}
	inst := p[0]

	// 重复扫描不重复创建
	scan(t, e, "2026-09-06")
	scan(t, e, "2026-09-07")
	var count int
	e.db.QueryRow(`SELECT COUNT(1) FROM recurrence_instances WHERE rule_id=?`, rule.ID).Scan(&count)
	if count != 2 { // 9/5 与 10/5（35 天窗口）
		t.Fatalf("instances = %d, want 2 (no duplicates)", count)
	}

	// 分次支付：先付 1200 → partial，剩 800
	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-06", AmountCents: 120000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New(), RecurrenceInstanceID: inst.ID})
	p = pendingOn(t, e, "2026-09-07")
	if len(p) != 1 || p[0].Status != "partial" || p[0].ConfirmedCents != "120000" {
		t.Fatalf("after partial = %+v", p)
	}
	// 再付 800 → done
	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-08", AmountCents: 80000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New(), RecurrenceInstanceID: inst.ID})
	p = pendingOn(t, e, "2026-09-09")
	if len(p) != 0 {
		t.Fatalf("still pending after full payment: %+v", p)
	}
	// 已处理实例再关联返回明确错误
	err := func() error {
		_, err := e.svc.Post(ledger.PostInput{LedgerID: e.ledgerID, ActorID: e.userID,
			Type: "expense", BusinessDate: "2026-09-09", AmountCents: 100,
			CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New(), RecurrenceInstanceID: inst.ID})
		return err
	}()
	if err == nil {
		t.Fatal("linking a done instance accepted")
	} else if le, ok := err.(*ledger.Error); !ok || le.Code != "recurrence_already_processed" {
		t.Fatalf("want recurrence_already_processed, got %v", err)
	}
}

// T28: 跳过、延后、停用规则、重启补查幂等。
func TestT28_SkipPostponeDisable(t *testing.T) {
	e := newTestEnv(t)
	rule := mustRule(t, e, ledger.RecurrenceRule{
		Name: "宽带", TxType: "expense", Frequency: "monthly",
		MonthDay: 10, AnchorDate: "2026-09-10", StartDate: "2026-09-01",
	})
	scan(t, e, "2026-09-01")
	p := pendingOn(t, e, "2026-09-10")
	if len(p) != 1 {
		t.Fatalf("pending = %+v", p)
	}
	// 延后：改计划日期，不生成新实例
	if err := e.svc.PostponeInstance(e.ledgerID, p[0].ID, "2026-09-15"); err != nil {
		t.Fatal(err)
	}
	if got := pendingOn(t, e, "2026-09-10"); len(got) != 0 {
		t.Fatalf("postponed still pending on 9/10")
	}
	if got := pendingOn(t, e, "2026-09-15"); len(got) != 1 {
		t.Fatalf("postponed not pending on 9/15: %+v", got)
	}
	// 跳过
	if err := e.svc.SkipInstance(e.ledgerID, p[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := pendingOn(t, e, "2026-09-20"); len(got) != 0 {
		t.Fatalf("skipped still pending")
	}
	// 跳过不能再操作
	if err := e.svc.SkipInstance(e.ledgerID, p[0].ID); err == nil {
		t.Fatal("double skip accepted")
	}
	// 停用后不再生成
	if err := e.svc.SetRuleEnabled(e.ledgerID, rule.ID, false, 0); err != nil {
		t.Fatal(err)
	}
	scan(t, e, "2026-10-20")
	var cnt int
	e.db.QueryRow(`SELECT COUNT(1) FROM recurrence_instances WHERE rule_id=? AND period_key='2026-10'`, rule.ID).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("disabled rule still generating")
	}
}

// TestT22_RuleVersionConflict：base_version 乐观并发——
// 版本一致正常更新；版本落后返回 version_conflict 且不被覆盖；
// base_version=0 兼容未做版本感知的旧调用。
func TestT22_RuleVersionConflict(t *testing.T) {
	e := newTestEnv(t)
	rule, err := e.svc.CreateRule(e.ledgerID, ledger.RecurrenceRule{
		Name: "房租", TxType: "expense", Frequency: "monthly", MonthDay: 1,
		AnchorDate: "2026-01-01", StartDate: "2026-01-01", AmountPolicy: "manual",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 新建规则版本为 1（建表默认值，CreateRule 不回填结构体字段）
	var ver int
	e.db.QueryRow(`SELECT version FROM recurrence_rules WHERE id=?`, rule.ID).Scan(&ver)
	if ver != 1 {
		t.Fatalf("new rule version = %d, want 1", ver)
	}
	// 版本一致：v1 → v2
	if err := e.svc.SetRuleEnabled(e.ledgerID, rule.ID, false, 1); err != nil {
		t.Fatal(err)
	}
	// 用旧版本 v1 再改：冲突，不生效
	err = e.svc.SetRuleEnabled(e.ledgerID, rule.ID, true, 1)
	le, ok := err.(*ledger.Error)
	if !ok || le.Code != "version_conflict" {
		t.Fatalf("stale base_version: got %v, want version_conflict", err)
	}
	var enabled bool
	e.db.QueryRow(`SELECT enabled, version FROM recurrence_rules WHERE id=?`, rule.ID).Scan(&enabled, &ver)
	if enabled || ver != 2 {
		t.Fatalf("conflict write slipped through: enabled=%v version=%d", enabled, ver)
	}
	// 当前版本 v2 重试成功
	if err := e.svc.SetRuleEnabled(e.ledgerID, rule.ID, true, 2); err != nil {
		t.Fatal(err)
	}
	// base_version=0：兼容路径，不校验
	if err := e.svc.SetRuleEnabled(e.ledgerID, rule.ID, false, 0); err != nil {
		t.Fatal(err)
	}
	// 不存在的规则
	err = e.svc.SetRuleEnabled(e.ledgerID, "no-such-rule", false, 1)
	if le, ok := err.(*ledger.Error); !ok || le.Code != "not_found" {
		t.Fatalf("missing rule: got %v, want not_found", err)
	}
}

// T29: 共享房租规则一本账一份，不按成员重复生成。
func TestT29_SharedRuleNotDuplicated(t *testing.T) {
	e := newTestEnv(t)
	rule := mustRule(t, e, ledger.RecurrenceRule{
		Name: "房租", Frequency: "monthly", MonthDay: 5,
		AnchorDate: "2026-09-05", StartDate: "2026-09-01", Shared: true, SharedSet: true,
	})
	scan(t, e, "2026-09-01")
	scan(t, e, "2026-09-02") // 另一个“成员”触发扫描
	var cnt int
	e.db.QueryRow(`SELECT COUNT(1) FROM recurrence_instances WHERE rule_id=? AND period_key='2026-09'`, rule.ID).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("shared rule instances = %d, want 1", cnt)
	}
}

// 推荐：到期规则优先且给出真实原因。
func TestRecommendDueRuleFirst(t *testing.T) {
	e := newTestEnv(t)
	mustRule(t, e, ledger.RecurrenceRule{
		Name: "房租", TxType: "expense", Frequency: "monthly", MonthDay: 5,
		AnchorDate: "2026-09-05", StartDate: "2026-09-01",
	})
	n, _ := time.Parse("2006-01-02", "2026-09-06")
	scan(t, e, "2026-09-06")
	recs, err := e.svc.Recommend(e.ledgerID, n)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 || recs[0].Kind != "recurrence" {
		t.Fatalf("recs = %+v", recs)
	}
	if recs[0].Reason == "" || len(recs) > 4 {
		t.Fatalf("bad recommendations: %+v", recs)
	}
}
