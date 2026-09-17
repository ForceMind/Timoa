package ledger_test

import (
	"strings"
	"testing"

	"xiaozhang/internal/auth"
)

// TestPlatformRegisterAndOverview：公开注册（独立账本、隔离）+ 平台超管
// 只看元数据统计，不看明细；注册开关可关。
func TestPlatformRegisterAndOverview(t *testing.T) {
	e := newTestEnv(t)

	// 默认注册开放
	open, err := e.svc.RegistrationOpen()
	if err != nil || !open {
		t.Fatalf("registration should default open: open=%v err=%v", open, err)
	}

	// 注册新用户 → 独立账本
	uid, err := e.svc.RegisterUser("alice", "Alice", "password-123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	var ledgerID, role string
	if err := e.db.QueryRow(`SELECT ledger_id,role FROM ledger_members WHERE user_id=?`, uid).Scan(&ledgerID, &role); err != nil {
		t.Fatalf("new user has own ledger: %v", err)
	}
	if ledgerID == e.ledgerID {
		t.Fatal("new user must get an independent ledger, not the admin's")
	}
	if role != "admin" {
		t.Fatalf("registrant should be admin of own ledger, got %q", role)
	}

	// 注册即建默认「现金」账户（开箱即可记账）
	var cashAcct int
	if err := e.db.QueryRow(`SELECT COUNT(1) FROM accounts WHERE ledger_id=? AND type='cash' AND name='现金'`, ledgerID).Scan(&cashAcct); err != nil {
		t.Fatal(err)
	}
	if cashAcct != 1 {
		t.Fatalf("default cash account missing, got %d", cashAcct)
	}

	// 用户名唯一约束
	if _, err := e.svc.RegisterUser("alice", "A2", "password-456"); err == nil {
		t.Fatal("duplicate username accepted")
	}

	// 弱密码拒绝
	if _, err := e.svc.RegisterUser("bob", "Bob", "short"); err == nil {
		t.Fatal("short password accepted")
	}

	// 平台总览：2 用户（admin + alice）、2 账本；元数据不含明细字段
	stats, users, err := e.svc.PlatformOverview()
	if err != nil {
		t.Fatal(err)
	}
	if stats.UserCount != 2 || stats.LedgerCount != 2 {
		t.Fatalf("stats wrong: %+v", stats)
	}
	if len(users) != 2 {
		t.Fatalf("users: %+v", users)
	}

	// 初始无人是超管；提升后可见
	ok, err := e.svc.IsSuperadmin(e.userID)
	if err != nil || ok {
		t.Fatalf("admin is not platform superadmin by default: ok=%v err=%v", ok, err)
	}
	if _, err := e.db.Exec(`UPDATE users SET platform_role='superadmin' WHERE id=?`, e.userID); err != nil {
		t.Fatal(err)
	}
	ok, err = e.svc.IsSuperadmin(e.userID)
	if err != nil || !ok {
		t.Fatalf("after promote should be superadmin: ok=%v err=%v", ok, err)
	}

	// 关闭注册后拒绝
	if err := e.svc.SetRegistrationOpen(false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RegisterUser("carol", "Carol", "password-789"); err == nil {
		t.Fatal("registration should be closed")
	}
}

// TestPlatformFreezeAndReset：冻结用户 + 超管重置密码（会话吊销在 http 层）。
func TestPlatformFreezeAndReset(t *testing.T) {
	e := newTestEnv(t)
	uid, err := e.svc.RegisterUser("dave", "Dave", "password-123")
	if err != nil {
		t.Fatal(err)
	}

	// 冻结
	if err := e.svc.SetUserArchived(uid, true); err != nil {
		t.Fatal(err)
	}
	var archived interface{}
	if err := e.db.QueryRow(`SELECT archived_at FROM users WHERE id=?`, uid).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived == nil {
		t.Fatal("user should be archived")
	}
	// 解冻
	if err := e.svc.SetUserArchived(uid, false); err != nil {
		t.Fatal(err)
	}

	// 重置密码
	if err := e.svc.PlatformResetPassword(uid, "new-password-456"); err != nil {
		t.Fatal(err)
	}
	// 不存在的用户
	if err := e.svc.PlatformResetPassword("no-such-id", "whatever-123"); err == nil {
		t.Fatal("reset for missing user should fail")
	}
}

// TestEnsurePlatformSuperadmin：无任何平台超管时自动创建 admin + 随机密码（首次部署），
// 密码可登录、role=superadmin、配独立账本与默认现金账户；已有超管时不重复创建。
func TestEnsurePlatformSuperadmin(t *testing.T) {
	e := newTestEnv(t)

	// 初始（newTestEnv 只建了账本 admin，不是平台超管）→ 应创建一个
	created, uname, pwd, err := e.svc.EnsurePlatformSuperadmin()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !created {
		t.Fatal("should create superadmin when none exists")
	}
	// newTestEnv 已占用 admin（账本用户），故此处应为 admin- 前缀；空库则是 admin
	if uname != "admin" && !strings.HasPrefix(uname, "admin-") {
		t.Fatalf("username should be admin or admin-<suffix>, got %q", uname)
	}
	if len(pwd) < 16 {
		t.Fatalf("password too weak: %q", pwd)
	}

	// 账号确为平台超管，且密码可校验通过
	var uid, role, hash string
	if err := e.db.QueryRow(`SELECT id,platform_role,password_hash FROM users WHERE username=?`, uname).Scan(&uid, &role, &hash); err != nil {
		t.Fatal(err)
	}
	if role != "superadmin" {
		t.Fatalf("role should be superadmin, got %q", role)
	}
	if !auth.VerifyPassword(pwd, hash) {
		t.Fatal("returned password does not verify against stored hash")
	}

	// 有独立账本 + 默认现金账户
	var ledgerID string
	if err := e.db.QueryRow(`SELECT ledger_id FROM ledger_members WHERE user_id=? AND role='admin'`, uid).Scan(&ledgerID); err != nil {
		t.Fatalf("superadmin has own ledger: %v", err)
	}
	var cash int
	if err := e.db.QueryRow(`SELECT COUNT(1) FROM accounts WHERE ledger_id=? AND type='cash'`, ledgerID).Scan(&cash); err != nil || cash != 1 {
		t.Fatalf("default cash account missing: cash=%d err=%v", cash, err)
	}

	// 再次调用：已有超管 → 不重复创建
	created2, _, _, err := e.svc.EnsurePlatformSuperadmin()
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Fatal("should not create a second superadmin")
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(1) FROM users WHERE platform_role='superadmin'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("expect exactly 1 superadmin, got %d err=%v", n, err)
	}
}
