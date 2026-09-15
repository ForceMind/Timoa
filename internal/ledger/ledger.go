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
	"sort"
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
	OpeningBalance     string `json:"opening_balance_cents"`
	OpeningDate        string `json:"opening_date,omitempty"`
	BalanceConfirmed   bool   `json:"balance_confirmed"`
	IncludeInFunds     bool   `json:"include_in_funds"`
	Archived           bool   `json:"archived"`
	Balance            string `json:"balance_cents"`
	BalanceUnconfirmed bool   `json:"balance_unconfirmed"`
}

func subjectCode(prefix, id string) string { return prefix + id }

// CreateAccount creates a funds account and its backing subject.
func (s *Service) CreateAccount(ledgerID, actorID, name, typ string, holderUserID *string, openingCents int64, openingDate *string, confirmed bool) (*Account, error) {
	if name == "" {
		return nil, errf(400, "invalid_input", "account name required")
	}
	valid := map[string]bool{"cash": true, "bank_card": true, "wechat_change": true, "alipay_balance": true, "stored_value": true, "credit_card": true, "huabei": true, "other_asset": true, "loan_liability": true}
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

	var out []Account
	for rows.Next() {
		var a Account
		var opening int64
		var confirmed, include, archived int
		var kind string
		var net int64
		if err := rows.Scan(&a.ID, &a.LedgerID, &a.Name, &a.Type, &a.HolderUserID, &opening,
			&a.OpeningDate, &confirmed, &include, &archived, &kind, &net); err != nil {
			return nil, err
		}
		a.OpeningBalance = fmt.Sprintf("%d", opening)
		a.BalanceConfirmed = confirmed == 1
		a.IncludeInFunds = include == 1
		a.Archived = archived == 1
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
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

type Category struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Sort     int    `json:"sort"`
	Seed     bool   `json:"is_seed"`
	Archived bool   `json:"archived"`
}

func (s *Service) ListCategories(ledgerID, kind string) ([]Category, error) {
	q := `SELECT id,COALESCE(parent_id,''),kind,name,sort,is_seed,archived_at IS NOT NULL
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
	var out []Category
	for rows.Next() {
		var c Category
		var seed, archived int
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Kind, &c.Name, &c.Sort, &seed, &archived); err != nil {
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

// PostInput is a validated posting request. AmountCents is int64 cents;
// OperationID is the client idempotency key.
type PostInput struct {
	LedgerID      string
	ActorID       string
	Type          string // expense | income | transfer
	BusinessDate  string // YYYY-MM-DD or RFC3339
	DatePrecision string // datetime | day
	AmountCents   int64
	CategoryID    string // expense/income only
	FromAccountID string // expense pays from; transfer source
	ToAccountID   string // income pays to; transfer target
	Note          string
	Merchant      string
	Channel       string
	OperationID   string
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
	}
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
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
	if _, err := tx.Exec(`INSERT INTO transactions
		(id,ledger_id,type,status,business_date,date_precision,amount_cents,category_id,from_account_id,to_account_id,note,merchant,channel,created_by,operation_id,content_hash)
		VALUES(?,?,?,'posted',?,?,?,?,?,?,?,?,?,?,?,?)`,
		txID, in.LedgerID, in.Type, in.BusinessDate, prec, in.AmountCents, cat, fromAcct, toAcct,
		nullIfEmpty(in.Note), nullIfEmpty(in.Merchant), nullIfEmpty(in.Channel), in.ActorID, in.OperationID, in.contentHash()); err != nil {
		return nil, err
	}

	type entry struct {
		subjectCode string
		accountID   string
		direction   string
	}
	var es []entry
	switch in.Type {
	case "expense":
		es = []entry{
			{"expense:cat:" + in.CategoryID, "", "debit"},
			{"", in.FromAccountID, "credit"},
		}
	case "income":
		es = []entry{
			{"", in.ToAccountID, "debit"},
			{"income:cat:" + in.CategoryID, "", "credit"},
		}
	case "transfer":
		es = []entry{
			{"", in.ToAccountID, "debit"},
			{"", in.FromAccountID, "credit"},
		}
	}

	var debitSum, creditSum int64
	for _, e := range es {
		subID, acctSubjectKind, err := resolveSubject(tx, in.LedgerID, e.subjectCode, e.accountID)
		if err != nil {
			return nil, err
		}
		_ = acctSubjectKind
		var acct any
		if e.accountID != "" {
			acct = e.accountID
		}
		if _, err := tx.Exec(`INSERT INTO entries(id,tx_id,subject_id,account_id,direction,amount_cents)
			VALUES(?,?,?,?,?,?)`, ids.New(), txID, subID, acct, e.direction, in.AmountCents); err != nil {
			return nil, err
		}
		if e.direction == "debit" {
			debitSum, err = money.CheckedAdd(debitSum, in.AmountCents)
		} else {
			creditSum, err = money.CheckedAdd(creditSum, in.AmountCents)
		}
		if err != nil {
			return nil, errf(400, "amount_overflow", "amount aggregation overflow")
		}
	}
	if debitSum != creditSum {
		// Defense in depth: construction above always balances.
		return nil, errf(500, "unbalanced_entries", "internal error: unbalanced entries")
	}

	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.post", "transaction", txID, map[string]any{
		"type": in.Type, "amount_cents": in.AmountCents, "business_date": in.BusinessDate,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: txID}, nil
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
	case "expense":
		if err := s.requireCategory(in.LedgerID, in.CategoryID, "expense"); err != nil {
			return err
		}
		return s.requireAccount(in.LedgerID, in.FromAccountID)
	case "income":
		if err := s.requireCategory(in.LedgerID, in.CategoryID, "income"); err != nil {
			return err
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

// PeriodSummary sums income/expense for a [from,to) business-date window.
func (s *Service) PeriodSummary(ledgerID, from, to string) (*Summary, error) {
	var income, expense int64
	err := s.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN type='income' THEN amount_cents END),0),
		COALESCE(SUM(CASE WHEN type='expense' THEN amount_cents END),0)
		FROM transactions WHERE ledger_id=? AND status='posted'
		AND business_date>=? AND business_date<?`, ledgerID, from, to).Scan(&income, &expense)
	if err != nil {
		return nil, err
	}
	net, err := money.CheckedAdd(income, -expense)
	if err != nil {
		return nil, errf(500, "amount_overflow", "summary overflow")
	}
	return &Summary{
		IncomeCents:  fmt.Sprintf("%d", income),
		ExpenseCents: fmt.Sprintf("%d", expense),
		NetCents:     fmt.Sprintf("%d", net),
		AsOf:         nowUTC().Format(time.RFC3339),
	}, nil
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
