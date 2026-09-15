package ledger

import (
	"fmt"

	"xiaozhang/internal/ids"
)

// moneyops.go: 借入/借出/还本/收本、房贷车贷本息、理财赎回、押金。
// 全部复式分录，不混算费用或收入（T14/T15）。

// LendInput 借出/支付押金：形成应收（带对方），资产减少。
type LendInput struct {
	LedgerID, ActorID string
	BusinessDate      string
	FromAccountID     string
	AmountCents       int64
	Counterparty      string
	Note              string
	OperationID       string
}

// Lend 借出 400：借 资产:应收 400；贷 资产:银行 400。收本金走 Settle。
func (s *Service) Lend(in LendInput) (*PostResult, error) {
	if in.AmountCents <= 0 || in.Counterparty == "" {
		return nil, errf(400, "invalid_input", "amount and counterparty required")
	}
	if err := s.requireAccount(in.LedgerID, in.FromAccountID); err != nil {
		return nil, err
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	hash := fmt.Sprintf("lend|%s|%s|%d|%s", in.FromAccountID, in.Counterparty, in.AmountCents, in.BusinessDate)
	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, hash); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	id := ids.New()
	if err := insertLinkedTx(tx, linkedTx{
		id: id, ledgerID: in.LedgerID, typ: "lend", date: in.BusinessDate,
		amount: in.AmountCents, fromAccount: in.FromAccountID, counterparty: in.Counterparty,
		note: in.Note, actor: in.ActorID, operationID: in.OperationID, hash: hash,
	}); err != nil {
		return nil, err
	}
	if err := insertEntrySubj(tx, id, in.LedgerID, "asset:receivable", "", "debit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := insertEntry(tx, id, in.FromAccountID, "credit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.lend", "transaction", id,
		map[string]any{"amount_cents": in.AmountCents, "counterparty": in.Counterparty}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

// BorrowInput 借入：资产增加，应付负债增加（带对方）。
type BorrowInput struct {
	LedgerID, ActorID string
	BusinessDate      string
	ToAccountID       string
	AmountCents       int64
	Counterparty      string
	Note              string
	OperationID       string
}

// Borrow 借入 400：借 资产:银行 400；贷 负债:应付 400。不是收入。
func (s *Service) Borrow(in BorrowInput) (*PostResult, error) {
	if in.AmountCents <= 0 || in.Counterparty == "" {
		return nil, errf(400, "invalid_input", "amount and counterparty required")
	}
	if err := s.requireAccount(in.LedgerID, in.ToAccountID); err != nil {
		return nil, err
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	hash := fmt.Sprintf("borrow|%s|%s|%d|%s", in.ToAccountID, in.Counterparty, in.AmountCents, in.BusinessDate)
	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, hash); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	id := ids.New()
	if err := insertLinkedTx(tx, linkedTx{
		id: id, ledgerID: in.LedgerID, typ: "borrow", date: in.BusinessDate,
		amount: in.AmountCents, toAccount: in.ToAccountID, counterparty: in.Counterparty,
		note: in.Note, actor: in.ActorID, operationID: in.OperationID, hash: hash,
	}); err != nil {
		return nil, err
	}
	if err := insertEntry(tx, id, in.ToAccountID, "debit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := insertEntrySubj(tx, id, in.LedgerID, "liability:payable", "", "credit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.borrow", "transaction", id,
		map[string]any{"amount_cents": in.AmountCents, "counterparty": in.Counterparty}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

// RepayInput 还本金：结清对应应付，利息单独记费用。
type RepayInput struct {
	LedgerID, ActorID string
	OriginalID        string // 借入原单
	BusinessDate      string
	FromAccountID     string
	AmountCents       int64
	Note              string
	OperationID       string
}

// Repay 还本金：借 负债:应付；贷 资产:银行。不得超额。
func (s *Service) Repay(in RepayInput) (*PostResult, error) {
	if in.AmountCents <= 0 {
		return nil, ErrInvalidAmount
	}
	if err := s.requireAccount(in.LedgerID, in.FromAccountID); err != nil {
		return nil, err
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	hash := fmt.Sprintf("repay|%s|%s|%d|%s", in.OriginalID, in.FromAccountID, in.AmountCents, in.BusinessDate)
	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, hash); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	orig, err := loadEffectiveTx(tx, in.LedgerID, in.OriginalID, "borrow")
	if err != nil {
		return nil, err
	}
	var repaid int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transactions t
		WHERE t.link_id=? AND t.type='repay' AND `+effectiveClause, orig.ID).Scan(&repaid); err != nil {
		return nil, err
	}
	if repaid+in.AmountCents > orig.AmountCents {
		return nil, errf(409, "repay_cap_exceeded", "repay exceeds outstanding principal (%d)", orig.AmountCents-repaid)
	}

	id := ids.New()
	if err := insertLinkedTx(tx, linkedTx{
		id: id, ledgerID: in.LedgerID, typ: "repay", date: in.BusinessDate,
		amount: in.AmountCents, linkID: orig.ID, fromAccount: in.FromAccountID,
		note: in.Note, actor: in.ActorID, operationID: in.OperationID, hash: hash,
	}); err != nil {
		return nil, err
	}
	if err := insertEntrySubj(tx, id, in.LedgerID, "liability:payable", "", "debit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := insertEntry(tx, id, in.FromAccountID, "credit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.repay", "transaction", id,
		map[string]any{"original": orig.ID, "amount_cents": in.AmountCents}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

// LoanRepayInput 房贷/车贷还款：本金减负债 + 利息计费用，不整笔算消费。
type LoanRepayInput struct {
	LedgerID, ActorID string
	BusinessDate      string
	FromAccountID     string // 付款账户（资产）
	LoanAccountID     string // 贷款账户（负债）
	TotalCents        int64
	PrincipalCents    int64 // 本金部分；利息 = 总额 - 本金
	InterestCategory  string
	Note              string
	OperationID       string
}

func (s *Service) LoanRepay(in LoanRepayInput) (*PostResult, error) {
	if in.TotalCents <= 0 || in.PrincipalCents <= 0 || in.PrincipalCents >= in.TotalCents {
		return nil, errf(400, "invalid_input", "total and principal required (principal < total)")
	}
	if err := s.requireAccount(in.LedgerID, in.FromAccountID); err != nil {
		return nil, err
	}
	if err := s.requireAccount(in.LedgerID, in.LoanAccountID); err != nil {
		return nil, err
	}
	interest := in.TotalCents - in.PrincipalCents
	if err := s.requireCategory(in.LedgerID, in.InterestCategory, "expense"); err != nil {
		return nil, err
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	hash := fmt.Sprintf("loan_repay|%s|%s|%d|%d|%s", in.FromAccountID, in.LoanAccountID, in.TotalCents, in.PrincipalCents, in.BusinessDate)
	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, hash); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	id := ids.New()
	if _, err := tx.Exec(`INSERT INTO transactions
		(id,ledger_id,type,status,business_date,date_precision,amount_cents,from_account_id,to_account_id,category_id,principal_cents,note,created_by,operation_id,content_hash)
		VALUES(?,?,'loan_repay','posted',?,'datetime',?,?,?,?,?,?,?,?,?)`,
		id, in.LedgerID, in.BusinessDate, in.TotalCents, in.FromAccountID, in.LoanAccountID,
		in.InterestCategory, in.PrincipalCents, nullIfEmpty(in.Note), in.ActorID, in.OperationID, hash); err != nil {
		return nil, err
	}
	// 借 负债:贷款账户（本金）；借 费用:利息；贷 资产:付款账户（总额）
	if err := insertEntry(tx, id, in.LoanAccountID, "debit", in.PrincipalCents); err != nil {
		return nil, err
	}
	subj, err := categorySubject(tx, in.LedgerID, "expense", in.InterestCategory)
	if err != nil {
		return nil, err
	}
	if err := insertEntryID(tx, id, subj, "", "debit", interest); err != nil {
		return nil, err
	}
	if err := insertEntry(tx, id, in.FromAccountID, "credit", in.TotalCents); err != nil {
		return nil, err
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.loan_repay", "transaction", id,
		map[string]any{"total": in.TotalCents, "principal": in.PrincipalCents, "interest": interest}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

// RedeemInput 理财赎回：本金转回 + 已确认收益计收入。手工账面，不接行情。
type RedeemInput struct {
	LedgerID, ActorID string
	BusinessDate      string
	ToAccountID       string // 收款账户
	InvestAccountID   string // 理财账户（其他资产）
	TotalCents        int64
	PrincipalCents    int64 // 本金；收益 = 总额 - 本金（可 0）
	YieldCategory     string
	Note              string
	OperationID       string
}

func (s *Service) Redeem(in RedeemInput) (*PostResult, error) {
	if in.TotalCents <= 0 || in.PrincipalCents <= 0 || in.PrincipalCents > in.TotalCents {
		return nil, errf(400, "invalid_input", "total and principal required (principal <= total)")
	}
	if err := s.requireAccount(in.LedgerID, in.ToAccountID); err != nil {
		return nil, err
	}
	if err := s.requireAccount(in.LedgerID, in.InvestAccountID); err != nil {
		return nil, err
	}
	yieldAmt := in.TotalCents - in.PrincipalCents
	if yieldAmt > 0 {
		if err := s.requireCategory(in.LedgerID, in.YieldCategory, "income"); err != nil {
			return nil, err
		}
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	hash := fmt.Sprintf("redeem|%s|%s|%d|%d|%s", in.ToAccountID, in.InvestAccountID, in.TotalCents, in.PrincipalCents, in.BusinessDate)
	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, hash); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	id := ids.New()
	if _, err := tx.Exec(`INSERT INTO transactions
		(id,ledger_id,type,status,business_date,date_precision,amount_cents,from_account_id,to_account_id,category_id,principal_cents,note,created_by,operation_id,content_hash)
		VALUES(?,?,'redeem','posted',?,'datetime',?,?,?,?,?,?,?,?,?)`,
		id, in.LedgerID, in.BusinessDate, in.TotalCents, in.InvestAccountID, in.ToAccountID,
		in.YieldCategory, in.PrincipalCents, nullIfEmpty(in.Note), in.ActorID, in.OperationID, hash); err != nil {
		return nil, err
	}
	// 借 资产:收款账户（总额）；贷 资产:理财（本金）；贷 收入:收益
	if err := insertEntry(tx, id, in.ToAccountID, "debit", in.TotalCents); err != nil {
		return nil, err
	}
	if err := insertEntry(tx, id, in.InvestAccountID, "credit", in.PrincipalCents); err != nil {
		return nil, err
	}
	if yieldAmt > 0 {
		subj, err := categorySubject(tx, in.LedgerID, "income", in.YieldCategory)
		if err != nil {
			return nil, err
		}
		if err := insertEntryID(tx, id, subj, "", "credit", yieldAmt); err != nil {
			return nil, err
		}
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.redeem", "transaction", id,
		map[string]any{"total": in.TotalCents, "principal": in.PrincipalCents, "yield": yieldAmt}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

// CopyTransaction 复制账单另存新单：新业务 ID 与 operation_id（T-重复记一笔）。
func (s *Service) CopyTransaction(ledgerID, actorID, originalID, newOperationID string) (*PostResult, error) {
	var in PostInput
	var prec string
	err := s.db.QueryRow(`SELECT type,business_date,date_precision,amount_cents,COALESCE(category_id,''),
		COALESCE(from_account_id,''),COALESCE(to_account_id,''),COALESCE(note,''),COALESCE(merchant,''),COALESCE(channel,'')
		FROM transactions WHERE id=? AND ledger_id=? AND status='posted' AND type IN ('expense','income','transfer')`,
		originalID, ledgerID).
		Scan(&in.Type, &in.BusinessDate, &prec, &in.AmountCents, &in.CategoryID, &in.FromAccountID, &in.ToAccountID, &in.Note, &in.Merchant, &in.Channel)
	if err != nil {
		return nil, ErrNotFound
	}
	// 复制拆分
	rows, err := s.db.Query(`SELECT part_type,COALESCE(category_id,''),COALESCE(counterparty,''),amount_cents
		FROM transaction_splits WHERE tx_id=? ORDER BY rowid`, originalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sp SplitInput
		if err := rows.Scan(&sp.PartType, &sp.CategoryID, &sp.Counterparty, &sp.AmountCents); err != nil {
			return nil, err
		}
		in.Splits = append(in.Splits, sp)
	}
	in.LedgerID = ledgerID
	in.ActorID = actorID
	in.DatePrecision = prec
	in.OperationID = newOperationID
	return s.Post(in)
}
