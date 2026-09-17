package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"xiaozhang/internal/backup"
	"xiaozhang/internal/bootstrap"
	"xiaozhang/internal/config"
	"xiaozhang/internal/httpapi"
	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
	"xiaozhang/internal/storage"
)

var version = "0.0.0-dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Println(version)
			return
		case "init-admin":
			cmdInitAdmin(os.Args[2:])
			return
		case "reset-password":
			cmdResetPassword(os.Args[2:])
			return
		case "make-superadmin":
			cmdMakeSuperadmin(os.Args[2:])
			return
		case "serve":
			cmdServe(os.Args[2:])
			return
		case "backup":
			cmdBackup(os.Args[2:])
			return
		case "restore":
			cmdRestore(os.Args[2:])
			return
		case "panel":
			cmdPanel(os.Args[2:])
			return
		case "admin":
			cmdAdmin(os.Args[2:])
			return
		case "help", "--help", "-h":
			printUsage()
			return
		}
	}
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
	cmdServe(os.Args[1:])
}

func printUsage() {
	fmt.Print(`小账 xiaozhang — 单文件记账服务与运维工具

用法:
  xiaozhang serve              启动服务（默认）
  xiaozhang admin              终端交互式运维面板（统计/用户/冻结/重置密码/备份/注册开关）
  xiaozhang panel              打印 Web 后台入口地址
  xiaozhang init-admin         初始化管理员
  xiaozhang reset-password     重置用户密码
  xiaozhang make-superadmin    提升平台超管
  xiaozhang backup             备份数据库
  xiaozhang restore            恢复数据库
  xiaozhang version            版本

常用:
  xiaozhang admin              直接在终端管理（自动读 /etc/xiaozhang/env）
`)
}

// cmdPanel 打印运营面板的随机入口 URL（首次运行生成路径并持久化）。
// 用法: xiaozhang panel -data <数据目录>
func cmdPanel(args []string) {
	fs := flag.NewFlagSet("panel", flag.ExitOnError)
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	addr := fs.String("addr", config.Getenv("XIAOZHANG_ADDR", "127.0.0.1:8787"), "service address")
	_ = fs.Parse(args)

	db, err := openDBFull(*data)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	svc := ledger.NewService(db)
	opsPath, err := svc.EnsureOpsPath()
	if err != nil {
		log.Fatalf("ensure ops path: %v", err)
	}
	host := *addr
	if strings.HasPrefix(host, "0.0.0.0") {
		host = "127.0.0.1" + strings.TrimPrefix(host, "0.0.0.0")
	}
	fmt.Printf("\n小账运营面板入口（固定随机路径，登录后可见）:\n\n  http://%s/%s\n\n", host, opsPath)
	// 首次部署生成的初始超管凭据（若文件仍在），随面板入口一并提示
	if creds, err := os.ReadFile(filepath.Join(*data, "initial-admin.txt")); err == nil {
		fmt.Printf("初始平台超管凭据（首次部署生成，建议登录后修改密码并删除该文件）:\n\n%s\n", strings.TrimSpace(string(creds)))
	}
	fmt.Printf("请妥善保管入口地址；泄露后可在面板内「重新生成路径」。\n\n")
}

// writeInitialAdminCreds 把首次部署的超管凭据写入 <dataDir>/initial-admin.txt（0600）。
func writeInitialAdminCreds(dataDir, username, password string) error {
	content := fmt.Sprintf("  用户名: %s\n  密码:   %s\n", username, password)
	return os.WriteFile(filepath.Join(dataDir, "initial-admin.txt"), []byte(content), 0o600)
}

// openDBFull opens the database and applies pending migrations.
func openDBFull(dataDir string) (*sql.DB, error) {
	db, err := storage.Open(dataDir)
	if err != nil {
		return nil, err
	}
	if err := storage.Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// promptSecret reads a password without echoing it.
func promptSecret(prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		log.Fatalf("read password: %v", err)
	}
	return string(b)
}

