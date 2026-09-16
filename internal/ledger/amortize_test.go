package ledger_test

import (
	"testing"

	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
)

// TestAmortization：分摊计划——创建校验、按期计提（尾差落最后一期）、
// 累计恰等于总额、计划取消后不再计提。
func TestAmortization(t *testing.T) {
	e := newTestEnv(t)
	acc := e.account(t, "现金", "cash", 1_000_000_00)
	cat := e.categoryID(t, "expense", "餐饮")
	incCat := e.categoryID(t, "income", "工资薪酬")

	// 原大额支出 1000.01 元（故意取不整除金额验尾差）
	orig := e.post(t, ledger.PostInput{
		Type: "expense", BusinessDate: "2026-01-05", AmountCents: 1000_01,
		CategoryID: cat, FromAccountID: acc.ID, OperationID: ids.New(),
	})

	// 校验：期数 < 2
	if _, err := e.svc.CreateAmortization(e.ledgerID, e.userID, orig.TxID, cat, acc.ID, 1000_01, 1, "2026-01-05"); err == nil {
		t.Fatal("periods=1 accepted")
	}
	// 校验：非费用单
	inc := e.post(t, ledger.PostInput{
		Type: "income", BusinessDate: "2026-01-05", AmountCents: 100_00,
		CategoryID: incCat, ToAccountID: acc.ID, OperationID: ids.New(),
	})
	if _, err := e.svc.CreateAmortization(e.ledgerID, e.userID, inc.TxID, cat, acc.ID, 100_00, 2, "2026-01-05"); err == nil {
		t.Fatal("income tx accepted")
	}

	// 创建：100001 分 / 3 期 → 33333 + 33333 + 33335
	p, err := e.svc.CreateAmortization(e.ledgerID, e.userID, orig.TxID, cat, acc.ID, 1000_01, 3, "2026-01-05")
	if err != nil {
		t.Fatal(err)
	}
	if p.Periods != 3 || p.Status != "active" || p.DonePeriods != 0 {
		t.Fatalf("plan: %+v", p)
	}
	// 一笔原支出只能建立一条计划，且总额不能超过原额。
	if _, err := e.svc.CreateAmortization(e.ledgerID, e.userID, orig.TxID, cat, acc.ID, 1000_01, 3, "2026-01-05"); err == nil {
		t.Fatal("duplicate original transaction accepted")
	}
	other := e.post(t, ledger.PostInput{
		Type: "expense", BusinessDate: "2026-01-06", AmountCents: 10_00,
		CategoryID: cat, FromAccountID: acc.ID, OperationID: ids.New(),
	})
	if _, err := e.svc.CreateAmortization(e.ledgerID, e.userID, other.TxID, cat, acc.ID, 10_01, 2, "2026-01-06"); err == nil {
		t.Fatal("amount above original accepted")
	}

	// 逐期计提
	balBefore := e.balance(t, acc.ID)
	for i := 0; i < 3; i++ {
		got, err := e.svc.RunAmortization(e.ledgerID, e.userID, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.DonePeriods != i+1 {
			t.Fatalf("done: %d want %d", got.DonePeriods, i+1)
		}
	}
	// 第 4 期拒绝
	if _, err := e.svc.RunAmortization(e.ledgerID, e.userID, p.ID); err == nil {
		t.Fatal("4th run accepted")
	}

	// 累计计提恰等于总额（尾差落最后一期）
	var total int64
	e.db.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM amortization_entries WHERE plan_id=?`, p.ID).Scan(&total)
	if total != 1000_01 {
		t.Fatalf("amortized total %d, want 100001", total)
	}

	// 费用入账：余额减少总额
	balAfter := e.balance(t, acc.ID)
	want := balBefore - 1000_01
	if balAfter != want {
		t.Fatalf("balance: got %d want %d", balAfter, want)
	}

	// 计划状态 done
	plans, err := e.svc.ListAmortizations(e.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Status != "done" || plans[0].DonePeriods != 3 {
		t.Fatalf("plans: %+v", plans)
	}

	// 取消后不再计提（使用另一笔原支出）。
	cancelOrig := e.post(t, ledger.PostInput{
		Type: "expense", BusinessDate: "2026-02-01", AmountCents: 100_00,
		CategoryID: cat, FromAccountID: acc.ID, OperationID: ids.New(),
	})
	p2, err := e.svc.CreateAmortization(e.ledgerID, e.userID, cancelOrig.TxID, cat, acc.ID, 100_00, 2, "2026-02-01")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CancelAmortization(e.ledgerID, p2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RunAmortization(e.ledgerID, e.userID, p2.ID); err == nil {
		t.Fatal("cancelled plan ran")
	}
}
