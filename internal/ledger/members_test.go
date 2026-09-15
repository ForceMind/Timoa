package ledger_test

import (
	"testing"
	"time"

	"xiaozhang/internal/auth"
	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
)

// T40（部分）: 邀请单次有效、过期/撤销拒绝；成员撤销后拒绝登录与旧会话；
// 普通成员不能更正他人账单。
func TestInviteAndMemberPermissions(t *testing.T) {
	e := newTestEnv(t)

	// 创建邀请
	token, inv, err := e.svc.CreateInvite(e.ledgerID, e.userID)
	if err != nil || token == "" || inv.ID == "" {
		t.Fatalf("create invite: %v", err)
	}
	// 凭邀请注册加入
	memberID, err := e.svc.AcceptInvite(token, "mom", "妈妈", "member-pass-123")
	if err != nil {
		t.Fatal(err)
	}
	// 令牌单次使用
	if _, err := e.svc.AcceptInvite(token, "dad", "爸爸", "member-pass-123"); err == nil {
		t.Fatal("invite reused")
	}
	// 假令牌拒绝
	if _, err := e.svc.AcceptInvite("deadbeef", "x", "x", "member-pass-123"); err == nil {
		t.Fatal("invalid token accepted")
	}

	// 普通成员可记账
	bank := e.account(t, "家庭卡", "bank_card", 0)
	cat := e.categoryID(t, "expense", "餐饮")
	memberTx, err := e.svc.Post(ledger.PostInput{
		LedgerID: e.ledgerID, ActorID: memberID, Type: "expense", BusinessDate: "2026-09-16",
		AmountCents: 3000, CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 普通成员可以更正自己的账单
	if err := e.svc.CanEditTx(e.ledgerID, memberID, "member", memberTx.TxID); err != nil {
		t.Fatalf("member cannot edit own tx: %v", err)
	}
	adminTx := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-16", AmountCents: 1000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})
	if err := e.svc.CanEditTx(e.ledgerID, memberID, "member", adminTx.TxID); err == nil {
		t.Fatal("member can edit admin's tx")
	}
	if err := e.svc.CanEditTx(e.ledgerID, e.userID, "admin", adminTx.TxID); err != nil {
		t.Fatalf("admin cannot edit: %v", err)
	}

	// 成员会话有效 → 撤销后立即失效
	sess, err := auth.CreateSession(e.db, memberID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.LookupSession(e.db, sess); err != nil {
		t.Fatalf("session should be valid: %v", err)
	}
	if err := e.svc.RevokeMember(e.ledgerID, e.userID, memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.LookupSession(e.db, sess); err == nil {
		t.Fatal("revoked member session still valid")
	}
	// 撤销后登录拒绝（archived 检查在登录路径，直接验证用户已归档）
	var archived interface{}
	if err := e.db.QueryRow(`SELECT archived_at FROM users WHERE id=?`, memberID).Scan(&archived); err != nil || archived == nil {
		t.Fatalf("member not archived: %v", err)
	}
}

// 预测：固定计划与历史估计分开；已入账固定项不重复计入。
func TestForecast(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 200000)
	cat := e.categoryID(t, "expense", "居住")

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	// 本月已花 1000（其中房租 2000 关联周期，已确认）
	rule := mustRule(t, e, ledger.RecurrenceRule{
		Name: "房租", TxType: "expense", CategoryID: cat, Frequency: "monthly", MonthDay: 5,
		AnchorDate: "2026-09-05", StartDate: "2026-09-01", AmountPolicy: "fixed", FixedCents: "200000",
	})
	n, _ := time.Parse("2006-01-02", "2026-09-01")
	_ = n
	scanRec(t, e, now)
	p := pendingOn(t, e, "2026-09-06")
	if len(p) != 1 {
		t.Fatalf("pending = %+v", p)
	}
	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-05", AmountCents: 200000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New(), RecurrenceInstanceID: p[0].ID})
	// 另一笔变动支出 1000
	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-10", AmountCents: 100000,
		CategoryID: e.categoryID(t, "expense", "餐饮"), FromAccountID: bank.ID, OperationID: ids.New()})
	// 下月还有一个未付固定项（10 月不在本月范围，不应计入）
	_ = rule

	f, err := e.svc.Forecast(e.ledgerID, now)
	if err != nil {
		t.Fatal(err)
	}
	if f.SpentCents != "300000" {
		t.Fatalf("spent = %s, want 300000", f.SpentCents)
	}
	// 房租已入账 → 剩余固定 = 0（不重复计入）
	if f.RemainingFixed != "0" {
		t.Fatalf("remaining fixed = %s, want 0", f.RemainingFixed)
	}
	// 变动估计：变动已花 100000 / 15 天 × 15 天 = 100000
	if f.VariableEstimate != "100000" {
		t.Fatalf("variable estimate = %s, want 100000", f.VariableEstimate)
	}
	// 预计月底净支出 = 300000 + 0 + 100000 = 400000
	if f.ExpectedEndExpense != "400000" {
		t.Fatalf("expected end = %s", f.ExpectedEndExpense)
	}
	// 可用资金：200000 - 200000 - 100000 = -100000；月底 = -100000 - 0 - 100000 = -200000 → 缺口
	if f.AvailableFunds != "-100000" {
		t.Fatalf("available = %s", f.AvailableFunds)
	}
	if !f.Gap {
		t.Fatalf("expected gap flag")
	}
}

// 日历：账单与周期事项分开展示。
func TestCalendarMonth(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 0)
	cat := e.categoryID(t, "expense", "餐饮")
	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-10", AmountCents: 2800,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})
	mustRule(t, e, ledger.RecurrenceRule{
		Name: "信用卡还款日", TxType: "transfer", Frequency: "monthly", MonthDay: 20,
		AnchorDate: "2026-09-20", StartDate: "2026-09-01",
	})
	scanRec(t, e, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))

	days, err := e.svc.CalendarMonth(e.ledgerID, "2026-09")
	if err != nil {
		t.Fatal(err)
	}
	var d10, d20 *ledger.CalendarDay
	for i := range days {
		if days[i].Date == "2026-09-10" {
			d10 = &days[i]
		}
		if days[i].Date == "2026-09-20" {
			d20 = &days[i]
		}
	}
	if d10 == nil || d10.ExpenseCents != "2800" {
		t.Fatalf("d10 = %+v", d10)
	}
	if d20 == nil || len(d20.Events) != 1 || d20.Events[0].Name != "信用卡还款日" || d20.Events[0].Kind != "instance" {
		t.Fatalf("d20 = %+v", d20)
	}
}

func scanRec(t *testing.T, e *testEnv, now time.Time) {
	t.Helper()
	if err := ledger.ScanRecurrence(e.db, now); err != nil {
		t.Fatal(err)
	}
}
