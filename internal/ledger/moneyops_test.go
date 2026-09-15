package ledger_test

import (
	"testing"

	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
)

// T14: 借入、借出、还本、收本金、利息、押金各有正确账务归属。
func TestT14_BorrowLendRepayDeposit(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)

	// 借出 400：资产减少，应收增加，不是费用
	lend, err := e.svc.Lend(ledger.LendInput{
		LedgerID: e.ledgerID, ActorID: e.userID, BusinessDate: "2026-04-01",
		FromAccountID: bank.ID, AmountCents: 40000, Counterparty: "朋友", OperationID: ids.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.balance(t, bank.ID); got != 60000 {
		t.Fatalf("after lend balance = %d, want 60000", got)
	}
	ov, _ := e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.NetExpenseCents != "0" || ov.NetIncomeCents != "0" {
		t.Fatalf("lend leaked into overview: %+v", ov)
	}

	// 收本金 400（Settle on lend）：应收结清，不是收入
	if _, err := e.svc.Settle(ledger.SettlementInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: lend.TxID, BusinessDate: "2026-04-10",
		AccountID: bank.ID, AmountCents: 40000, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	ov, _ = e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.NetIncomeCents != "0" {
		t.Fatalf("principal collection counted as income: %+v", ov)
	}
	if got := e.balance(t, bank.ID); got != 100000 {
		t.Fatalf("after settle balance = %d", got)
	}

	// 借入 400：资产增加，应付增加，不是收入
	borrow, err := e.svc.Borrow(ledger.BorrowInput{
		LedgerID: e.ledgerID, ActorID: e.userID, BusinessDate: "2026-04-02",
		ToAccountID: bank.ID, AmountCents: 40000, Counterparty: "同事", OperationID: ids.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ov, _ = e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.NetIncomeCents != "0" {
		t.Fatalf("borrow counted as income: %+v", ov)
	}

	// 还本金 300：结清对应负债；超额还本拒绝
	if _, err := e.svc.Repay(ledger.RepayInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: borrow.TxID, BusinessDate: "2026-04-15",
		FromAccountID: bank.ID, AmountCents: 30000, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Repay(ledger.RepayInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: borrow.TxID, BusinessDate: "2026-04-16",
		FromAccountID: bank.ID, AmountCents: 10001, OperationID: ids.New(),
	}); err == nil {
		t.Fatal("over-repayment accepted")
	}
	ov, _ = e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.NetExpenseCents != "0" {
		t.Fatalf("principal repayment counted as expense: %+v", ov)
	}

	// 利息单独记费用
	interestCat := e.createCat(t, "expense", "借款利息")
	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-04-15", AmountCents: 500,
		CategoryID: interestCat, FromAccountID: bank.ID, OperationID: ids.New()})
	ov, _ = e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.NetExpenseCents != "500" {
		t.Fatalf("interest expense = %s, want 500", ov.NetExpenseCents)
	}

	// 押金：支付形成往来（lend 语义），退回结清（settle），全程不是消费/工资
	deposit, err := e.svc.Lend(ledger.LendInput{
		LedgerID: e.ledgerID, ActorID: e.userID, BusinessDate: "2026-04-03",
		FromAccountID: bank.ID, AmountCents: 100000, Counterparty: "房东（押金）", OperationID: ids.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Settle(ledger.SettlementInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: deposit.TxID, BusinessDate: "2026-04-20",
		AccountID: bank.ID, AmountCents: 100000, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	ov, _ = e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.NetExpenseCents != "500" || ov.NetIncomeCents != "0" {
		t.Fatalf("deposit leaked into overview: %+v", ov)
	}
}

// T15: 房贷本金和利息、理财赎回本金和收益不会混算费用或收入。
func TestT15_LoanAndRedeem(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 500000)
	mortgage := e.account(t, "房贷", "loan_liability", 1000000)
	invest := e.account(t, "理财", "other_asset", 200000)
	interestCat := e.createCat(t, "expense", "房贷利息")
	yieldCat := e.categoryID(t, "income", "工资薪酬")

	// 房贷还款 5000：本金 3000 减负债，利息 2000 计费用
	if _, err := e.svc.LoanRepay(ledger.LoanRepayInput{
		LedgerID: e.ledgerID, ActorID: e.userID, BusinessDate: "2026-04-05",
		FromAccountID: bank.ID, LoanAccountID: mortgage.ID, TotalCents: 500000,
		PrincipalCents: 300000, InterestCategory: interestCat, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.balance(t, mortgage.ID); got != 700000 {
		t.Fatalf("mortgage debt = %d, want 700000 (10000.00 - 3000.00 本金)", got)
	}
	ov, _ := e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.NetExpenseCents != "200000" || ov.NetIncomeCents != "0" {
		t.Fatalf("loan repay overview = %+v, want expense 200000 (仅利息)", ov)
	}

	// 理财赎回 1100：本金 1000 转回，收益 100 计收入
	if _, err := e.svc.Redeem(ledger.RedeemInput{
		LedgerID: e.ledgerID, ActorID: e.userID, BusinessDate: "2026-04-06",
		ToAccountID: bank.ID, InvestAccountID: invest.ID, TotalCents: 110000,
		PrincipalCents: 100000, YieldCategory: yieldCat, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	ov, _ = e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.NetIncomeCents != "10000" || ov.NetExpenseCents != "200000" {
		t.Fatalf("redeem overview = %+v, want income 10000 (仅收益)", ov)
	}
	if got := e.balance(t, invest.ID); got != 100000 {
		t.Fatalf("invest balance = %d, want 100000", got)
	}
}

// 支付优惠：应付 10 实付 9 → 净费用 9（费用 10 - 优惠 1）。
func TestDiscountSplit(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	food := e.categoryID(t, "expense", "餐饮")

	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-04-07", AmountCents: 900,
		FromAccountID: bank.ID, OperationID: ids.New(), Merchant: "碰一碰",
		Splits: []ledger.SplitInput{
			{PartType: "expense", CategoryID: food, AmountCents: 1000},
			{PartType: "discount", CategoryID: food, AmountCents: 100},
		}})

	ov, _ := e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.GrossExpenseCents != "900" || ov.NetExpenseCents != "900" {
		t.Fatalf("overview = %+v, want net 900 (应付 10 优惠 1 实付 9)", ov)
	}
	if got := e.balance(t, bank.ID); got != 99100 {
		t.Fatalf("balance = %d, want 99100", got)
	}
	nets, _ := e.svc.CategoryNets(e.ledgerID, "2026-04-01", "2026-05-01", "accrual")
	if len(nets) != 1 || nets[0].NetCents != "900" {
		t.Fatalf("category net = %+v, want 900", nets)
	}
}

// 重复记一笔：复制生成新业务 ID 与 operation_id。
func TestCopyTransaction(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	cat := e.categoryID(t, "expense", "餐饮")
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-04-08", AmountCents: 2800,
		CategoryID: cat, FromAccountID: bank.ID, Note: "午餐", OperationID: ids.New()})

	cp, err := e.svc.CopyTransaction(e.ledgerID, e.userID, orig.TxID, ids.New())
	if err != nil {
		t.Fatal(err)
	}
	if cp.TxID == orig.TxID {
		t.Fatal("copy returned the same transaction id")
	}
	txs, _ := e.svc.ListTransactions(e.ledgerID, 10)
	if len(txs) != 2 {
		t.Fatalf("transactions = %d, want 2", len(txs))
	}
	ov, _ := e.svc.Overview(e.ledgerID, "2026-04-01", "2026-05-01")
	if ov.NetExpenseCents != "5600" {
		t.Fatalf("net expense = %s, want 5600", ov.NetExpenseCents)
	}
}

// T36（部分）: 预制模板实际存在；种子重复执行不重复创建。
func TestSeedLibraryIdempotent(t *testing.T) {
	e := newTestEnv(t)
	// EnsureSeeds 已在 bootstrap 外显式调用一次；再次执行必须幂等
	if err := ledger.EnsureSeeds(e.db); err != nil {
		t.Fatal(err)
	}
	if err := ledger.EnsureSeeds(e.db); err != nil {
		t.Fatal(err)
	}

	tpls, err := ledger.ListTemplates(e.db, e.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tpls) < 80 {
		t.Fatalf("templates = %d, want full library (>=80)", len(tpls))
	}
	// 支出模板覆盖 spec 7.1 关键项
	want := []string{"午餐", "外卖", "房租", "公交地铁", "挂号", "学费", "给父母生活费", "婚礼随礼", "电影", "手机话费", "宠物食品", "保障型保险保费"}
	have := map[string]bool{}
	for _, tp := range tpls {
		have[tp.Name] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing expense template %q", w)
		}
	}
	// 收入与资金操作
	for _, w := range []string{"月工资", "已确认理财收益", "账户互转", "信用卡还款", "房贷还款", "借入", "借出", "支付押金", "朋友 AA 回款"} {
		if !have[w] {
			t.Errorf("missing template %q", w)
		}
	}
	// 分类为两级
	var deep int
	if err := e.db.QueryRow(`SELECT COUNT(1) FROM categories c JOIN categories p ON p.id=c.parent_id WHERE p.parent_id IS NOT NULL`).Scan(&deep); err != nil {
		t.Fatal(err)
	}
	if deep != 0 {
		t.Fatalf("categories deeper than two levels: %d", deep)
	}
	// 预置标签
	var tags int
	if err := e.db.QueryRow(`SELECT COUNT(1) FROM tags WHERE ledger_id=?`, e.ledgerID).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if tags != 6 {
		t.Fatalf("seed tags = %d, want 6", tags)
	}
}
