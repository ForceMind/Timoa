package ledger_test

import (
	"database/sql"
	"strconv"
	"testing"

	"xiaozhang/internal/bootstrap"
	"xiaozhang/internal/ledger"
	"xiaozhang/internal/storage"
)

// newTestEnv opens a real temporary SQLite database, migrates and
// bootstraps an admin + default ledger. Nothing is mocked: constraints,
// transactions and aggregation are exercised for real.
type testEnv struct {
	db       *sql.DB
	svc      *ledger.Service
	ledgerID string
	userID   string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := bootstrap.InitAdmin(db, "admin", "Admin", "test-password-1", "测试账本"); err != nil {
		t.Fatalf("init admin: %v", err)
	}
	var ledgerID, userID string
	if err := db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT ledger_id FROM ledger_members WHERE user_id=?`, userID).Scan(&ledgerID); err != nil {
		t.Fatal(err)
	}
	return &testEnv{db: db, svc: ledger.NewService(db), ledgerID: ledgerID, userID: userID}
}

func (e *testEnv) account(t *testing.T, name, typ string, opening int64) *ledger.Account {
	t.Helper()
	a, err := e.svc.CreateAccount(e.ledgerID, e.userID, name, typ, nil, opening, strPtr("2026-01-01"), true)
	if err != nil {
		t.Fatalf("create account %s: %v", name, err)
	}
	return a
}

func strPtr(s string) *string { return &s }

func (e *testEnv) categoryID(t *testing.T, kind, name string) string {
	t.Helper()
	cats, err := e.svc.ListCategories(e.ledgerID, kind)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if c.Name == name {
			return c.ID
		}
	}
	t.Fatalf("seed category %s/%s missing", kind, name)
	return ""
}

func (e *testEnv) post(t *testing.T, in ledger.PostInput) *ledger.PostResult {
	t.Helper()
	in.LedgerID = e.ledgerID
	in.ActorID = e.userID
	res, err := e.svc.Post(in)
	if err != nil {
		t.Fatalf("post %s: %v", in.Type, err)
	}
	return res
}

func (e *testEnv) balance(t *testing.T, accountID string) int64 {
	t.Helper()
	a, err := e.svc.GetAccount(e.ledgerID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := strconv.ParseInt(a.Balance, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestUpdateAndDeleteUnusedAccount(t *testing.T) {
	e := newTestEnv(t)
	a := e.account(t, "旧账户", "bank_card", 100_00)
	if err := e.svc.UpdateAccount(e.ledgerID, e.userID, a.ID, "新账户", 250_00); err != nil {
		t.Fatalf("update unused account: %v", err)
	}
	updated, err := e.svc.GetAccount(e.ledgerID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "新账户" || updated.OpeningBalance != "25000" {
		t.Fatalf("updated=%+v, want name and opening 25000", updated)
	}
	if err := e.svc.DeleteAccount(e.ledgerID, e.userID, a.ID); err != nil {
		t.Fatalf("delete unused account: %v", err)
	}
	if _, err := e.svc.GetAccount(e.ledgerID, a.ID); err == nil {
		t.Fatal("deleted account should not be found")
	}
}

func TestAccountWithEntriesCannotUpdateOpeningOrDelete(t *testing.T) {
	e := newTestEnv(t)
	from := e.account(t, "银行卡", "bank_card", 1000_00)
	to := e.account(t, "现金", "cash", 0)
	e.post(t, ledger.PostInput{Type: "transfer", Amount: 100_00, FromAccountID: from.ID, ToAccountID: to.ID, BusinessDate: "2026-01-02", OperationID: "test-account-immutable"})
	if err := e.svc.UpdateAccount(e.ledgerID, e.userID, from.ID, "改名", 1); err == nil {
		t.Fatal("updating opening balance after entries should fail")
	}
	if err := e.svc.DeleteAccount(e.ledgerID, e.userID, from.ID); err == nil {
		t.Fatal("deleting account with entries should fail")
	}
}
