package ledger_test

import (
	"testing"

	"xiaozhang/internal/ledger"
)

// TestSubAccounts：二类子账户（活期/定期/理财）——
// 父账户必须是资产类、非子账户、未归档；余额独立计算、父账户汇总。
func TestSubAccounts(t *testing.T) {
	e := newTestEnv(t)

	// 父账户：银行卡（资产）
	parent, err := e.svc.CreateAccount(e.ledgerID, e.userID, "招行卡", "bank_card", nil, 100000_00, nil, true)
	if err != nil {
		t.Fatal(err)
	}

	// 三个子账户
	cur, err := e.svc.CreateSubAccount(e.ledgerID, e.userID, parent.ID, "活期", "current", 30000_00, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	dep, err := e.svc.CreateSubAccount(e.ledgerID, e.userID, parent.ID, "一年定期", "deposit", 50000_00, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := e.svc.CreateSubAccount(e.ledgerID, e.userID, parent.ID, "理财", "investment", 20000_00, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Type != "bank_card" || dep.Type != "bank_card" || inv.Type != "bank_card" {
		t.Fatalf("sub types: %q %q %q", cur.Type, dep.Type, inv.Type)
	}
	if cur.SubKind != "current" || dep.SubKind != "deposit" || inv.SubKind != "investment" {
		t.Fatalf("sub kinds: %q %q %q", cur.SubKind, dep.SubKind, inv.SubKind)
	}
	if cur.ParentID != parent.ID {
		t.Fatalf("parent not linked")
	}

	// 余额独立
	if cur.Balance != "3000000" || dep.Balance != "5000000" || inv.Balance != "2000000" {
		t.Fatalf("sub balances: %s %s %s", cur.Balance, dep.Balance, inv.Balance)
	}

	// 父账户汇总 = 自身 + 各子账户
	accts, err := e.svc.ListAccounts(e.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	var pAcc *ledger.Account
	for i := range accts {
		if accts[i].ID == parent.ID {
			pAcc = &accts[i]
		}
	}
	if pAcc == nil {
		t.Fatal("parent not in list")
	}
	// 100000_00 + 30000_00 + 50000_00 + 20000_00 = 200000_00
	if pAcc.Balance != "20000000" {
		t.Fatalf("parent rollup: got %s, want 20000000", pAcc.Balance)
	}

	// 校验：非法 sub_kind
	if _, err := e.svc.CreateSubAccount(e.ledgerID, e.userID, parent.ID, "x", "bad", 0, nil, false); err == nil {
		t.Fatal("bad sub_kind accepted")
	}
	// 校验：嵌套子账户
	if _, err := e.svc.CreateSubAccount(e.ledgerID, e.userID, cur.ID, "x", "current", 0, nil, false); err == nil {
		t.Fatal("nested sub-account accepted")
	}
	// 校验：负债账户不能挂子账户
	card, err := e.svc.CreateAccount(e.ledgerID, e.userID, "信用卡", "credit_card", nil, 0, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateSubAccount(e.ledgerID, e.userID, card.ID, "x", "current", 0, nil, false); err == nil {
		t.Fatal("sub-account under liability accepted")
	}
	// 校验：跨账本父账户
	e2 := newTestEnv(t)
	if _, err := e.svc.CreateSubAccount(e2.ledgerID, e2.userID, parent.ID, "x", "current", 0, nil, false); err == nil {
		t.Fatal("cross-ledger parent accepted")
	}
}
