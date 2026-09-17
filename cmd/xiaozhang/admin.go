package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xiaozhang/internal/auth"
	"xiaozhang/internal/config"
	"xiaozhang/internal/ids"
	"xiaozhang/internal/ledger"
)

// admin.go: 终端交互式运维面板（xiaozhang admin）。
// 面向自托管部署者：在服务器 shell 里直接管理，无需开浏览器。
// 所有操作走与 Web 后台相同的 Service，关键动作由对应 Service 写审计。

// cmdAdmin 进入交互式终端运维面板。
// 用法: xiaozhang admin [-data <数据目录>]
// 数据目录解析优先级：-data 参数 > 环境变量 XIAOZHANG_DATA_DIR > /etc/xiaozhang/env > ./data。
func cmdAdmin(args []string) {
	fs := flag.NewFlagSet("admin", flag.ExitOnError)
	data := fs.String("data", "", "data directory")
	_ = fs.Parse(args)
	dataDir := resolveDataDir(*data)

	db, err := openDBFull(dataDir)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	svc := ledger.NewService(db)
	a := &adminUI{db: db, svc: svc, dataDir: dataDir, r: bufio.NewReader(os.Stdin)}

	fmt.Println("\n=== 小账 终端运维面板 ===")
	fmt.Printf("数据目录: %s\n\n", dataDir)
	for {
		fmt.Println("------------------------------")
		fmt.Println(" 1) 全局统计")
		fmt.Println(" 2) 用户列表")
		fmt.Println(" 3) 查看用户数据（账户+流水）")
		fmt.Println(" 4) 冻结 / 解冻用户")
		fmt.Println(" 5) 重置用户密码")
		fmt.Println(" 6) 切换公开注册开关")
		fmt.Println(" 7) 立即备份数据库")
		fmt.Println(" 8) 查看后台入口地址")
		fmt.Println(" 9) 提升用户为超管")
		fmt.Println(" 0) 退出")
		switch a.prompt("请选择: ") {
		case "1":
			a.showStats()
		case "2":
			a.listUsers()
		case "3":
			a.viewUser()
		case "4":
			a.freezeUser()
		case "5":
			a.resetPassword()
		case "6":
			a.toggleRegistration()
		case "7":
			a.backup()
		case "8":
			a.showPanelURL()
		case "9":
			a.makeSuperadmin()
		case "0", "q", "exit":
			fmt.Println("再见。")
			return
		default:
			fmt.Println("无效选择。")
		}
	}
}

type adminUI struct {
	db      *sql.DB
	svc     *ledger.Service
	dataDir string
	r       *bufio.Reader
}

func (a *adminUI) prompt(label string) string {
	fmt.Print(label)
	line, _ := a.r.ReadString('\n')
	return strings.TrimSpace(line)
}

// audit 写一条终端运维操作到 audit_log（ops.terminal.*），挂在目标用户主账本下。
func (a *adminUI) audit(actorID, ledgerID, action, entityID string, detail any) {
	if ledgerID == "" {
		return
	}
	b, _ := json.Marshal(detail)
	_, _ = a.db.Exec(`INSERT INTO audit_log(id,ledger_id,actor_user_id,action,entity_type,entity_id,detail)
		VALUES(?,?,?,?,?,?,?)`, ids.New(), ledgerID, actorID, action, "user", entityID, string(b))
}

func (a *adminUI) showStats() {
	st, _, err := a.svc.PlatformOverview()
	if err != nil {
		fmt.Println("读取失败:", err)
		return
	}
	open, _ := a.svc.RegistrationOpen()
	fmt.Printf("\n注册用户: %d   账本: %d   流水: %d   公开注册: %v\n\n",
		st.UserCount, st.LedgerCount, st.TxCount, open)
}

func (a *adminUI) listUsers() {
	_, users, err := a.svc.PlatformOverview()
	if err != nil {
		fmt.Println("读取失败:", err)
		return
	}
	fmt.Printf("\n%-16s %-14s %-6s %-6s %-6s %s\n", "用户名", "显示名", "账本", "流水", "状态", "角色")
	for _, u := range users {
		st := "正常"
		if u.Archived {
			st = "已冻结"
		}
		role := ""
		if u.PlatformRole == "superadmin" {
			role = "超管"
		}
		fmt.Printf("%-16s %-14s %-6d %-6d %-6s %s\n", u.Username, u.DisplayName, u.LedgerCount, u.TxCount, st, role)
	}
	fmt.Println()
}

func (a *adminUI) viewUser() {
	username := a.prompt("输入用户名: ")
	if username == "" {
		return
	}
	u := a.findUser(username)
	if u == nil {
		fmt.Println("未找到用户:", username)
		return
	}
	ledgerID := a.primaryLedger(u.UserID)
	if ledgerID == "" {
		fmt.Println("该用户暂无账本。")
		return
	}
	accounts, err := a.svc.ListAccounts(ledgerID)
	if err != nil {
		fmt.Println("读取账户失败:", err)
		return
	}
	fmt.Printf("\n== %s（@%s）账户 ==\n", u.DisplayName, u.Username)
	n := 0
	for _, ac := range accounts {
		if ac.ParentID != "" {
			continue
		}
		n++
		fmt.Printf("  [%s] %s  余额 %s 分\n", ac.Type, ac.Name, ac.Balance)
	}
	if n == 0 {
		fmt.Println("  （无账户）")
	}
	txs, err := a.svc.ListTransactions(ledgerID, 20)
	if err == nil {
		fmt.Printf("\n== 最近 %d 笔流水 ==\n", len(txs))
		for _, t := range txs {
			fmt.Printf("  %s  %-8s %s  %s %s\n", t.BusinessDate, t.Type, t.Amount, t.CategoryName, t.Note)
		}
	}
	fmt.Println()
	a.audit(u.UserID, ledgerID, "ops.terminal.view_user", u.UserID, map[string]any{"username": u.Username})
}

