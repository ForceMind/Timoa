package ledger_test

import (
	"sync"
	"testing"

	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
)

// T01: 消费 500、退款 200 → 净支出 300，正常收入不增加。
func TestT01_RefundReducesNetExpense(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	cat := e.categoryID(t, "expense", "购物")

	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 50000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})

	_, err := e.svc.Refund(ledger.RefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-08",
		AccountID: bank.ID, Allocations: []ledger.RefundAlloc{{AmountCents: 20000}}, OperationID: ids.New(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ov, err := e.svc.Overview(e.ledgerID, "2026-03-01", "2026-04-01")
	if err != nil {
		t.Fatal(err)
	}
	if ov.NetExpenseCents != "30000" || ov.RefundsCents != "20000" || ov.NetIncomeCents != "0" {
		t.Fatalf("overview = %+v, want net expense 30000 refunds 20000 income 0", ov)
	}
	// 余额：500 出、200 回
	if got := e.balance(t, bank.ID); got != 100000-50000+20000 {
		t.Fatalf("balance = %d", got)
	}
	d, err := e.svc.TxDetail(e.ledgerID, orig.TxID)
	if err != nil {
		t.Fatal(err)
	}
	if d.RefundedTotal != "20000" || len(d.Refunds) != 1 {
		t.Fatalf("detail refunds = %+v", d.Refunds)
	}
}

// T02: 多次退款到达上限后不能再退；并发退款也不超额。
func TestT02_RefundCapAndConcurrency(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	cat := e.categoryID(t, "expense", "购物")
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 50000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})

	refund := func(amount int64) error {
		_, err := e.svc.Refund(ledger.RefundInput{
			LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-08",
			AccountID: bank.ID, Allocations: []ledger.RefundAlloc{{AmountCents: amount}}, OperationID: ids.New(),
		})
		return err
	}
	if err := refund(30000); err != nil {
		t.Fatal(err)
	}
	if err := refund(20000); err != nil {
		t.Fatal(err)
	}
	// 已达上限
	if err := refund(100); err == nil {
		t.Fatal("over-cap refund accepted")
	} else if le, ok := err.(*ledger.Error); !ok || le.Code != "refund_cap_exceeded" {
		t.Fatalf("want refund_cap_exceeded, got %v", err)
	}

	// 并发：新一笔 500，两个“设备”各退 300，总额不得超过 500
	orig2 := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-06", AmountCents: 50000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = e.svc.Refund(ledger.RefundInput{
				LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig2.TxID, BusinessDate: "2026-03-08",
				AccountID: bank.ID, Allocations: []ledger.RefundAlloc{{AmountCents: 30000}}, OperationID: ids.New(),
			})
		}(i)
	}
	wg.Wait()
	d, err := e.svc.TxDetail(e.ledgerID, orig2.TxID)
	if err != nil {
		t.Fatal(err)
	}
	if d.RefundedTotal != "30000" {
		t.Fatalf("concurrent refunds total = %s, want 30000 (exactly one won)", d.RefundedTotal)
	}
}

// T03: 食品 200、日用品 100，退日用品 60 → 最终 200 / 40。
func TestT03_SplitRefundPerCategory(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	food := e.createCat(t, "expense", "食品")
	daily := e.createCat(t, "expense", "日用品")

	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 30000,
		FromAccountID: bank.ID, OperationID: ids.New(),
		Splits: []ledger.SplitInput{
			{PartType: "expense", CategoryID: food, AmountCents: 20000},
			{PartType: "expense", CategoryID: daily, AmountCents: 10000},
		}})

	d, _ := e.svc.TxDetail(e.ledgerID, orig.TxID)
	var dailySplit string
	for _, sp := range d.Splits {
		if sp.CategoryID == daily {
			dailySplit = sp.ID
		}
	}
	if dailySplit == "" {
		t.Fatal("daily split not found")
	}
	if _, err := e.svc.Refund(ledger.RefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-08",
		AccountID: bank.ID, OperationID: ids.New(),
		Allocations: []ledger.RefundAlloc{{SplitID: dailySplit, AmountCents: 6000}},
	}); err != nil {
		t.Fatal(err)
	}

	nets, err := e.svc.CategoryNets(e.ledgerID, "2026-03-01", "2026-04-01", "accrual")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, n := range nets {
		got[n.CategoryID] = n.NetCents
	}
	if got[food] != "20000" || got[daily] != "4000" {
		t.Fatalf("category nets = %v, want food 20000 daily 4000", got)
	}
}