// cmdInitAdmin creates the first administrator locally (server shell
// required). Password comes from XIAOZHANG_ADMIN_PASSWORD or an
// interactive prompt — never from a CLI argument that lands in history.
func cmdInitAdmin(args []string) {
	fs := flag.NewFlagSet("init-admin", flag.ExitOnError)
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	username := fs.String("username", "", "admin username (required)")
	name := fs.String("name", "", "display name")
	ledgerName := fs.String("ledger", "家庭账本", "default ledger name")
	_ = fs.Parse(args)

	if *username == "" {
		log.Fatal("-username is required")
	}
	password := os.Getenv("XIAOZHANG_ADMIN_PASSWORD")
	if password == "" {
		password = promptSecret("Admin password (min 8 chars): ")
	}
	db, err := openDBFull(*data)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := bootstrap.InitAdmin(db, *username, *name, password, *ledgerName); err != nil {
		log.Fatalf("init admin: %v", err)
	}
	fmt.Println("administrator created; log in via the web UI")
}

// cmdResetPassword recovers access when the admin password is lost.
// Requires server-local execution; revokes existing sessions.
func cmdResetPassword(args []string) {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	username := fs.String("username", "", "username (required)")
	_ = fs.Parse(args)
	if *username == "" {
		log.Fatal("-username is required")
	}
	password := os.Getenv("XIAOZHANG_ADMIN_PASSWORD")
	if password == "" {
		password = promptSecret("New password (min 8 chars): ")
	}
	db, err := openDBFull(*data)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := bootstrap.ResetPassword(db, *username, password); err != nil {
		log.Fatalf("reset password: %v", err)
	}
	fmt.Println("password updated; previous sessions revoked")
}

// cmdMakeSuperadmin promotes a user to platform superadmin (server-local).
// Platform superadmin sees only cross-user stats/metadata, never ledger details.
func cmdMakeSuperadmin(args []string) {
	fs := flag.NewFlagSet("make-superadmin", flag.ExitOnError)
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	username := fs.String("username", "", "username (required)")
	_ = fs.Parse(args)
	if *username == "" {
		log.Fatal("-username is required")
	}
	db, err := openDBFull(*data)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	res, err := db.Exec(`UPDATE users SET platform_role='superadmin' WHERE username=?`, *username)
	if err != nil {
		log.Fatalf("make superadmin: %v", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		log.Fatalf("user not found: %s", *username)
	}
	fmt.Printf("user %s is now platform superadmin\n", *username)
}

// cmdBackup 手动一致性备份。加密口令来自 XIAOZHANG_BACKUP_KEY。
func cmdBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	_ = fs.Parse(args)
	db, err := openDBFull(*data)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	path, err := backup.Create(db, *data, os.Getenv("XIAOZHANG_BACKUP_KEY"), time.Now())
	if err != nil {
		log.Fatalf("backup: %v", err)
	}
	if err := backup.Prune(*data, 14); err != nil {
		log.Printf("prune: %v", err)
	}
	fmt.Println("backup written:", path)
	if os.Getenv("XIAOZHANG_BACKUP_KEY") == "" {
		fmt.Println("note: unencrypted (set XIAOZHANG_BACKUP_KEY to encrypt; keep the passphrase separately)")
	}
}

