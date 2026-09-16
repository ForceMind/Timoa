package ledger_test

import (
	"testing"

	"xiaozhang/internal/ledger"
)

// TestNotesCRUD：便笺是非账务内容——增删改查正常、不进统计；
// 跨账本必须不可见（服务端按 ledger_id 隔离）。
func TestNotesCRUD(t *testing.T) {
	e := newTestEnv(t)

	n, err := e.svc.CreateNote(e.ledgerID, e.userID, "周五记得交物业费")
	if err != nil {
		t.Fatal(err)
	}
	if n.ID == "" || n.CreatedAt == "" {
		t.Fatalf("note not materialized: %+v", n)
	}

	// 列表：置顶在前
	n2, err := e.svc.CreateNote(e.ledgerID, e.userID, "购物清单：牛奶、鸡蛋")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.UpdateNote(e.ledgerID, n2.ID, "购物清单：牛奶、鸡蛋、面包", true); err != nil {
		t.Fatal(err)
	}
	notes, err := e.svc.ListNotes(e.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].ID != n2.ID || !notes[0].Pinned {
		t.Fatalf("pinned first expected: %+v", notes)
	}
	if notes[0].Content != "购物清单：牛奶、鸡蛋、面包" {
		t.Fatalf("content not updated: %q", notes[0].Content)
	}

	// 校验：空内容 / 超长
	if _, err := e.svc.CreateNote(e.ledgerID, e.userID, ""); err == nil {
		t.Fatal("empty content accepted")
	}
	long := make([]rune, 2001)
	for i := range long {
		long[i] = '字'
	}
	if _, err := e.svc.CreateNote(e.ledgerID, e.userID, string(long)); err == nil {
		t.Fatal("overlong content accepted")
	}

	// 跨账本隔离：另一个账本看不到也改不到
	e2 := newTestEnv(t)
	other, err := e2.svc.ListNotes(e2.ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("cross-ledger notes visible")
	}
	if err := e.svc.UpdateNote(e2.ledgerID, n.ID, "x", false); err != ledger.ErrNotFound {
		t.Fatalf("cross-ledger update: got %v, want ErrNotFound", err)
	}
	if err := e.svc.DeleteNote(e2.ledgerID, n.ID); err != ledger.ErrNotFound {
		t.Fatalf("cross-ledger delete: got %v, want ErrNotFound", err)
	}

	// 删除
	if err := e.svc.DeleteNote(e.ledgerID, n.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteNote(e.ledgerID, n.ID); err != ledger.ErrNotFound {
		t.Fatalf("double delete: got %v, want ErrNotFound", err)
	}
}
