package ledger

import (
	"database/sql"
	"errors"

	"xiaozhang/internal/auth"
	"xiaozhang/internal/ids"
)

// platform.go: 多租户 SaaS 形态的平台层——公开注册（注册即建独立账本）
// 与平台级超管的跨用户统计/元数据查询。
//
// 隐私边界（重要）：平台超管只能看统计与元数据（用户数、账本数、交易笔数、
// 存储占用、注册时间），看不到任何账本的明细内容；账目明细仍严格按
// ledger_members 隔离。platform_role='superadmin' 与账本内 role 是两个维度。

// RegistrationOpen 报告公开注册是否开启（platform_settings.registration_open）。
func (s *Service) RegistrationOpen() (bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM platform_settings WHERE key='registration_open'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil // 迁移默认插入 '1'；缺行时按开启处理
	}
	if err != nil {
		return false, err
	}
	return v == "1", nil
}

// SetRegistrationOpen 切换公开注册开关（超管操作）。
func (s *Service) SetRegistrationOpen(open bool) error {
	v := "0"
	if open {
		v = "1"
	}
	_, err := s.db.Exec(`INSERT INTO platform_settings(key,value) VALUES('registration_open',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, v)
	return err
}

// EnsureOpsPath 返回运营面板的随机路径（不带前导斜杠）；首次调用时生成并持久化，
// 之后固定不变。CLI 与 serve 启动都经此，保证入口稳定。
func (s *Service) EnsureOpsPath() (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM platform_settings WHERE key='ops_path'`).Scan(&v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if v != "" {
		return v, nil
	}
	v = "ops-" + ids.Token(4) // 如 ops-x7k9p2qm，8 位随机 hex
	if _, err := s.db.Exec(`INSERT INTO platform_settings(key,value) VALUES('ops_path',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, v); err != nil {
		return "", err
	}
	return v, nil
}

// RegenerateOpsPath 重新生成随机路径（旧入口立即失效），返回新路径。
func (s *Service) RegenerateOpsPath() (string, error) {
	v := "ops-" + ids.Token(4)
	_, err := s.db.Exec(`UPDATE platform_settings SET value=? WHERE key='ops_path'`, v)
	return v, err
}

// RegisterUser 公开注册：创建用户 + 其独立账本 + admin 成员关系 + 种子。
// 每个注册用户拥有自己的账本，互相隔离；家庭共享仍走账本内邀请。
func (s *Service) RegisterUser(username, displayName, password string) (userID string, err error) {
	open, err := s.RegistrationOpen()
	if err != nil {
		return "", err
	}
	if !open {
		return "", errf(403, "registration_closed", "registration is closed")
	}
	if username == "" || password == "" {
		return "", errf(400, "invalid_input", "username and password are required")
	}
	if len(password) < 8 {
		return "", errf(400, "invalid_input", "password must be at least 8 characters")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	userID = ids.New()
	if displayName == "" {
		displayName = username
	}
	if _, err := tx.Exec(`INSERT INTO users(id,username,display_name,password_hash) VALUES(?,?,?,?)`,
		userID, username, displayName, hash); err != nil {
		return "", errf(409, "username_taken", "username already exists")
	}
	ledgerID := ids.New()
	if _, err := tx.Exec(`INSERT INTO ledgers(id,name) VALUES(?,?)`, ledgerID, "我的账本"); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO ledger_members(ledger_id,user_id,role) VALUES(?,?,'admin')`,
		ledgerID, userID); err != nil {
		return "", err
	}
	if err := SeedCoreCategories(tx, ledgerID); err != nil {
		return "", err
	}
	if err := SeedLibrary(tx, ledgerID); err != nil {
		return "", err
	}
	for _, sd := range []struct{ id, kind, code, name string }{
		{"seed-recv-" + ledgerID, "asset", "asset:receivable", "应收款项"},
		{"seed-pay-" + ledgerID, "liability", "liability:payable", "应付款项"},
	} {
		if _, err := tx.Exec(`INSERT INTO subjects(id,ledger_id,kind,code,name) VALUES(?,?,?,?,?)`,
			sd.id, ledgerID, sd.kind, sd.code, sd.name); err != nil {
			return "", err
		}
	}
	// 默认「现金」资金账户：注册即可直接记账，无需先建账户（复式分录需要资金科目）。
	// 用户可随时在我的 → 资金账户里新增/归档。
	if err := seedDefaultCashAccount(tx, ledgerID, userID); err != nil {
		return "", err
	}
	if err := audit(tx, ledgerID, userID, "user.register", "user", userID, map[string]any{"username": username}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return userID, nil
}

// seedDefaultCashAccount 在注册事务内建一个默认「现金」账户（现金类型资产），
// 与 CreateAccount 的分录结构一致，但复用外层事务、期初余额 0、未确认。
func seedDefaultCashAccount(tx *sql.Tx, ledgerID, actorID string) error {
	acctID := ids.New()
	subID := ids.New()
	if _, err := tx.Exec(`INSERT INTO subjects(id,ledger_id,kind,code,name) VALUES(?,?,?,?,?)`,
		subID, ledgerID, "asset", subjectCode("asset:acct:", acctID), "现金"); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO accounts(id,ledger_id,subject_id,name,type,holder_user_id,opening_balance_cents,opening_date,balance_confirmed)
		VALUES(?,?,?,?,'cash',NULL,0,NULL,0)`, acctID, ledgerID, subID, "现金"); err != nil {
		return err
	}
	return audit(tx, ledgerID, actorID, "account.create", "account", acctID, map[string]any{"name": "现金", "type": "cash", "seed": true})
}

// PlatformUser 平台视角的用户元数据（不含任何账目明细）。
type PlatformUser struct {
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	PlatformRole string `json:"platform_role"`
	CreatedAt    string `json:"created_at"`
	Archived     bool   `json:"archived"`
	LedgerCount  int    `json:"ledger_count"`
	TxCount      int    `json:"tx_count"`
}

// PlatformStats 全局聚合统计。
type PlatformStats struct {
	UserCount   int `json:"user_count"`
	LedgerCount int `json:"ledger_count"`
	TxCount     int `json:"tx_count"`
}

// PlatformOverview 返回全局统计 + 用户列表（仅元数据，超管专用）。
func (s *Service) PlatformOverview() (*PlatformStats, []PlatformUser, error) {
	var st PlatformStats
	if err := s.db.QueryRow(`SELECT
		(SELECT COUNT(1) FROM users WHERE archived_at IS NULL),
		(SELECT COUNT(1) FROM ledgers),
		(SELECT COUNT(1) FROM transactions)`).Scan(&st.UserCount, &st.LedgerCount, &st.TxCount); err != nil {
		return nil, nil, err
	}
	rows, err := s.db.Query(`SELECT u.id,u.username,u.display_name,u.platform_role,u.created_at,
		(u.archived_at IS NOT NULL),
		(SELECT COUNT(1) FROM ledger_members m WHERE m.user_id=u.id),
		(SELECT COUNT(1) FROM transactions t JOIN ledger_members m2 ON m2.ledger_id=t.ledger_id WHERE m2.user_id=u.id)
		FROM users u ORDER BY u.created_at DESC`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	users := []PlatformUser{}
	for rows.Next() {
		var u PlatformUser
		if err := rows.Scan(&u.UserID, &u.Username, &u.DisplayName, &u.PlatformRole, &u.CreatedAt,
			&u.Archived, &u.LedgerCount, &u.TxCount); err != nil {
			return nil, nil, err
		}
		users = append(users, u)
	}
	return &st, users, rows.Err()
}

// IsSuperadmin 报告用户是否为平台超管。
func (s *Service) IsSuperadmin(userID string) (bool, error) {
	var role string
	err := s.db.QueryRow(`SELECT platform_role FROM users WHERE id=?`, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errf(404, "not_found", "user not found")
	}
	if err != nil {
		return false, err
	}
	return role == "superadmin", nil
}

// SetUserArchived 平台层冻结/解冻用户（归档即吊销其全部会话，由调用方处理）。
func (s *Service) SetUserArchived(userID string, archived bool) error {
	var err error
	if archived {
		_, err = s.db.Exec(`UPDATE users SET archived_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, userID)
	} else {
		_, err = s.db.Exec(`UPDATE users SET archived_at=NULL WHERE id=?`, userID)
	}
	return err
}

// PlatformResetPassword 超管重置某用户密码（吊销其会话由调用方处理）。
func (s *Service) PlatformResetPassword(userID, newPassword string) error {
	if len(newPassword) < 8 {
		return errf(400, "invalid_input", "password must be at least 8 characters")
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errf(404, "not_found", "user not found")
	}
	return nil
}