// cmdRestore 校验式恢复：独立临时目录校验 → 备份当前数据 → 停服换文件。
// 需要应用已停止（CLI 离线操作）；恢复后旧会话失效、恢复世代更新。
func cmdRestore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	yes := fs.Bool("yes", false, "confirm overwriting current data (required)")
	_ = fs.Parse(args)
	pkg := fs.Arg(0)
	if pkg == "" {
		log.Fatal("usage: xiaozhang restore -data <dir> [-yes] <backup.tar.gz[.age]>")
	}
	if !*yes {
		log.Fatal("restore overwrites current data; re-run with -yes after stopping the app")
	}

	tmp, err := backup.ValidateAndExtract(pkg, os.Getenv("XIAOZHANG_BACKUP_KEY"))
	if err != nil {
		log.Fatalf("backup validation failed: %v", err)
	}
	defer os.RemoveAll(tmp)
	log.Printf("backup validated (integrity, foreign keys, balanced entries, checksums)")

	// 先备份当前数据（可回退）
	if _, err := os.Stat(filepath.Join(*data, "xiaozhang.db")); err == nil {
		db, err := openDBFull(*data)
		if err != nil {
			log.Fatalf("open current database: %v", err)
		}
		safety, err := backup.Create(db, *data, "", time.Now())
		db.Close()
		if err != nil {
			log.Fatalf("safety backup of current data failed: %v", err)
		}
		log.Printf("current data backed up to %s", safety)
	}

	// 换文件：先删 WAL/SHM，再原子替换
	for _, suffix := range []string{"-wal", "-shm"} {
		os.Remove(filepath.Join(*data, "xiaozhang.db"+suffix))
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		log.Fatal(err)
	}
	if err := copyFileIO(filepath.Join(tmp, "xiaozhang.db"), filepath.Join(*data, "xiaozhang.db")); err != nil {
		log.Fatalf("restore database: %v", err)
	}
	// 附件
	attSrc := filepath.Join(tmp, "attachments")
	if entries, err := os.ReadDir(attSrc); err == nil {
		dst := filepath.Join(*data, "attachments")
		if err := os.MkdirAll(dst, 0o700); err != nil {
			log.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if err := copyFileIO(filepath.Join(attSrc, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				log.Fatalf("restore attachment %s: %v", e.Name(), err)
			}
		}
	}
	// 恢复世代：旧客户端必须重新对齐，不能盲目重放队列
	db, err := openDBFull(*data)
	if err != nil {
		log.Fatalf("open restored database: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO app_meta(key,value) VALUES('restore_generation',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, ids.New()); err != nil {
		log.Fatalf("set restore generation: %v", err)
	}
	// 恢复后撤销全部会话
	if _, err := db.Exec(`UPDATE sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE revoked_at IS NULL`); err != nil {
		log.Printf("revoke sessions: %v", err)
	}
	fmt.Println("restore complete; all sessions revoked, restore generation updated")
}

func copyFileIO(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", config.Getenv("XIAOZHANG_ADDR", "127.0.0.1:8787"), "listen address")
	data := fs.String("data", config.Getenv("XIAOZHANG_DATA_DIR", "./data"), "data directory")
	secureCookies := fs.Bool("secure-cookies", config.Getenv("XIAOZHANG_SECURE_COOKIES", "") == "1", "mark session cookies Secure (HTTPS)")
	_ = fs.Parse(args)

	cfg := config.Config{
		Addr:          *addr,
		DataDir:       *data,
		Version:       version,
		SecureCookies: *secureCookies,
	}

	db, err := openDBFull(cfg.DataDir)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if needs, err := bootstrap.NeedsInit(db); err == nil && needs {
		log.Printf("no administrator yet; run: xiaozhang init-admin -data %s -username <name>", cfg.DataDir)
	}

	if err := ledger.EnsureSeeds(db); err != nil {
		log.Fatalf("seed library: %v", err)
	}

	// 首次部署/老库升级：无任何平台超管时自动创建一个（admin + 随机密码），
	// 凭据只写入 <data>/initial-admin.txt（0600），不进日志（密钥不进日志约定）。
	if created, uname, pwd, err := ledger.NewService(db).EnsurePlatformSuperadmin(); err != nil {
		log.Printf("ensure platform superadmin: %v", err)
	} else if created {
		if perr := writeInitialAdminCreds(cfg.DataDir, uname, pwd); perr != nil {
			log.Printf("write initial admin credentials: %v", perr)
		} else {
			log.Printf("platform superadmin created: %s (credentials in %s)", uname, filepath.Join(cfg.DataDir, "initial-admin.txt"))
		}
	}

	// 周期扫描：启动补查遗漏（幂等），之后每日扫描；状态在数据库，
	// 不依赖用户打开页面。测试直接调用 ledger.ScanRecurrence 注入时间。
	if err := ledger.ScanRecurrence(db, time.Now()); err != nil {
		log.Printf("recurrence scan: %v", err)
	}
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := ledger.ScanRecurrence(db, time.Now()); err != nil {
				log.Printf("recurrence scan: %v", err)
			}
		}
	}()

	// 每日自动备份（时间可配置，默认 03:17 避开整点）；状态落盘（文件即状态）
	backupTime := config.Getenv("XIAOZHANG_BACKUP_TIME", "03:17")
	go func() {
		for {
			now := time.Now()
			h, m := 3, 17
			fmt.Sscanf(backupTime, "%d:%d", &h, &m)
			next := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			time.Sleep(time.Until(next))
			path, err := backup.Create(db, cfg.DataDir, os.Getenv("XIAOZHANG_BACKUP_KEY"), time.Now())
			if err != nil {
				log.Printf("daily backup failed: %v", err)
				continue
			}
			if err := backup.Prune(cfg.DataDir, 14); err != nil {
				log.Printf("backup prune: %v", err)
			}
			log.Printf("daily backup written: %s", path)
		}
	}()

	srv := httpapi.NewServer(cfg, db)
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("xiaozhang %s listening on http://%s (data: %s)", cfg.Version, cfg.Addr, cfg.DataDir)
		errCh <- httpSrv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}
}
