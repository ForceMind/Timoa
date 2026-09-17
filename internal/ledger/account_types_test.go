package ledger_test

import "testing"

// TestInvestmentAccountTypes：股票、基金是一级资产账户，能与多个银行卡/信用卡并存。
func TestInvestmentAccountTypes(t *testing.T) {
	e := newTestEnv(t)
	for _, tc := range []struct {
		name string
		typ  string
	}{
		{"招商银行工资卡", "bank_card"},
		{"招商银行储蓄卡", "bank_card"},
		{"招行信用卡", "credit_card"},
		{"东方财富股票", "stock"},
		{"支付宝指数基金", "fund"},
	} {
		a := e.account(t, tc.name, tc.typ, 12345)
		if a.Type != tc.typ {
			t.Fatalf("%s type=%q, want %q", tc.name, a.Type, tc.typ)
		}
		var kind string
		if err := e.db.QueryRow(`SELECT kind FROM subjects WHERE id=?`, a.SubjectID).Scan(&kind); err != nil {
			t.Fatalf("%s backing subject: %v", tc.name, err)
		}
		wantKind := "asset"
		if tc.typ == "credit_card" {
			wantKind = "liability"
		}
		if kind != wantKind {
			t.Fatalf("%s subject kind=%q, want %q", tc.name, kind, wantKind)
		}
	}
}
