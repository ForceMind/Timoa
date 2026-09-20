// Package ledger implements the double-entry accounting core.
//
// Invariants (enforced here and covered by tests):
//   - every transaction's entries balance (sum debits == sum credits)
//   - transaction + entries + audit commit in ONE database transaction
//   - idempotent create: same operation_id + same content returns the
//     original; same id + different content is a conflict
//   - transfers between own accounts never touch income/expense subjects
//   - posted records are immutable (corrections arrive via the reversal
//     chain in a later stage; there is no update/delete of posted rows)
package ledger

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"xiaozhang/internal/ids"
	"xiaozhang/internal/money"
)

// Account types whose balance is a debt (liability subjects).
var liabilityTypes = map[string]bool{
	"credit_card":    true,
	"huabei":         true,
	"loan_liability": true,
}

// Error is an API-facing failure with a stable machine code.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	HTTP    int    `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func errf(http int, code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), HTTP: http}
}

var (
	ErrNotFound      = errf(404, "not_found", "resource not found")
	ErrCrossLedger   = errf(403, "cross_ledger", "resource belongs to another ledger")
	ErrInvalidAmount = errf(400, "invalid_amount", "invalid amount")
)

// Service owns ledger writes. The write mutex serializes posting inside
// this single-process monolith; the database remains the final guard via
// constraints and transactions.
type Service struct {
	db *sql.DB
	wm sync.Mutex
}

func NewService(db *sql.DB) *Service { return &Service{db: db} }

// nowUTC returns second-truncated UTC time; tests inject fixed times via
// explicit business dates.
func nowUTC() time.Time { return time.Now().UTC() }

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

type Account struct {
	ID                 string `json:"id"`
	LedgerID           string `json:"ledger_id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	HolderUserID       string `json:"holder_user_id,omitempty"`
	ParentID           string `json:"parent_id,omitempty"`  // 子账户：父账户（仅资产类、一级）
	SubKind            string `json:"sub_kind,omitempty"`   // 子账户：current/deposit/investment
	OpeningBalance     string `json:"opening_balance_cents"`
	OpeningDate        string `json:"opening_date,omitempty"`
	BalanceConfirmed   bool   `json:"balance_confirmed"`
	IncludeInFunds     bool   `json:"include_in_funds"`
	Archived           bool   `json:"archived"`
	Balance            string `json:"balance_cents"`
	BalanceUnconfirmed bool   `json:"balance_unconfirmed"`
	FaceValue          string `json:"face_value_cents,omitempty"` // 储值卡面额
	ExpiresOn          string `json:"expires_on,omitempty"`       // 储值卡到期日
}

func subjectCode(prefix, id string) string { return prefix + id }

// CreateAccount creates a funds account and its backing subject.
func (s *Service) CreateAccount(ledgerID, actorID, name, typ string, holderUserID *string, openingCents int64, openingDate *string, confirmed bool) (*Account, error) {
	if name == "" {
		return nil, errf(400, "invalid_input", "account name required")
	}
	valid := map[string]bool{"cash": true, "bank_card": true, "wechat_change": true, "alipay_balance": true, "stored_value": true, "credit_card": true, "huabei": true, "stock": true, "fund": true, "other_asset": true, "loan_liability": true}
	if !valid[typ] {
		return nil, errf(400, "invalid_input", "unknown account type %q", typ)
	}
	if openingCents > money.MaxCents || openingCents < -money.MaxCents {
		return nil, ErrInvalidAmount
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	kind := "asset"
	if liabilityTypes[typ] {
		kind = "liability"
	}
	acctID := ids.New()
	subID := ids.New()
	if _, err := tx.Exec(`INSERT INTO subjects(id,ledger_id,kind,code,name) VALUES(?,?,?,?,?)`,
		subID, ledgerID, kind, subjectCode(kind+":acct:", acctID), name); err != nil {
		return nil, err
	}
	var holder any
	if holderUserID != nil {
		holder = *holderUserID
	}
	var od any
	if openingDate != nil {
		od = *openingDate
	}
	if _, err := tx.Exec(`INSERT INTO accounts(id,ledger_id,subject_id,name,type,holder_user_id,opening_balance_cents,opening_date,balance_confirmed)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		acctID, ledgerID, subID, name, typ, holder, openingCents, od, boolToInt(confirmed)); err != nil {
		return nil, err
	}
	if err := audit(tx, ledgerID, actorID, "account.create", "account", acctID, map[string]any{"name": name, "type": typ}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAccount(ledgerID, acctID)
}