// T06: 聚餐付 500、自担 100、代付 400；回款后费用仍为 100。
func TestT06_SharedBillWithReceivable(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	food := e.categoryID(t, "expense", "餐饮")

	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 50000,
		FromAccountID: bank.ID, OperationID: ids.New(),
		Splits: []ledger.SplitInput{
			{PartType: "expense", CategoryID: food, AmountCents: 10000},
			{PartType: "receivable", Counterparty: "朋友小李", AmountCents: 40000},
		}})

	if _, err := e.svc.Settle(ledger.SettlementInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-10",
		AccountID: bank.ID, AmountCents: 40000, Counterparty: "朋友小李", OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}

	ov, _ := e.svc.Overview(e.ledgerID, "2026-03-01", "2026-04-01")
	if ov.NetExpenseCents != "10000" || ov.NetIncomeCents != "0" {
		t.Fatalf("overview = %+v, want net expense 10000 income 0 (回款不是收入)", ov)
	}
	d, _ := e.svc.TxDetail(e.ledgerID, orig.TxID)
	if d.Receivable == nil || d.Receivable.Outstanding != "0" || !d.Receivable.FullySettled {
		t.Fatalf("receivable = %+v", d.Receivable)
	}
	if got := e.balance(t, bank.ID); got != 90000 {
		t.Fatalf("balance = %d, want 90000 (付 500 回 400，自担 100)", got)
	}
}

// T07: 多次部分报销结清应收，未收部分保留，超额结算被拒绝。
func TestT07_PartialSettlements(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	food := e.categoryID(t, "expense", "餐饮")
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 50000,
		FromAccountID: bank.ID, OperationID: ids.New(),
		Splits: []ledger.SplitInput{
			{PartType: "expense", CategoryID: food, AmountCents: 10000},
			{PartType: "receivable", Counterparty: "同事", AmountCents: 40000},
		}})

	settle := func(amount int64) error {
		_, err := e.svc.Settle(ledger.SettlementInput{
			LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-10",
			AccountID: bank.ID, AmountCents: amount, OperationID: ids.New(),
		})
		return err
	}
	if err := settle(15000); err != nil {
		t.Fatal(err)
	}
	if err := settle(25000); err != nil {
		t.Fatal(err)
	}
	if err := settle(100); err == nil {
		t.Fatal("over-settlement accepted")
	} else if le, ok := err.(*ledger.Error); !ok || le.Code != "settlement_cap_exceeded" {
		t.Fatalf("want settlement_cap_exceeded, got %v", err)
	}
	d, _ := e.svc.TxDetail(e.ledgerID, orig.TxID)
	if d.Receivable.Outstanding != "0" {
		t.Fatalf("outstanding = %s", d.Receivable.Outstanding)
	}
}

