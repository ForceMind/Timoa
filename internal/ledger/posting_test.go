package ledger_test

import (
	"testing"

	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
)

// T-stage1-01: 银行卡消费 500 → 费用 +500，银行资产 -500，收入不变。
func TestExpensePosting(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "招商银行卡", "bank_card", 100000)
	cat := e.categoryID(t, "expense", "餐饮")

	e.post(t, ledger.PostInput{
		Type: "expense", BusinessDate: "2026-09-01", AmountCents: 50000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New(),
	})

	if got := e.balance(t, bank.ID); got != 50000 {
		t.Fatalf("bank balance = %d, want 50000", got)
	}
	sum, err := e.svc.PeriodSummary(e.ledgerID, "2026-09-01", "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if sum.ExpenseCents != "50000" || sum.IncomeCents != "0" {
		t.Fatalf("summary = %+v, want expense 50000 income 0", sum)
	}
}

// T-stage1-02: 工资收入到账 → 收入 +10000，资产 +10000，费用不变。
func TestIncomePosting(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "工资卡", "bank_card", 0)
	cat := e.categoryID(t, "income", "工资薪酬")

	e.post(t, ledger.PostInput{
		Type: "income", BusinessDate: "2026-09-10", AmountCents: 1000000,
		CategoryID: cat, ToAccountID: bank.ID, OperationID: ids.New(),
	})

	if got := e.balance(t, bank.ID); got != 1000000 {
		t.Fatalf("bank balance = %d, want 1000000", got)
	}
	sum, _ := e.svc.PeriodSummary(e.ledgerID, "2026-09-01", "2026-10-01")
	if sum.IncomeCents != "1000000" || sum.ExpenseCents != "0" {
		t.Fatalf("summary = %+v", sum)
	}
}

// T05（阶段一部分）: 自有账户互转 1000 → 不记收支，净资产不变，一边资产减少、一边增加。
func TestTransferIsNotIncomeOrExpense(t *testing.T) {
	e := newTestEnv(t)
	a := e.account(t, "银行卡", "bank_card", 500000)
	b := e.account(t, "微信零钱", "wechat_change", 10000)

	e.post(t, ledger.PostInput{
		Type: "transfer", BusinessDate: "2026-09-05", AmountCents: 100000,
		FromAccountID: a.ID, ToAccountID: b.ID, OperationID: ids.New(),
	})

	if got := e.balance(t, a.ID); got != 400000 {
		t.Fatalf("from balance = %d, want 400000", got)
	}
	if got := e.balance(t, b.ID); got != 110000 {
		t.Fatalf("to balance = %d, want 110000", got)
	}
	sum, _ := e.svc.PeriodSummary(e.ledgerID, "2026-09-01", "2026-10-01")
	if sum.IncomeCents != "0" || sum.ExpenseCents != "0" {
		t.Fatalf("transfer leaked into summary: %+v", sum)
	}
}

// T04（阶段一部分）: 信用卡消费增加负债；还款（转账）减少负债；费用只记一次；溢缴为负负债不取绝对值。
func TestCreditCardDebtAndRepayment(t *testing.T) {
	e := newTestEnv(t)
	card := e.account(t, "信用卡", "credit_card", 0)
	bank := e.account(t, "储蓄卡", "bank_card", 200000)
	cat := e.categoryID(t, "expense", "购物")

	e.post(t, ledger.PostInput{
		Type: "expense", BusinessDate: "2026-09-02", AmountCents: 50000,
		CategoryID: cat, FromAccountID: card.ID, OperationID: ids.New(),
	})
	if got := e.balance(t, card.ID); got != 50000 {
		t.Fatalf("card debt = %d, want 50000", got)
	}

	// 还款 500：资产减少，负债减少，不产生新费用。
	e.post(t, ledger.PostInput{
		Type: "transfer", BusinessDate: "2026-09-20", AmountCents: 50000,
		FromAccountID: bank.ID, ToAccountID: card.ID, OperationID: ids.New(),
	})
	if got := e.balance(t, card.ID); got != 0 {
		t.Fatalf("card debt after repay = %d, want 0", got)
	}
	if got := e.balance(t, bank.ID); got != 150000 {
		t.Fatalf("bank balance = %d, want 150000", got)
	}
	sum, _ := e.svc.PeriodSummary(e.ledgerID, "2026-09-01", "2026-10-01")
	if sum.ExpenseCents != "50000" {
		t.Fatalf("expense = %s, want 50000 (repayment is not an expense)", sum.ExpenseCents)
	}

	// 溢缴：多还 100 → 负债为 -100（保留符号）。
	e.post(t, ledger.PostInput{
		Type: "transfer", BusinessDate: "2026-09-21", AmountCents: 10000,
		FromAccountID: bank.ID, ToAccountID: card.ID, OperationID: ids.New(),
	})
	if got := e.balance(t, card.ID); got != -10000 {
		t.Fatalf("card overpayment = %d, want -10000", got)
	}
}