// CreateSubAccount 创建二类子账户（活期/定期/理财）。
// 约束：父账户必须存在、同账本、资产类、未归档、且自身不是子账户（仅一级）。
// 子账户独立建 subject，余额独立计算；父账户汇总在 listAccounts 完成。
func (s *Service) CreateSubAccount(ledgerID, actorID, parentID, name, subKind string, openingCents int64, openingDate *string, confirmed bool) (*Account, error) {
	if name == "" {
		return nil, errf(400, "invalid_input", "account name required")
	}
	if subKind != "current" && subKind != "deposit" && subKind != "investment" {
		return nil, errf(400, "invalid_input", "sub_kind must be current/deposit/investment")
	}
	if openingCents > money.MaxCents || openingCents < -money.MaxCents {
		return nil, ErrInvalidAmount
	}

	s.wm.Lock()
	defer s.wm.Unlock()

	// 校验父账户：同账本、资产类、未归档、非子账户
	var pKind, pType, pParent string
	var pArchived bool
	err := s.db.QueryRow(`SELECT s.kind, a.type, COALESCE(a.parent_id,''), a.archived_at IS NOT NULL
		FROM accounts a JOIN subjects s ON s.id=a.subject_id
		WHERE a.id=? AND a.ledger_id=?`, parentID, ledgerID).Scan(&pKind, &pType, &pParent, &pArchived)
	if err != nil {
		return nil, errf(400, "invalid_input", "parent account not found")
	}
	if pKind != "asset" {
		return nil, errf(400, "invalid_input", "sub-accounts only under asset accounts")
	}
	if pParent != "" {
		return nil, errf(400, "invalid_input", "nested sub-accounts not allowed")
	}
	if pArchived {
		return nil, errf(400, "invalid_input", "parent account archived")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	acctID := ids.New()
	subID := ids.New()
	// sub_kind 编码进 subject.name 前缀（accounts 表无该列，避免再加迁移）
	subName := map[string]string{"current": "[活期]", "deposit": "[定期]", "investment": "[理财]"}[subKind] + name
	if _, err := tx.Exec(`INSERT INTO subjects(id,ledger_id,kind,code,name) VALUES(?,?,?,?,?)`,
		subID, ledgerID, "asset", subjectCode("asset:acct:", acctID), subName); err != nil {
		return nil, err
	}
	var od any
	if openingDate != nil {
		od = *openingDate
	}
	if _, err := tx.Exec(`INSERT INTO accounts(id,ledger_id,subject_id,name,type,parent_id,opening_balance_cents,opening_date,balance_confirmed)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		acctID, ledgerID, subID, subName, pType, parentID, openingCents, od, boolToInt(confirmed)); err != nil {
		return nil, err
	}
	if err := audit(tx, ledgerID, actorID, "account.create_sub", "account", acctID,
		map[string]any{"name": name, "parent_id": parentID, "sub_kind": subKind}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAccount(ledgerID, acctID)
}

// ArchiveAccount marks an account archived. Accounts with transactions can
// only be archived, never deleted.
func (s *Service) ArchiveAccount(ledgerID, actorID, accountID string) error {
	s.wm.Lock()
	defer s.wm.Unlock()
	res, err := s.db.Exec(`UPDATE accounts SET archived_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND ledger_id=? AND archived_at IS NULL`, accountID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateAccount updates the display name and, only before any posting, the opening balance.
// Once entries exist, the financial record is immutable; callers must create an adjustment instead.
func (s *Service) UpdateAccount(ledgerID, actorID, accountID, name string, openingCents int64) error {
	if name == "" {
		return errf(400, "invalid_input", "account name required")
	}
	if openingCents > money.MaxCents || openingCents < -money.MaxCents {
		return ErrInvalidAmount
	}
	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var subjectID string
	if err := tx.QueryRow(`SELECT subject_id FROM accounts WHERE id=? AND ledger_id=? AND archived_at IS NULL`, accountID, ledgerID).Scan(&subjectID); err != nil {
		return ErrNotFound
	}
	var entries int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM entries WHERE account_id=?`, accountID).Scan(&entries); err != nil {
		return err
	}
	if entries > 0 {
		return errf(409, "account_has_transactions", "account has transactions; use a balance adjustment instead")
	}
	if _, err := tx.Exec(`UPDATE accounts SET name=?, opening_balance_cents=?, balance_confirmed=1 WHERE id=?`, name, openingCents, accountID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE subjects SET name=? WHERE id=?`, name, subjectID); err != nil {
		return err
	}
	if err := audit(tx, ledgerID, actorID, "account.update", "account", accountID, map[string]any{"name": name, "opening_balance_cents": openingCents}); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteAccount permanently removes an unused account. Accounts carrying any
// ledger entry or child account must remain in the immutable history and can only be archived.
func (s *Service) DeleteAccount(ledgerID, actorID, accountID string) error {
	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var subjectID string
	if err := tx.QueryRow(`SELECT subject_id FROM accounts WHERE id=? AND ledger_id=?`, accountID, ledgerID).Scan(&subjectID); err != nil {
		return ErrNotFound
	}
	var refs, children int
	if err := tx.QueryRow(`SELECT
		(SELECT COUNT(*) FROM entries WHERE account_id=?) +
		(SELECT COUNT(*) FROM transactions WHERE from_account_id=? OR to_account_id=?) +
		(SELECT COUNT(*) FROM templates WHERE default_account_id=?) +
		(SELECT COUNT(*) FROM recurrence_rules WHERE account_id=?) +
		(SELECT COUNT(*) FROM savings_goals WHERE account_id=?) +
		(SELECT COUNT(*) FROM amortization_plans WHERE account_id=?)`,
		accountID, accountID, accountID, accountID, accountID, accountID, accountID).Scan(&refs); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM accounts WHERE parent_id=?`, accountID).Scan(&children); err != nil {
		return err
	}
	if refs > 0 || children > 0 {
		return errf(409, "account_not_deletable", "account has transactions, dependent records or sub-accounts; archive it instead")
	}
	if err := audit(tx, ledgerID, actorID, "account.delete", "account", accountID, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM accounts WHERE id=?`, accountID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM subjects WHERE id=?`, subjectID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetStoredValueMeta 设置储值卡面额与到期日（仅 stored_value 类型）。
func (s *Service) SetStoredValueMeta(ledgerID, accountID string, faceValueCents int64, expiresOn string) error {
	if expiresOn != "" {
		if _, err := time.Parse("2006-01-02", expiresOn); err != nil {
			return errf(400, "invalid_input", "expires_on must be YYYY-MM-DD")
		}
	}
	res, err := s.db.Exec(`UPDATE accounts SET face_value_cents=?, expires_on=?
		WHERE id=? AND ledger_id=? AND type='stored_value'`,
		faceValueCents, nullIfEmpty(expiresOn), accountID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errf(400, "invalid_input", "not a stored-value account")
	}
	return nil
}

// ExpiringStoredValue 返回即将到期的储值卡（余额 > 0 且到期日在 horizon 天内）。
func (s *Service) ExpiringStoredValue(ledgerID string, withinDays int) ([]Account, error) {
	accts, err := s.listAccounts(ledgerID, "")
	if err != nil {
		return nil, err
	}
	now := time.Now()
	horizon := now.AddDate(0, 0, withinDays)
	out := []Account{}
	for _, a := range accts {
		if a.Type != "stored_value" || a.ExpiresOn == "" || a.Archived {
			continue
		}
		exp, err := time.Parse("2006-01-02", a.ExpiresOn)
		if err != nil || exp.After(horizon) || exp.Before(now.AddDate(0, 0, -1)) {
			continue
		}
		bal, _ := strconv.ParseInt(a.Balance, 10, 64)
		if bal > 0 {
			out = append(out, a)
		}
	}
	return out, nil
}

// GetAccount loads one account with its computed balance.
func (s *Service) GetAccount(ledgerID, accountID string) (*Account, error) {
	accts, err := s.listAccounts(ledgerID, accountID)
	if err != nil {
		return nil, err
	}
	if len(accts) == 0 {
		return nil, ErrNotFound
	}
	return &accts[0], nil
}

// ListAccounts returns all accounts (archived included, flagged) with
// balances derived from opening + complete entry sums.
func (s *Service) ListAccounts(ledgerID string) ([]Account, error) {
	return s.listAccounts(ledgerID, "")
}

func (s *Service) listAccounts(ledgerID, onlyID string) ([]Account, error) {
	q := `SELECT a.id,a.ledger_id,a.name,a.type,COALESCE(a.holder_user_id,''),a.opening_balance_cents,
		COALESCE(a.opening_date,''),a.balance_confirmed,a.include_in_funds,a.archived_at IS NOT NULL,s.kind,
		COALESCE(a.parent_id,''),COALESCE(a.face_value_cents,0),COALESCE(a.expires_on,''),
		COALESCE((
			SELECT SUM(CASE WHEN e.direction='debit' THEN e.amount_cents ELSE -e.amount_cents END)
			FROM entries e JOIN transactions t ON t.id=e.tx_id
			WHERE e.account_id=a.id AND t.status='posted'
		),0) AS net
		FROM accounts a JOIN subjects s ON s.id=a.subject_id
		WHERE a.ledger_id=?`
	args := []any{ledgerID}
	if onlyID != "" {
		q += ` AND a.id=?`
		args = append(args, onlyID)
	}
	q += ` ORDER BY a.created_at, a.name`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Account{}
	for rows.Next() {
		var a Account
		var opening int64
		var confirmed, include, archived int
		var kind string
		var net int64
		var faceValue int64
		var expiresOn string
		if err := rows.Scan(&a.ID, &a.LedgerID, &a.Name, &a.Type, &a.HolderUserID, &opening,
			&a.OpeningDate, &confirmed, &include, &archived, &kind, &a.ParentID, &faceValue, &expiresOn, &net); err != nil {
			return nil, err
		}
		if faceValue > 0 {
			a.FaceValue = fmt.Sprintf("%d", faceValue)
		}
		a.ExpiresOn = expiresOn
		a.OpeningBalance = fmt.Sprintf("%d", opening)
		a.BalanceConfirmed = confirmed == 1
		a.IncludeInFunds = include == 1
		a.Archived = archived == 1
		// 子账户：从 subject.name 前缀解析 sub_kind（活期/定期/理财）
		if a.ParentID != "" {
			if strings.HasPrefix(a.Name, "[活期]") {
				a.SubKind = "current"
				a.Name = strings.TrimPrefix(a.Name, "[活期]")
			} else if strings.HasPrefix(a.Name, "[定期]") {
				a.SubKind = "deposit"
				a.Name = strings.TrimPrefix(a.Name, "[定期]")
			} else if strings.HasPrefix(a.Name, "[理财]") {
				a.SubKind = "investment"
				a.Name = strings.TrimPrefix(a.Name, "[理财]")
			}
		}
		bal := opening + net
		if kind == "liability" {
			// Debt outstanding; negative means overpayment (e.g. credit
			// card 溢缴款) and is kept signed, never abs().
			bal = opening - net
		}
		a.Balance = fmt.Sprintf("%d", bal)
		a.BalanceUnconfirmed = !a.BalanceConfirmed
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 子账户汇总：父账户 Balance 累加各子账户余额（资产类、仅一级）。
	if onlyID == "" {
		byID := make(map[string]*Account, len(out))
		for i := range out {
			byID[out[i].ID] = &out[i]
		}
		// 先清零父账户自身余额再累加（父余额 = 自身 + 各子账户）
		// 注意：父账户自身余额保持不变，汇总值通过 BalanceWithSubs 给出？
		// 需求是父账户汇总展示——这里把子账户余额加进父账户 Balance，
		// 子账户自身余额不变，便于列表直接展示层级。
		for i := range out {
			if out[i].ParentID != "" {
				if p, ok := byID[out[i].ParentID]; ok {
					pBal, _ := strconv.ParseInt(p.Balance, 10, 64)
					cBal, _ := strconv.ParseInt(out[i].Balance, 10, 64)
					p.Balance = fmt.Sprintf("%d", pBal+cBal)
				}
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

type Category struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Icon     string `json:"icon,omitempty"`
	Sort     int    `json:"sort"`
	Seed     bool   `json:"is_seed"`
	Archived bool   `json:"archived"`
}

func (s *Service) ListCategories(ledgerID, kind string) ([]Category, error) {
	q := `SELECT id,COALESCE(parent_id,''),kind,name,COALESCE(icon,''),sort,is_seed,archived_at IS NOT NULL
		FROM categories WHERE ledger_id=?`
	args := []any{ledgerID}
	if kind != "" {
		q += ` AND kind=?`
		args = append(args, kind)
	}
	q += ` ORDER BY sort, name`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		var seed, archived int
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Kind, &c.Name, &c.Icon, &c.Sort, &seed, &archived); err != nil {
			return nil, err
		}
		c.Seed = seed == 1
		c.Archived = archived == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateCategory creates a custom category (max two levels) and its
// income/expense subject.
func (s *Service) CreateCategory(ledgerID, actorID string, parentID *string, kind, name string) (*Category, error) {
	if kind != "expense" && kind != "income" {
		return nil, errf(400, "invalid_input", "category kind must be expense or income")
	}
	if name == "" {
		return nil, errf(400, "invalid_input", "category name required")
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var parent any
	if parentID != nil && *parentID != "" {
		var pKind string
		var pParent sql.NullString
		err := tx.QueryRow(`SELECT kind,parent_id FROM categories WHERE id=? AND ledger_id=? AND archived_at IS NULL`, *parentID, ledgerID).Scan(&pKind, &pParent)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errf(400, "invalid_input", "parent category not found")
		}
		if err != nil {
			return nil, err
		}
		if pKind != kind {
			return nil, errf(400, "invalid_input", "parent kind mismatch")
		}
		if pParent.Valid {
			return nil, errf(400, "invalid_input", "categories support at most two levels")
		}
		parent = *parentID
	}

	cat := &Category{ID: ids.New(), Kind: kind, Name: name}
	if parentID != nil {
		cat.ParentID = *parentID
	}
	if err := insertCategoryTx(tx, ledgerID, cat, parent, 0); err != nil {
		return nil, err
	}
	if err := audit(tx, ledgerID, actorID, "category.create", "category", cat.ID, map[string]any{"name": name, "kind": kind}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return cat, nil
}

// insertCategoryTx inserts a category plus its subject inside a tx.
func insertCategoryTx(tx *sql.Tx, ledgerID string, c *Category, parent any, sort int) error {
	if _, err := tx.Exec(`INSERT INTO categories(id,ledger_id,parent_id,kind,name,sort,is_seed)
		VALUES(?,?,?,?,?,?,?)`, c.ID, ledgerID, parent, c.Kind, c.Name, sort, boolToInt(c.Seed)); err != nil {
		return err
	}
	kind := "expense"
	if c.Kind == "income" {
		kind = "income"
	}
	_, err := tx.Exec(`INSERT INTO subjects(id,ledger_id,kind,code,name) VALUES(?,?,?,?,?)`,
		ids.New(), ledgerID, kind, subjectCode(kind+":cat:", c.ID), c.Name)
	return err
}

// ---------------------------------------------------------------------------
// Posting transactions
// ---------------------------------------------------------------------------

// SplitInput 是拆分部分：一笔账单可拆成多个费用/应收（代付）部分。
type SplitInput struct {
	PartType     string // expense | receivable（income 单仅 expense→income）
	CategoryID   string
	Counterparty string // receivable 必填
	AmountCents  int64
}

// PostInput is a validated posting request. AmountCents is int64 cents;
// OperationID is the client idempotency key.
type PostInput struct {
	LedgerID      string
	ActorID       string
	Type          string // expense | income | transfer
	BusinessDate  string // YYYY-MM-DD or RFC3339
	DatePrecision string // datetime | day
	AmountCents   int64
	CategoryID    string // expense/income only（无拆分时）
	Splits        []SplitInput
	FromAccountID string // expense pays from; transfer source
	ToAccountID   string // income pays to; transfer target
	Note          string
	Merchant      string
	Channel       string
	OperationID   string
	// RecurrenceInstanceID 关联周期实例：手工/模板记账确认本期事项，
	// 同事务累计已关联金额，部分付款正确扣剩余计划。
	RecurrenceInstanceID string
	// SourceTxNo 导入来源单号（强去重，唯一索引）
	SourceTxNo string
}

// PostResult is the outcome of a posting (or its idempotent replay).
type PostResult struct {
	TxID     string `json:"tx_id"`
	Replayed bool   `json:"replayed"`
}

// contentHash is the stable digest of the business payload.
func (in *PostInput) contentHash() string {
	payload := map[string]any{
		"type": in.Type, "date": in.BusinessDate, "amount": in.AmountCents,
		"cat": in.CategoryID, "from": in.FromAccountID, "to": in.ToAccountID,
		"note": in.Note, "merchant": in.Merchant, "channel": in.Channel,
		"splits": in.Splits,
	}
	buf, _ := json.Marshal(payload)
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// Post validates and records a transaction with balanced entries.
func (s *Service) Post(in PostInput) (*PostResult, error) {
	if err := s.validate(&in); err != nil {
		return nil, err
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Idempotency: replay returns the original result; a same-key
	// different-content submission is a conflict, never an overwrite.
	var existingID, existingHash string
	err = tx.QueryRow(`SELECT id, content_hash FROM transactions WHERE ledger_id=? AND operation_id=?`,
		in.LedgerID, in.OperationID).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash == in.contentHash() {
			tx.Commit()
			return &PostResult{TxID: existingID, Replayed: true}, nil
		}
		return nil, errf(409, "idempotency_conflict", "operation_id already used with different content")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if err := s.postInTx(tx, in); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	var id string
	if err := s.db.QueryRow(`SELECT id FROM transactions WHERE ledger_id=? AND operation_id=?`,
		in.LedgerID, in.OperationID).Scan(&id); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

// postInTx posts inside an existing transaction (used by Post and by
// Revise for the replacement version). Caller holds the write mutex.
func (s *Service) postInTx(tx *sql.Tx, in PostInput) error {
	txID := ids.New()
	prec := in.DatePrecision
	if prec == "" {
		prec = "datetime"
	}
	var cat any
	if in.CategoryID != "" {
		cat = in.CategoryID
	}
	var fromAcct, toAcct any
	if in.FromAccountID != "" {
		fromAcct = in.FromAccountID
	}
	if in.ToAccountID != "" {
		toAcct = in.ToAccountID
	}
	var recInst any
	if in.RecurrenceInstanceID != "" {
		recInst = in.RecurrenceInstanceID
	}
	var srcNo any
	if in.SourceTxNo != "" {
		srcNo = in.SourceTxNo
	}
	if _, err := tx.Exec(`INSERT INTO transactions
		(id,ledger_id,type,status,business_date,date_precision,amount_cents,category_id,from_account_id,to_account_id,note,merchant,channel,recurrence_instance_id,source_tx_no,created_by,operation_id,content_hash)
		VALUES(?,?,?,'posted',?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		txID, in.LedgerID, in.Type, in.BusinessDate, prec, in.AmountCents, cat, fromAcct, toAcct,
		nullIfEmpty(in.Note), nullIfEmpty(in.Merchant), nullIfEmpty(in.Channel), recInst, srcNo, in.ActorID, in.OperationID, in.contentHash()); err != nil {
		return err
	}
	// 周期实例确认（同事务）：已入账或部分入账扣除对应金额
	if in.RecurrenceInstanceID != "" {
		if err := confirmInstanceTx(tx, in.RecurrenceInstanceID, in.AmountCents); err != nil {
			return err
		}
	}

	type entry struct {
		subjectCode string
		accountID   string
		direction   string
		amount      int64
	}
	var es []entry

	if len(in.Splits) > 0 {
		// 拆分单：逐部分产生分录并落 transaction_splits
		for _, sp := range in.Splits {
			kind := "expense"
			if in.Type == "income" {
				kind = "income"
			}
			var spCat any
			if sp.CategoryID != "" {
				spCat = sp.CategoryID
			}
			var cp any
			if sp.Counterparty != "" {
				cp = sp.Counterparty
			}
			splitID := ids.New()
			if _, err := tx.Exec(`INSERT INTO transaction_splits(id,tx_id,part_type,category_id,counterparty,amount_cents)
				VALUES(?,?,?,?,?,?)`, splitID, txID, sp.PartType, spCat, cp, sp.AmountCents); err != nil {
				return err
			}
			switch sp.PartType {
			case "receivable":
				es = append(es, entry{"subj:asset:receivable", "", "debit", sp.AmountCents})
			case "discount":
				// 支付优惠：冲减费用（应付 10 实付 9 → 费用 10 - 优惠 1）
				es = append(es, entry{kind + ":cat:" + sp.CategoryID, "", "credit", sp.AmountCents})
			default: // expense / income 部分
				es = append(es, entry{kind + ":cat:" + sp.CategoryID, "", "debit", sp.AmountCents})
			}
		}
		// 资金侧：总额
		if in.Type == "expense" {
			es = append(es, entry{"", in.FromAccountID, "credit", in.AmountCents})
		} else {
			es = append(es, entry{"", in.ToAccountID, "debit", in.AmountCents})
		}
	} else {
		switch in.Type {
		case "expense":
			es = []entry{
				{"expense:cat:" + in.CategoryID, "", "debit", in.AmountCents},
				{"", in.FromAccountID, "credit", in.AmountCents},
			}
		case "income":
			es = []entry{
				{"", in.ToAccountID, "debit", in.AmountCents},
				{"income:cat:" + in.CategoryID, "", "credit", in.AmountCents},
			}
		case "transfer":
			es = []entry{
				{"", in.ToAccountID, "debit", in.AmountCents},
				{"", in.FromAccountID, "credit", in.AmountCents},
			}
		}
	}

	var debitSum, creditSum int64
	for _, e := range es {
		var subID string
		var err error
		if e.accountID != "" {
			err = tx.QueryRow(`SELECT subject_id FROM accounts WHERE id=?`, e.accountID).Scan(&subID)
		} else if strings.HasPrefix(e.subjectCode, "subj:") {
			err = tx.QueryRow(`SELECT id FROM subjects WHERE ledger_id=? AND code=?`,
				in.LedgerID, strings.TrimPrefix(e.subjectCode, "subj:")).Scan(&subID)
		} else {
			err = tx.QueryRow(`SELECT id FROM subjects WHERE ledger_id=? AND code=?`, in.LedgerID, e.subjectCode).Scan(&subID)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return errf(400, "invalid_input", "subject not resolvable")
		}
		if err != nil {
			return err
		}
		var acct any
		if e.accountID != "" {
			acct = e.accountID
		}
		if _, err := tx.Exec(`INSERT INTO entries(id,tx_id,subject_id,account_id,direction,amount_cents)
			VALUES(?,?,?,?,?,?)`, ids.New(), txID, subID, acct, e.direction, e.amount); err != nil {
			return err
		}
		if e.direction == "debit" {
			debitSum, err = money.CheckedAdd(debitSum, e.amount)
		} else {
			creditSum, err = money.CheckedAdd(creditSum, e.amount)
		}
		if err != nil {
			return errf(400, "amount_overflow", "amount aggregation overflow")
		}
	}
	if debitSum != creditSum {
		// Defense in depth: construction above always balances.
		return errf(500, "unbalanced_entries", "internal error: unbalanced entries")
	}

	// 商户 → 分类学习：用户确认过账即记忆，纠正（更正后新分类）会更新
	// 后续建议；不自动改过去账单。
	if in.Merchant != "" {
		catForLearn := in.CategoryID
		if catForLearn == "" {
			for _, sp := range in.Splits {
				if sp.PartType == "expense" || sp.PartType == "income" {
					catForLearn = sp.CategoryID
					break
				}
			}
		}
		if catForLearn != "" {
			if _, err := tx.Exec(`INSERT INTO merchant_map(ledger_id,merchant,category_id,use_count,last_used_at)
				VALUES(?,?,?,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'))
				ON CONFLICT(ledger_id,merchant) DO UPDATE SET
					category_id=excluded.category_id, use_count=use_count+1, last_used_at=excluded.last_used_at`,
				in.LedgerID, in.Merchant, catForLearn); err != nil {
				return err
			}
		}
	}

	return audit(tx, in.LedgerID, in.ActorID, "transaction.post", "transaction", txID, map[string]any{
		"type": in.Type, "amount_cents": in.AmountCents, "business_date": in.BusinessDate,
	})
}

// validate checks business rules before opening a write transaction.
func (s *Service) validate(in *PostInput) error {
	if in.AmountCents <= 0 {
		return ErrInvalidAmount
	}
	if in.AmountCents > money.MaxCents {
		return errf(400, "amount_overflow", "amount exceeds maximum")
	}
	if in.OperationID == "" {
		return errf(400, "invalid_input", "operation_id required")
	}
	if _, err := time.Parse("2006-01-02", in.BusinessDate); err != nil {
		if _, err2 := time.Parse(time.RFC3339, in.BusinessDate); err2 != nil {
			return errf(400, "invalid_input", "business_date must be YYYY-MM-DD or RFC3339")
		}
	}
	switch in.Type {
	case "expense", "income":
		kind := in.Type
		if len(in.Splits) > 0 {
			var pos, disc int64
			for _, sp := range in.Splits {
				if sp.AmountCents <= 0 {
					return ErrInvalidAmount
				}
				var err error
				switch sp.PartType {
				case "expense", "income":
					if sp.PartType != kind {
						return errf(400, "invalid_input", "split part kind mismatch")
					}
					if err := s.requireCategory(in.LedgerID, sp.CategoryID, kind); err != nil {
						return err
					}
					pos, err = money.CheckedAdd(pos, sp.AmountCents)
				case "receivable":
					if kind != "expense" {
						return errf(400, "invalid_input", "receivable splits only on expenses")
					}
					if strings.TrimSpace(sp.Counterparty) == "" {
						return errf(400, "invalid_input", "receivable split requires counterparty")
					}
					pos, err = money.CheckedAdd(pos, sp.AmountCents)
				case "discount":
					if kind != "expense" {
						return errf(400, "invalid_input", "discount splits only on expenses")
					}
					if err := s.requireCategory(in.LedgerID, sp.CategoryID, kind); err != nil {
						return err
					}
					disc, err = money.CheckedAdd(disc, sp.AmountCents)
				default:
					return errf(400, "invalid_input", "unknown split part type %q", sp.PartType)
				}
				if err != nil {
					return errf(400, "amount_overflow", "split total overflow")
				}
			}
			// 拆分合计（费用 + 应收 - 优惠）必须等于实付总额
			net, err := money.CheckedAdd(pos, -disc)
			if err != nil || net != in.AmountCents {
				return errf(400, "invalid_input", "splits (parts - discount) must add up to the total amount")
			}
		} else if err := s.requireCategory(in.LedgerID, in.CategoryID, kind); err != nil {
			return err
		}
		if in.Type == "expense" {
			return s.requireAccount(in.LedgerID, in.FromAccountID)
		}
		return s.requireAccount(in.LedgerID, in.ToAccountID)
	case "transfer":
		if in.FromAccountID == "" || in.ToAccountID == "" {
			return errf(400, "invalid_input", "transfer requires from and to accounts")
		}
		if in.FromAccountID == in.ToAccountID {
			return errf(400, "invalid_input", "transfer accounts must differ")
		}
		if err := s.requireAccount(in.LedgerID, in.FromAccountID); err != nil {
			return err
		}
		return s.requireAccount(in.LedgerID, in.ToAccountID)
	default:
		return errf(400, "invalid_input", "unknown transaction type %q", in.Type)
	}
}

func (s *Service) requireAccount(ledgerID, accountID string) error {
	if accountID == "" {
		return errf(400, "invalid_input", "account required")
	}
	var archived sql.NullString
	err := s.db.QueryRow(`SELECT archived_at FROM accounts WHERE id=?`, accountID).Scan(&archived)
	if errors.Is(err, sql.ErrNoRows) {
		return errf(404, "account_not_found", "account not found")
	}
	if err != nil {
		return err
	}
	// Cross-ledger access is an explicit error, never silently applied.
	var owner string
	if err := s.db.QueryRow(`SELECT ledger_id FROM accounts WHERE id=?`, accountID).Scan(&owner); err != nil {
		return err
	}
	if owner != ledgerID {
		return ErrCrossLedger
	}
	if archived.Valid {
		return errf(409, "account_archived", "account is archived")
	}
	return nil
}

func (s *Service) requireCategory(ledgerID, categoryID, kind string) error {
	if categoryID == "" {
		return errf(400, "invalid_input", "category required")
	}
	var owner, k string
	var archived sql.NullString
	err := s.db.QueryRow(`SELECT ledger_id,kind,archived_at FROM categories WHERE id=?`, categoryID).Scan(&owner, &k, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return errf(404, "category_not_found", "category not found")
	}
	if err != nil {
		return err
	}
	if owner != ledgerID {
		return ErrCrossLedger
	}
	if k != kind {
		return errf(400, "invalid_input", "category kind mismatch")
	}
	if archived.Valid {
		return errf(409, "category_archived", "category is archived")
	}
	return nil
}

// resolveSubject finds the subject id either by code (category subjects)
// or by account (each account backs exactly one subject).
func resolveSubject(tx *sql.Tx, ledgerID, code, accountID string) (subID, kind string, err error) {
	if accountID != "" {
		err = tx.QueryRow(`SELECT a.subject_id, s.kind FROM accounts a JOIN subjects s ON s.id=a.subject_id
			WHERE a.id=? AND a.ledger_id=?`, accountID, ledgerID).Scan(&subID, &kind)
	} else {
		err = tx.QueryRow(`SELECT id, kind FROM subjects WHERE ledger_id=? AND code=?`, ledgerID, code).Scan(&subID, &kind)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errf(400, "invalid_input", "subject not resolvable")
	}
	return subID, kind, err
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

type TxView struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	BusinessDate  string `json:"business_date"`
	DatePrecision string `json:"date_precision"`
	Amount        string `json:"amount_cents"`
	CategoryID    string `json:"category_id,omitempty"`
	CategoryName  string `json:"category_name,omitempty"`
	FromAccountID string `json:"from_account_id,omitempty"`
	FromAccount   string `json:"from_account_name,omitempty"`
	ToAccountID   string `json:"to_account_id,omitempty"`
	ToAccount     string `json:"to_account_name,omitempty"`
	Note          string `json:"note,omitempty"`
	Merchant      string `json:"merchant,omitempty"`
	Channel       string `json:"channel,omitempty"`
	CreatedBy     string `json:"created_by"`
	CreatedAt     string `json:"created_at"`
}

// ListTransactions returns posted transactions newest first.
func (s *Service) ListTransactions(ledgerID string, limit int) ([]TxView, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT t.id,t.type,t.status,t.business_date,t.date_precision,t.amount_cents,
		COALESCE(t.category_id,''),COALESCE(c.name,''),
		COALESCE(t.from_account_id,''),COALESCE(fa.name,''),
		COALESCE(t.to_account_id,''),COALESCE(ta.name,''),
		COALESCE(t.note,''),COALESCE(t.merchant,''),COALESCE(t.channel,''),
		t.created_by,t.created_at
		FROM transactions t
		LEFT JOIN categories c ON c.id=t.category_id
		LEFT JOIN accounts fa ON fa.id=t.from_account_id
		LEFT JOIN accounts ta ON ta.id=t.to_account_id
		WHERE t.ledger_id=? AND t.status='posted'
		ORDER BY t.business_date DESC, t.created_at DESC
		LIMIT ?`, ledgerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TxView{}
	for rows.Next() {
		var v TxView
		var amount int64
		if err := rows.Scan(&v.ID, &v.Type, &v.Status, &v.BusinessDate, &v.DatePrecision, &amount,
			&v.CategoryID, &v.CategoryName, &v.FromAccountID, &v.FromAccount,
			&v.ToAccountID, &v.ToAccount, &v.Note, &v.Merchant, &v.Channel,
			&v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.Amount = fmt.Sprintf("%d", amount)
		out = append(out, v)
	}
	return out, rows.Err()
}

// Summary is the stage-1 home overview: real sums from posted rows only.
type Summary struct {
	IncomeCents  string `json:"income_cents"`
	ExpenseCents string `json:"expense_cents"`
	NetCents     string `json:"net_cents"`
	AsOf         string `json:"as_of"`
}

// PeriodSummary sums net income/expense for a [from,to) business-date
// window using the effective business projection: 净收入 = 有效收入 -
// 收入退回；净支出 = 有效费用 - 费用退款（含重分类影响）。
func (s *Service) PeriodSummary(ledgerID, from, to string) (*Summary, error) {
	ov, err := s.Overview(ledgerID, from, to)
	if err != nil {
		return nil, err
	}
	return &Summary{
		IncomeCents:  ov.NetIncomeCents,
		ExpenseCents: ov.NetExpenseCents,
		NetCents:     ov.BalanceCents,
		AsOf:         nowUTC().Format(time.RFC3339),
	}, nil
}

// DailySum is one calendar day's posted income/expense totals.
type DailySum struct {
	Date         string `json:"date"`
	IncomeCents  string `json:"income_cents"`
	ExpenseCents string `json:"expense_cents"`
}

// DailySums aggregates posted transactions per business day in [from,to).
// Dates are grouped by the ledger's business date (day precision prefix),
// which the UI renders in the ledger timezone.
func (s *Service) DailySums(ledgerID, from, to string) ([]DailySum, error) {
	rows, err := s.db.Query(`SELECT substr(t.business_date,1,10) AS d,
		COALESCE(SUM(CASE WHEN t.type='income' THEN t.amount_cents WHEN t.type='income_refund' THEN -t.amount_cents END),0),
		COALESCE(SUM(CASE WHEN t.type='expense' THEN t.amount_cents WHEN t.type='refund' THEN -t.amount_cents END),0)
		FROM transactions t WHERE t.ledger_id=? AND `+effectiveClause+`
		AND t.business_date>=? AND t.business_date<?
		GROUP BY d ORDER BY d`, ledgerID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DailySum{}
	for rows.Next() {
		var d DailySum
		var inc, exp int64
		if err := rows.Scan(&d.Date, &inc, &exp); err != nil {
			return nil, err
		}
		d.IncomeCents = fmt.Sprintf("%d", inc)
		d.ExpenseCents = fmt.Sprintf("%d", exp)
		out = append(out, d)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func audit(tx *sql.Tx, ledgerID, actorID, action, entityType, entityID string, detail any) error {
	buf, _ := json.Marshal(detail)
	_, err := tx.Exec(`INSERT INTO audit_log(id,ledger_id,actor_user_id,action,entity_type,entity_id,detail)
		VALUES(?,?,?,?,?,?,?)`, ids.New(), ledgerID, actorID, action, entityType, entityID, string(buf))
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