// T08: 普通支出转待报销再收回：重分类不制造现金流或重复收入。
func TestT08_ReclassThenSettle(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	cat := e.categoryID(t, "expense", "交通")
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 20000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})

	if _, err := e.svc.Reclass(ledger.ReclassInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-06",
		AmountCents: 15000, Counterparty: "公司", OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	// 重分类后：费用 50，应收 150，银行余额不变（无现金流）
	ov, _ := e.svc.Overview(e.ledgerID, "2026-03-01", "2026-04-01")
	if ov.NetExpenseCents != "5000" {
		t.Fatalf("net expense after reclass = %s, want 5000", ov.NetExpenseCents)
	}
	if got := e.balance(t, bank.ID); got != 80000 {
		t.Fatalf("balance after reclass = %d, want 80000 (重分类不动现金)", got)
	}

	if _, err := e.svc.Settle(ledger.SettlementInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-15",
		AccountID: bank.ID, AmountCents: 15000, Counterparty: "公司", OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	ov, _ = e.svc.Overview(e.ledgerID, "2026-03-01", "2026-04-01")
	if ov.NetIncomeCents != "0" || ov.NetExpenseCents != "5000" {
		t.Fatalf("overview = %+v, want income 0 expense 5000 (报销到账不是收入)", ov)
	}
	if got := e.balance(t, bank.ID); got != 95000 {
		t.Fatalf("balance = %d, want 95000", got)
	}
}

// T09: 混合自担与代付款的退款、回款不重复结算同一份额。
func TestT09_MixedRefundAndSettlement(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	food := e.categoryID(t, "expense", "餐饮")
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 50000,
		FromAccountID: bank.ID, OperationID: ids.New(),
		Splits: []ledger.SplitInput{
			{PartType: "expense", CategoryID: food, AmountCents: 10000},
			{PartType: "receivable", Counterparty: "朋友", AmountCents: 40000},
		}})

	d, _ := e.svc.TxDetail(e.ledgerID, orig.TxID)
	var expSplit, recvSplit string
	for _, sp := range d.Splits {
		if sp.PartType == "expense" {
			expSplit = sp.ID
		} else {
			recvSplit = sp.ID
		}
	}
	// 商户退款 200：100 退自担费用、100 退代付部分
	if _, err := e.svc.Refund(ledger.RefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-08",
		AccountID: bank.ID, OperationID: ids.New(),
		Allocations: []ledger.RefundAlloc{
			{SplitID: expSplit, AmountCents: 10000},
			{SplitID: recvSplit, AmountCents: 10000},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// 代付部分已退 100，应收剩 300；超额回款拒绝
	if _, err := e.svc.Settle(ledger.SettlementInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-10",
		AccountID: bank.ID, AmountCents: 40000, OperationID: ids.New(),
	}); err == nil {
		t.Fatal("settlement exceeding post-refund outstanding accepted")
	}
	if _, err := e.svc.Settle(ledger.SettlementInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-10",
		AccountID: bank.ID, AmountCents: 30000, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	ov, _ := e.svc.Overview(e.ledgerID, "2026-03-01", "2026-04-01")
	// 费用：100 - 100 退 = 0；收入 0
	if ov.NetExpenseCents != "0" || ov.NetIncomeCents != "0" {
		t.Fatalf("overview = %+v", ov)
	}
}

// T10: 收入 1,000、退回 200 → 净收入 800。
func TestT10_IncomeRefund(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	salary := e.categoryID(t, "income", "工资薪酬")
	orig := e.post(t, ledger.PostInput{Type: "income", BusinessDate: "2026-03-10", AmountCents: 100000,
		CategoryID: salary, ToAccountID: bank.ID, OperationID: ids.New()})

	if _, err := e.svc.IncomeRefund(ledger.IncomeRefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-12",
		AccountID: bank.ID, AmountCents: 20000, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	ov, _ := e.svc.Overview(e.ledgerID, "2026-03-01", "2026-04-01")
	if ov.GrossIncomeCents != "100000" || ov.IncomeReturnsCents != "20000" || ov.NetIncomeCents != "80000" {
		t.Fatalf("overview = %+v, want 100000/20000/80000", ov)
	}
	// 超额退回拒绝
	if _, err := e.svc.IncomeRefund(ledger.IncomeRefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-13",
		AccountID: bank.ID, AmountCents: 80001, OperationID: ids.New(),
	}); err == nil {
		t.Fatal("over-cap income refund accepted")
	}
}

// T11: 350 元错账更正为 35：费用 35，审计完整，无虚假退款现金流。
func TestT11_Correction(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	cat := e.categoryID(t, "expense", "餐饮")
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 35000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})

	_, err := e.svc.Revise(e.ledgerID, e.userID, orig.TxID, "金额误记，350 改为 35", &ledger.PostInput{
		Type: "expense", BusinessDate: "2026-03-05", AmountCents: 3500,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ov, _ := e.svc.Overview(e.ledgerID, "2026-03-01", "2026-04-01")
	if ov.NetExpenseCents != "3500" {
		t.Fatalf("net expense = %s, want 3500", ov.NetExpenseCents)
	}
	if ov.RefundsCents != "0" {
		t.Fatalf("refunds = %s, want 0 (技术冲正不是退款)", ov.RefundsCents)
	}
	if got := e.balance(t, bank.ID); got != 100000-3500 {
		t.Fatalf("balance = %d, want 96500", got)
	}
	d, _ := e.svc.TxDetail(e.ledgerID, orig.TxID)
	if !d.Reversed || d.Revision == nil || d.Revision.Reason == "" {
		t.Fatalf("revision chain missing: %+v", d)
	}
	// 审计包含更正记录
	var audits int
	if err := e.db.QueryRow(`SELECT COUNT(1) FROM audit_log WHERE action='transaction.correct'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("correct audit = %d err %v", audits, err)
	}
}

// T12: 作废不能重复冲正；有下游关联时阻止作废。
func TestT12_VoidRules(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	cat := e.categoryID(t, "expense", "餐饮")

	// 有退款的原单不能作废
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 50000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})
	if _, err := e.svc.Refund(ledger.RefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-03-06",
		AccountID: bank.ID, Allocations: []ledger.RefundAlloc{{AmountCents: 10000}}, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Revise(e.ledgerID, e.userID, orig.TxID, "想作废", nil); err == nil {
		t.Fatal("void with effective refunds accepted")
	} else if le, ok := err.(*ledger.Error); !ok || le.Code != "dependency_blocked" {
		t.Fatalf("want dependency_blocked, got %v", err)
	}

	// 干净原单作废后不能重复冲正
	orig2 := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 8000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})
	if _, err := e.svc.Revise(e.ledgerID, e.userID, orig2.TxID, "作废", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Revise(e.ledgerID, e.userID, orig2.TxID, "再作废", nil); err == nil {
		t.Fatal("double reversal accepted")
	}
	ov, _ := e.svc.Overview(e.ledgerID, "2026-03-01", "2026-04-01")
	// 费用：orig 500 - 退 100 = 400；orig2 已作废
	if ov.NetExpenseCents != "40000" {
		t.Fatalf("net expense = %s, want 40000", ov.NetExpenseCents)
	}
}

// T13: 3 月消费、4 月退款：发生期、原消费归属、现金流分别正确。
func TestT13_CrossMonthBases(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	cat := e.categoryID(t, "expense", "购物")
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-20", AmountCents: 50000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})
	if _, err := e.svc.Refund(ledger.RefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-04-02",
		AccountID: bank.ID, Allocations: []ledger.RefundAlloc{{AmountCents: 20000}}, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}

	// 发生期：3 月费用 500；4 月退款冲减 200（允许负净支出）
	mar, _ := e.svc.CategoryNets(e.ledgerID, "2026-03-01", "2026-04-01", "accrual")
	apr, _ := e.svc.CategoryNets(e.ledgerID, "2026-04-01", "2026-05-01", "accrual")
	if len(mar) != 1 || mar[0].NetCents != "50000" {
		t.Fatalf("march accrual = %+v, want 50000", mar)
	}
	if len(apr) != 1 || apr[0].NetCents != "-20000" {
		t.Fatalf("april accrual = %+v, want -20000 (跨期退款允许为负)", apr)
	}

	// 原消费归属：3 月该消费净额 300
	marOrigin, _ := e.svc.CategoryNets(e.ledgerID, "2026-03-01", "2026-04-01", "origin")
	if len(marOrigin) != 1 || marOrigin[0].NetCents != "30000" {
		t.Fatalf("march origin = %+v, want 30000", marOrigin)
	}

	// 现金流：仍按真实收付日期（3 月出 500、4 月回 200）
	if got := e.balance(t, bank.ID); got != 70000 {
		t.Fatalf("balance = %d, want 70000", got)
	}
}

// 核销：确认无法收回应收 → 转自担费用（有原因，不自动）。
func TestWriteoff(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 100000)
	food := e.categoryID(t, "expense", "餐饮")
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-03-05", AmountCents: 50000,
		FromAccountID: bank.ID, OperationID: ids.New(),
		Splits: []ledger.SplitInput{
			{PartType: "expense", CategoryID: food, AmountCents: 10000},
			{PartType: "receivable", Counterparty: "朋友", AmountCents: 40000},
		}})

	if _, err := e.svc.Writeoff(ledger.WriteoffInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-06-01",
		AmountCents: 40000, Reason: "多次催要无果，确认无法收回", OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	ov, _ := e.svc.Overview(e.ledgerID, "2026-06-01", "2026-07-01")
	if ov.NetExpenseCents != "40000" {
		t.Fatalf("june net expense = %s, want 40000 (核销计入当月费用)", ov.NetExpenseCents)
	}
	d, _ := e.svc.TxDetail(e.ledgerID, orig.TxID)
	if d.Receivable.Outstanding != "0" {
		t.Fatalf("outstanding = %s", d.Receivable.Outstanding)
	}
}

// 辅助：创建自定义分类
func (e *testEnv) createCat(t *testing.T, kind, name string) string {
	t.Helper()
	c, err := e.svc.CreateCategory(e.ledgerID, e.userID, nil, kind, name)
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}
