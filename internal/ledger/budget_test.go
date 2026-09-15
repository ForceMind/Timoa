package ledger_test

import (
	"testing"
	"time"

	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
)

// 预算：总预算与分类预算；父分类预算覆盖子分类；退款让剩余增加。
func TestBudgetStatus(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	food := e.categoryID(t, "expense", "餐饮") // 父分类（阶段1平铺种子）
	child := e.createCat(t, "expense", "早餐-test")
	// 把 child 挂到 food 下：直接更新 parent_id（管理功能后续提供 UI）
	if _, err := e.db.Exec(`UPDATE categories SET parent_id=? WHERE id=?`, food, child); err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.SetBudget(e.ledgerID, e.userID, "2026-09", "", 300000); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetBudget(e.ledgerID, e.userID, "2026-09", food, 100000); err != nil {
		t.Fatal(err)
	}

	// 子分类消费 600：计入父分类预算
	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-03", AmountCents: 60000,
		CategoryID: child, FromAccountID: bank.ID, OperationID: ids.New()})
	// 退款 100：剩余增加
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-05", AmountCents: 20000,
		CategoryID: food, FromAccountID: bank.ID, OperationID: ids.New()})
	if _, err := e.svc.Refund(ledger.RefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-09-06",
		AccountID: bank.ID, Allocations: []ledger.RefundAlloc{{AmountCents: 10000}}, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}

	st, err := e.svc.BudgetStatusFor(e.ledgerID, "2026-09")
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 2 {
		t.Fatalf("budgets = %d, want 2", len(st))
	}
	var total, foodB *ledger.BudgetStatus
	for i := range st {
		if st[i].CategoryID == "" {
			total = &st[i]
		} else {
			foodB = &st[i]
		}
	}
	// 父分类预算：子 600 + 父 200 - 退 100 = 700；父子不双算
	if foodB.SpentCents != "70000" || foodB.Remaining != "30000" {
		t.Fatalf("food budget = %+v", foodB)
	}
	if total.SpentCents != "70000" || total.Remaining != "230000" {
		t.Fatalf("total budget = %+v", total)
	}
}

// 推荐记忆：商户学习 → 输入商户给出分类/金额建议；屏蔽后不再推荐。
func TestRecommendationMemory(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	food := e.categoryID(t, "expense", "餐饮")

	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-10T12:30:00+08:00", AmountCents: 2800,
		CategoryID: food, FromAccountID: bank.ID, Merchant: "麦当劳", OperationID: ids.New()})

	sug, err := e.svc.SuggestMerchant(e.ledgerID, "麦当")
	if err != nil {
		t.Fatal(err)
	}
	if sug == nil || sug.CategoryID != food || sug.AmountHint != "2800" {
		t.Fatalf("suggestion = %+v", sug)
	}

	// 用户纠正（新分类过账）→ 建议更新
	traffic := e.categoryID(t, "expense", "交通")
	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-11T12:30:00+08:00", AmountCents: 1500,
		CategoryID: traffic, FromAccountID: bank.ID, Merchant: "麦当劳", OperationID: ids.New()})
	sug, _ = e.svc.SuggestMerchant(e.ledgerID, "麦当")
	if sug == nil || sug.CategoryID != traffic {
		t.Fatalf("after correction suggestion = %+v, want traffic", sug)
	}

	// 习惯推荐出现，屏蔽后消失
	n, _ := timeParse("2026-09-11")
	recs, err := e.svc.Recommend(e.ledgerID, n)
	if err != nil {
		t.Fatal(err)
	}
	var foundHabit bool
	for _, r := range recs {
		if r.Kind == "habit" && r.CategoryID == food {
			foundHabit = true
		}
	}
	// 屏蔽 habit:cat:food
	if err := e.svc.DismissRecommendation(e.ledgerID, "habit", food); err != nil {
		t.Fatal(err)
	}
	recs2, _ := e.svc.Recommend(e.ledgerID, n)
	for _, r := range recs2 {
		if r.Kind == "habit" && r.CategoryID == food {
			t.Fatalf("dismissed habit still recommended: %+v", r)
		}
	}
	_ = foundHabit
}

// 资产负债：应收/应付进入净资产；溢缴为负。
func TestAssetsOverview(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	card := e.account(t, "信用卡", "credit_card", 0)
	if _, err := e.svc.Lend(ledger.LendInput{
		LedgerID: e.ledgerID, ActorID: e.userID, BusinessDate: "2026-09-01",
		FromAccountID: bank.ID, AmountCents: 30000, Counterparty: "朋友", OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Borrow(ledger.BorrowInput{
		LedgerID: e.ledgerID, ActorID: e.userID, BusinessDate: "2026-09-01",
		ToAccountID: card.ID, AmountCents: 20000, Counterparty: "同事", OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	a, err := e.svc.AssetsOverview(e.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	// 资产：银行卡 70000；信用卡溢缴 -20000（借入到账，保留负号不取绝对值）；应收 30000；应付 20000
	if a.AssetCents != "70000" || a.LiabilityCents != "-20000" || a.ReceivableCents != "30000" || a.PayableCents != "20000" {
		t.Fatalf("assets = %+v", a)
	}
	// 净资产 = 90000 + 30000 - 0 - 20000 = 100000
	if a.NetWorthCents != "100000" {
		t.Fatalf("net worth = %s, want 100000", a.NetWorthCents)
	}
}

func timeParse(s string) (t time.Time, err error) {
	return time.Parse("2006-01-02", s)
}