func (a *adminUI) freezeUser() {
	username := a.prompt("输入用户名: ")
	u := a.findUser(username)
	if u == nil {
		fmt.Println("未找到用户:", username)
		return
	}
	target := !u.Archived
	word := "冻结"
	if !target {
		word = "解冻"
	}
	if a.prompt(fmt.Sprintf("确认%s @%s？(y/N): ", word, u.Username)) != "y" {
		fmt.Println("已取消。")
		return
	}
	if err := a.svc.SetUserArchived(u.UserID, target); err != nil {
		fmt.Println("操作失败:", err)
		return
	}
	if target {
		_, _ = a.db.Exec(`UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`,
			time.Now().UTC(), u.UserID)
	}
	fmt.Printf("已%s @%s。\n", word, u.Username)
	act := "ops.terminal.freeze_user"
	if !target {
		act = "ops.terminal.unfreeze_user"
	}
	a.audit(u.UserID, a.primaryLedger(u.UserID), act, u.UserID, map[string]any{"freeze": target})
}

func (a *adminUI) resetPassword() {
	username := a.prompt("输入用户名: ")
	u := a.findUser(username)
	if u == nil {
		fmt.Println("未找到用户:", username)
		return
	}
	if a.prompt(fmt.Sprintf("确认重置 @%s 的密码？(y/N): ", u.Username)) != "y" {
		fmt.Println("已取消。")
		return
	}
	newPassword := ids.Token(8)
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		fmt.Println("哈希失败:", err)
		return
	}
	if _, err := a.db.Exec(`UPDATE users SET password_hash=?, must_change_password=1 WHERE id=?`, hash, u.UserID); err != nil {
		fmt.Println("重置失败:", err)
		return
	}
	_, _ = a.db.Exec(`UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, time.Now().UTC(), u.UserID)
	fmt.Printf("\n已重置。新密码（仅此一次显示，请转交用户）:\n\n  %s\n\n", newPassword)
	a.audit(u.UserID, a.primaryLedger(u.UserID), "ops.terminal.reset_password", u.UserID, nil)
}

func (a *adminUI) toggleRegistration() {
	open, err := a.svc.RegistrationOpen()
	if err != nil {
		fmt.Println("读取失败:", err)
		return
	}
	if err := a.svc.SetRegistrationOpen(!open); err != nil {
		fmt.Println("切换失败:", err)
		return
	}
	if !open {
		fmt.Println("公开注册已开启。")
	} else {
		fmt.Println("公开注册已关闭。")
	}
}

func (a *adminUI) backup() {
	dir := filepath.Join(a.dataDir, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Println("创建备份目录失败:", err)
		return
	}
	name := fmt.Sprintf("xiaozhang-%s.db", time.Now().UTC().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if _, err := a.db.Exec(fmt.Sprintf(`VACUUM INTO '%s'`, strings.ReplaceAll(dst, "'", "''"))); err != nil {
		fmt.Println("备份失败:", err)
		return
	}
	fmt.Printf("备份完成: %s\n", dst)
}

func (a *adminUI) showPanelURL() {
	opsPath, err := a.svc.EnsureOpsPath()
	if err != nil {
		fmt.Println("读取失败:", err)
		return
	}
	addr := config.Getenv("XIAOZHANG_ADDR", "127.0.0.1:8787")
	host := addr
	if strings.HasPrefix(host, "0.0.0.0") {
		host = "127.0.0.1" + strings.TrimPrefix(host, "0.0.0.0")
	}
	fmt.Printf("\nWeb 后台入口:\n\n  http://%s/%s\n\n", host, opsPath)
}

func (a *adminUI) makeSuperadmin() {
	username := a.prompt("输入用户名: ")
	if username == "" {
		return
	}
	res, err := a.db.Exec(`UPDATE users SET platform_role='superadmin' WHERE username=?`, username)
	if err != nil {
		fmt.Println("操作失败:", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fmt.Println("未找到用户:", username)
		return
	}
	fmt.Printf("已将 @%s 提升为平台超管。\n", username)
}

// findUser 按用户名查用户概要。
func (a *adminUI) findUser(username string) *ledger.PlatformUser {
	_, users, err := a.svc.PlatformOverview()
	if err != nil {
		return nil
	}
	for _, u := range users {
		if u.Username == username {
			cp := u
			return &cp
		}
	}
	return nil
}

// primaryLedger 返回用户主账本 id。
func (a *adminUI) primaryLedger(userID string) string {
	var id string
	_ = a.db.QueryRow(`SELECT l.id FROM ledgers l JOIN ledger_members m ON m.ledger_id=l.id
		WHERE m.user_id=? AND l.owner_user_id=? ORDER BY l.created_at LIMIT 1`, userID, userID).Scan(&id)
	if id == "" {
		_ = a.db.QueryRow(`SELECT ledger_id FROM ledger_members WHERE user_id=? LIMIT 1`, userID).Scan(&id)
	}
	return id
}

// resolveDataDir 解析数据目录。显式 -data 优先；其次环境变量；再次尝试读
// systemd env 文件 /etc/xiaozhang/env（install.sh 部署）；最后回退 ./data。
func resolveDataDir(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("XIAOZHANG_DATA_DIR"); v != "" {
		return v
	}
	if b, err := os.ReadFile("/etc/xiaozhang/env"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "XIAOZHANG_DATA_DIR=") {
				if v := strings.TrimSpace(strings.TrimPrefix(line, "XIAOZHANG_DATA_DIR=")); v != "" {
					return v
				}
			}
		}
	}
	return "./data"
}
