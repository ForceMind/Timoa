package ledger_test

import (
	"testing"

	"xiaozhang/internal/ids"
	"xiaozhang/internal/impexp"
	"xiaozhang/internal/ledger"
)

// T38/T39：导入预览去重、失败交易不计、重复批次幂等、批次撤销检查依赖。
func TestImportFlow(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 0)
	cat := e.categoryID(t, "expense", "餐饮")

	rows := []impexp.RawRow{
		{Index: 1, Date: "2026-09-01", Type: "expense", Amount: "28.00", Merchant: "麦当劳", TxNo: "wechat:A1"},
		{Index: 2, Date: "2026-09-02", Type: "expense", Amount: "15.50", Merchant: "滴滴", TxNo: "wechat:A2"},
		{Index: 3, Date: "2026-09-03", Type: "expense", Amount: "28.00", Merchant: "麦当劳"},          // 无单号
		{Index: 4, Date: "2026-09-04", Type: "expense", Amount: "0.00", Err: "金额无法解析"},           // 失败行
		{Index: 5, Date: "2026-09-05", Type: "expense", Amount: "9.90", Skip: true, SkipWhy: "已关闭"}, // 状态跳过
	}

	b, rows, err := e.svc.PreviewImport(e.ledgerID, e.userID, "wechat", "wx.csv", rows)
	if err != nil {
		t.Fatal(err)
	}
	if b.Importable != 3 || b.Failed != 1 {
		t.Fatalf("batch = %+v", b)
	}
	// 预览不改正式账：汇总为 0
	ov, _ := e.svc.Overview(e.ledgerID, "2026-09-01", "2026-10-01")
	if ov.NetExpenseCents != "0" {
		t.Fatalf("preview touched books: %+v", ov)
	}

	// 确认入账
	if _, err := e.svc.ConfirmImport(e.ledgerID, e.userID, b.ID, cat, bank.ID); err != nil {
		t.Fatal(err)
	}
	ov, _ = e.svc.Overview(e.ledgerID, "2026-09-01", "2026-10-01")
	if ov.NetExpenseCents != "7150" { // 28.00+15.50+28.00
		t.Fatalf("after confirm net = %s, want 7150", ov.NetExpenseCents)
	}
	// 重复确认幂等
	b2, err := e.svc.ConfirmImport(e.ledgerID, e.userID, b.ID, cat, bank.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b2.Imported != 3 {
		t.Fatalf("re-confirm imported = %d", b2.Imported)
	}
	txs, _ := e.svc.ListTransactions(e.ledgerID, 20)
	if len(txs) != 3 {
		t.Fatalf("txs = %d, want 3", len(txs))
	}
	// 导入记录 date_precision = day（不伪造小时）
	var prec string
	if err := e.db.QueryRow(`SELECT date_precision FROM transactions WHERE source_tx_no='wechat:A1'`).Scan(&prec); err != nil || prec != "day" {
		t.Fatalf("precision = %s err %v", prec, err)
	}

	// 同单号再次导入 → 强去重
	rows2 := []impexp.RawRow{{Index: 1, Date: "2026-09-01", Type: "expense", Amount: "28.00", Merchant: "麦当劳", TxNo: "wechat:A1"}}
	b3, rows2, err := e.svc.PreviewImport(e.ledgerID, e.userID, "wechat", "wx2.csv", rows2)
	if err != nil {
		t.Fatal(err)
	}
	if b3.Importable != 0 || !rows2[0].Skip {
		t.Fatalf("dedup failed: %+v / %+v", b3, rows2[0])
	}
	_ = rows

	// 批次撤销：有退款关联的单据被阻止
	txA1, _ := e.svc.SearchTransactions(e.ledgerID, "麦当劳", 1)
	if _, err := e.svc.Refund(ledger.RefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: txA1[0].ID, BusinessDate: "2026-09-10",
		AccountID: bank.ID, Allocations: []ledger.RefundAlloc{{AmountCents: 500}}, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	_, blocked, err := e.svc.UndoImportBatch(e.ledgerID, e.userID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 1 {
		t.Fatalf("blocked = %v, want 1 (有退款的单据阻止批次硬删)", blocked)
	}
	// 其余两单已留痕冲正
	ov, _ = e.svc.Overview(e.ledgerID, "2026-09-01", "2026-10-01")
	if ov.NetExpenseCents != "2300" { // 28.00 - 5.00 退款
		t.Fatalf("after undo net = %s, want 2300", ov.NetExpenseCents)
	}
}

// T41（部分）：附件类型校验、越权拒绝。
func TestAttachments(t *testing.T) {
	e := newTestEnv(t)
	dir := t.TempDir()
	bank := e.account(t, "银行卡", "bank_card", 0)
	cat := e.categoryID(t, "expense", "餐饮")
	tx := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-01", AmountCents: 2800,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})

	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 100)...)
	a, err := e.svc.SaveAttachment(dir, e.ledgerID, e.userID, tx.TxID, "小票.png", png)
	if err != nil {
		t.Fatal(err)
	}
	if a.ContentType != "image/png" {
		t.Fatalf("content type = %s", a.ContentType)
	}
	// 假图片（内容为 EXE）
	exe := []byte("MZ this is not an image")
	if _, err := e.svc.SaveAttachment(dir, e.ledgerID, e.userID, tx.TxID, "evil.png", exe); err == nil {
		t.Fatal("fake png accepted")
	}
	// 超大拒绝
	if _, err := e.svc.SaveAttachment(dir, e.ledgerID, e.userID, tx.TxID, "big.png", append([]byte{0x89, 'P', 'N', 'G'}, make([]byte, 6<<20)...)); err == nil {
		t.Fatal("oversize accepted")
	}
	// 跨账本附件拒绝
	e2 := newTestEnv(t)
	if _, err := e2.svc.SaveAttachment(dir, e2.ledgerID, e2.userID, tx.TxID, "x.png", png); err == nil {
		t.Fatal("cross-ledger attachment accepted")
	}
	// 路径读取
	p, ct, err := e.svc.AttachmentPath(dir, e.ledgerID, a.ID)
	if err != nil || ct != "image/png" || p == "" {
		t.Fatalf("path = %s ct = %s err %v", p, ct, err)
	}
}

// 导出口径：与界面一致（净额含退款）。
func TestExportRows(t *testing.T) {
	e := newTestEnv(t)
	bank := e.account(t, "银行卡", "bank_card", 0)
	cat := e.categoryID(t, "expense", "餐饮")
	orig := e.post(t, ledger.PostInput{Type: "expense", BusinessDate: "2026-09-01", AmountCents: 50000,
		CategoryID: cat, FromAccountID: bank.ID, OperationID: ids.New()})
	if _, err := e.svc.Refund(ledger.RefundInput{
		LedgerID: e.ledgerID, ActorID: e.userID, OriginalID: orig.TxID, BusinessDate: "2026-09-02",
		AccountID: bank.ID, Allocations: []ledger.RefundAlloc{{AmountCents: 20000}}, OperationID: ids.New(),
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := e.svc.ExportRows(e.ledgerID, "2026-09-01", "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 { // 原单 + 退款单
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][2] != "500.00" || rows[0][3] != "300.00" {
		t.Fatalf("row = %v, want 原 500.00 净 300.00", rows[0])
	}
}