// T18/T19（阶段一部分）: 相同 operation_id + 相同内容 → 幂等重放只产生一笔；
// 相同 ID + 不同内容 → idempotency_conflict。
func TestIdempotentPosting(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 0)
	cat := e.categoryID(t, "expense", "餐饮")
	opID := ids.New()
	in := ledger.PostInput{
		Type: "expense", BusinessDate: "2026-09-03", AmountCents: 2800,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: opID,
	}

	first := e.post(t, in)
	// 模拟「保存成功但响应丢失」的重试：仍只产生一笔交易。
	second := e.post(t, in)
	if first.TxID != second.TxID || !second.Replayed {
		t.Fatalf("replay = %+v, want same tx id + replayed", second)
	}
	if got := e.balance(t, bank.ID); got != -2800 {
		t.Fatalf("balance = %d, want -2800 (posted exactly once)", got)
	}
	txs, err := e.svc.ListTransactions(e.ledgerID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 1 {
		t.Fatalf("transactions = %d, want 1", len(txs))
	}

	// 相同 operation_id 但内容不同 → 冲突。
	conflict := in
	conflict.AmountCents = 2900
	in.LedgerID, in.ActorID = e.ledgerID, e.userID
	conflict.LedgerID, conflict.ActorID = e.ledgerID, e.userID
	if _, err := e.svc.Post(conflict); err == nil {
		t.Fatal("want idempotency_conflict, got nil")
	} else if le, ok := err.(*ledger.Error); !ok || le.Code != "idempotency_conflict" {
		t.Fatalf("want idempotency_conflict, got %v", err)
	}
}

// 不变量：所有交易分录借贷平衡；账户余额与完整分录求和一致。
func TestInvariantBalancedEntriesAndBalances(t *testing.T) {
	e := newTestEnv(t)
	a := e.account(t, "银行卡", "bank_card", 123456)
	b := e.account(t, "现金", "cash", 0)
	expCat := e.categoryID(t, "expense", "餐饮")
	incCat := e.categoryID(t, "income", "工资薪酬")

	e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-01", AmountCents: 50000, CategoryID: expCat, FromAccountID: a.ID, OperationID: ids.New()})
	e.post(t, ledger.PostInput{Type: "income", BusinessDate: "2026-09-02", AmountCents: 90000, CategoryID: incCat, ToAccountID: b.ID, OperationID: ids.New()})
	e.post(t, ledger.PostInput{Type: "transfer", BusinessDate: "2026-09-03", AmountCents: 70000, FromAccountID: a.ID, ToAccountID: b.ID, OperationID: ids.New()})

	// 每笔交易 debits == credits。
	rows, err := e.db.Query(`SELECT tx_id,
		SUM(CASE WHEN direction='debit' THEN amount_cents ELSE 0 END),
		SUM(CASE WHEN direction='credit' THEN amount_cents ELSE 0 END)
		FROM entries GROUP BY tx_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var txID string
		var d, c int64
		if err := rows.Scan(&txID, &d, &c); err != nil {
			t.Fatal(err)
		}
		if d != c {
			t.Fatalf("tx %s unbalanced: debit %d credit %d", txID, d, c)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("checked %d txs, want 3", n)
	}

	// 余额 = 期初 + 完整分录求和（Service 已实现，这里独立复核）。
	if got := e.balance(t, a.ID); got != 123456-50000-70000 {
		t.Fatalf("account A = %d", got)
	}
	if got := e.balance(t, b.ID); got != 90000+70000 {
		t.Fatalf("account B = %d", got)
	}
}

// 非法输入：零金额、超额、同账户转账、跨账本账户、归档账户。
func TestValidation(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 0)
	cat := e.categoryID(t, "expense", "餐饮")

	mk := func(mut func(*ledger.PostInput)) ledger.PostInput {
		in := ledger.PostInput{
			LedgerID: e.ledgerID, ActorID: e.userID,
			Type: "expense", BusinessDate: "2026-09-01", AmountCents: 100,
			CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New(),
		}
		mut(&in)
		return in
	}
	cases := map[string]ledger.PostInput{
		"zero amount":   mk(func(in *ledger.PostInput) { in.AmountCents = 0 }),
		"negative":      mk(func(in *ledger.PostInput) { in.AmountCents = -5 }),
		"over max":      mk(func(in *ledger.PostInput) { in.AmountCents = 999_999_999_999 + 1 }),
		"bad date":      mk(func(in *ledger.PostInput) { in.BusinessDate = "2026-13-40" }),
		"missing op id": mk(func(in *ledger.PostInput) { in.OperationID = "" }),
		"bad category":  mk(func(in *ledger.PostInput) { in.CategoryID = ids.New() }),
		"bad account":   mk(func(in *ledger.PostInput) { in.FromAccountID = ids.New() }),
	}
	for name, in := range cases {
		if _, err := e.svc.Post(in); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}

	// 同账户转账
	if _, err := e.svc.Post(ledger.PostInput{
		LedgerID: e.ledgerID, ActorID: e.userID, Type: "transfer",
		BusinessDate: "2026-09-01", AmountCents: 100,
		FromAccountID: bank.ID, ToAccountID: bank.ID, OperationID: ids.New(),
	}); err == nil {
		t.Error("same-account transfer: want error")
	}

	// 跨账本：另一个账本的账户不能被引用。
	e2 := newTestEnv(t) // 独立数据库 → 该账户在本账本根本不存在，验证拒绝
	other := e2.account(t, "别人卡", "bank_card", 0)
	if _, err := e.svc.Post(ledger.PostInput{
		LedgerID: e.ledgerID, ActorID: e.userID, Type: "expense",
		BusinessDate: "2026-09-01", AmountCents: 100,
		CategoryID: cat, FromAccountID: other.ID, OperationID: ids.New(),
	}); err == nil {
		t.Error("cross-ledger account: want error")
	}

	// 归档账户拒绝入账。
	if err := e.svc.ArchiveAccount(e.ledgerID, e.userID, bank.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Post(mk(func(in *ledger.PostInput) {})); err == nil {
		t.Error("archived account: want error")
	}
}

// 未确认余额不被当作真实零：期初未确认标志随账户返回。
func TestUnconfirmedOpeningBalance(t *testing.T) {
	e := newTestEnv(t)
	a, err := e.svc.CreateAccount(e.ledgerID, e.userID, "旧卡", "bank_card", nil, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !a.BalanceUnconfirmed {
		t.Fatal("want balance_unconfirmed=true when opening not confirmed")
	}
}
