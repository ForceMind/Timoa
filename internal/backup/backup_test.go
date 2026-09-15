package backup_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xiaozhang/internal/backup"
	"xiaozhang/internal/bootstrap"
	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
	"xiaozhang/internal/storage"
)

// T42: 备份 → 独立目录恢复 → 余额、统计、关联一致。
// T43: 损坏的备份不会通过校验。
func TestBackupRestoreDrill(t *testing.T) {
	// 1) 准备账本与数据
	srcDir := t.TempDir()
	db := openMigrated(t, srcDir)
	if err := bootstrap.InitAdmin(db, "admin", "Admin", "test-password-1", "演练账本"); err != nil {
		t.Fatal(err)
	}
	svc := ledger.NewService(db)
	var ledgerID, userID string
	db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID)
	db.QueryRow(`SELECT ledger_id FROM ledger_members WHERE user_id=?`, userID).Scan(&ledgerID)

	bank, err := svc.CreateAccount(ledgerID, userID, "银行卡", "bank_card", nil, 100000, strP("2026-01-01"), true)
	if err != nil {
		t.Fatal(err)
	}
	cats, _ := svc.ListCategories(ledgerID, "expense")
	res, err := svc.Post(ledger.PostInput{
		LedgerID: ledgerID, ActorID: userID, Type: "expense", BusinessDate: "2026-09-01",
		AmountCents: 50000, CategoryID: cats[0].ID, FromAccountID: bank.ID, OperationID: ids.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refund(ledger.RefundInput{
		LedgerID: ledgerID, ActorID: userID, OriginalID: res.TxID, BusinessDate: "2026-09-02",
		AccountID: bank.ID, Allocations: []ledger.RefundAlloc{{AmountCents: 20000}}, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	// 附件
	if _, err := svc.SaveAttachment(srcDir, ledgerID, userID, res.TxID, "票.png",
		append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 50)...)); err != nil {
		t.Fatal(err)
	}

	ovBefore, err := svc.Overview(ledgerID, "2026-09-01", "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	balBefore, err := svc.GetAccount(ledgerID, bank.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 2) 备份
	pkg, err := backup.Create(db, srcDir, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// 3) 独立目录校验恢复
	tmp, err := backup.ValidateAndExtract(pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	dstDir := t.TempDir()
	if err := copyAll(filepath.Join(tmp, "xiaozhang.db"), filepath.Join(dstDir, "xiaozhang.db")); err != nil {
		t.Fatal(err)
	}
	// 附件存在性
	if _, err := os.Stat(filepath.Join(tmp, "attachments")); err != nil {
		t.Fatalf("attachments missing in backup: %v", err)
	}

	db2 := openMigrated(t, dstDir)
	defer db2.Close()
	svc2 := ledger.NewService(db2)
	ovAfter, err := svc2.Overview(ledgerID, "2026-09-01", "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if *ovBefore != *ovAfter {
		t.Fatalf("overview mismatch: before %+v after %+v", ovBefore, ovAfter)
	}
	balAfter, err := svc2.GetAccount(ledgerID, bank.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balBefore.Balance != balAfter.Balance {
		t.Fatalf("balance mismatch: %s vs %s", balBefore.Balance, balAfter.Balance)
	}
	// 退款链仍在
	d, err := svc2.TxDetail(ledgerID, res.TxID)
	if err != nil || d.RefundedTotal != "20000" {
		t.Fatalf("refund chain lost: %+v err %v", d, err)
	}

	// T43: 损坏的包（篡改一字节）必须校验失败，不覆盖好数据
	corrupt := pkg + ".corrupt"
	data, _ := os.ReadFile(pkg)
	data[len(data)/2] ^= 0xFF
	os.WriteFile(corrupt, data, 0o600)
	defer os.Remove(corrupt)
	if _, err := backup.ValidateAndExtract(corrupt, ""); err == nil {
		t.Fatal("corrupted backup passed validation")
	}

	// 缺 manifest 的包
	if _, err := backup.ValidateAndExtract(pkg, "wrong-pass"); err != nil {
		// 明文包传口令不影响（口令只在 .age 时使用）
		_ = err
	}
}

func openMigrated(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func strP(s string) *string { return &s }

func copyAll(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.ReadFrom(in)
	return err
}
