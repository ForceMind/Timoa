package ledger

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"xiaozhang/internal/ids"
)

// adjust.go: 退款、收入退回、报销/代付回款（结算）、重分类、核销与
// 不可变修订链（冲正 + 替代）。所有额度校验在写事务内重新执行，
// 写互斥 + 数据库约束保证两个设备同时操作也不超额。

// effectiveClause 是「有效业务投影」的 SQL 片段（别名 t）：
// 已入账、未被冲正、且自身不是技术冲正。禁止“过滤原单却累计其冲正”
// 的混合算法——业务统计只读有效投影，余额读完整分录。
const effectiveClause = `t.status='posted' AND t.type <> 'reversal'
	AND NOT EXISTS (SELECT 1 FROM revisions rv WHERE rv.original_tx_id = t.id)`

// ---------------------------------------------------------------------------
// 退款（真实退款，支持全额/部分/多次/按拆分项/退至不同账户）
// ---------------------------------------------------------------------------

type RefundAlloc struct {
	SplitID     string // 空 = 整单（无拆分原单）
	AmountCents int64
}

type RefundInput struct {
	LedgerID, ActorID string
	OriginalID        string
	BusinessDate      string
	AccountID         string // 退至哪个资金账户（可为信用卡）
	Allocations       []RefundAlloc
	Note              string
	OperationID       string
}

func (s *Service) Refund(in RefundInput) (*PostResult, error) {
	if len(in.Allocations) == 0 || in.OperationID == "" {
		return nil, errf(400, "invalid_input", "allocations and operation_id required")
	}
	var total int64
	for _, a := range in.Allocations {
		if a.AmountCents <= 0 {
			return nil, ErrInvalidAmount
		}
		var err error
		total, err = add64(total, a.AmountCents)
		if err != nil {
			return nil, errf(400, "amount_overflow", "refund total overflow")
		}
	}
	if err := s.requireAccount(in.LedgerID, in.AccountID); err != nil {
		return nil, err
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, refundHash(in)); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	orig, err := loadEffectiveTx(tx, in.LedgerID, in.OriginalID, "expense")
	if err != nil {
		return nil, err
	}
	splits, err := loadSplits(tx, orig.ID)
	if err != nil {
		return nil, err
	}

	refundID := ids.New()
	type entryLine struct {
		subject, account, direction string
		amount                      int64
	}
	var lines []entryLine
	type pendingAlloc struct {
		splitID *string
		amount  int64
	}
	var allocs []pendingAlloc

	for _, a := range in.Allocations {
		if len(splits) == 0 {
			if a.SplitID != "" {
				return nil, errf(400, "invalid_input", "original has no splits")
			}
			// 整单退款：累计有效退款不得超过原金额（T02）
			prior, err := priorWholeRefunds(tx, orig.ID)
			if err != nil {
				return nil, err
			}
			if prior+a.AmountCents > orig.AmountCents {
				return nil, errf(409, "refund_cap_exceeded", "refund exceeds refundable amount (%d remaining)", orig.AmountCents-prior)
			}
			allocs = append(allocs, pendingAlloc{nil, a.AmountCents})
			subj, err := categorySubject(tx, in.LedgerID, "expense", orig.CategoryID)
			if err != nil {
				return nil, err
			}
			lines = append(lines, entryLine{subj, in.AccountID, "credit", a.AmountCents})
			continue
		}

		sp, ok := findSplit(splits, a.SplitID)
		if !ok {
			return nil, errf(400, "invalid_input", "split not found on original")
		}
		prior, err := priorSplitRefunds(tx, sp.ID)
		if err != nil {
			return nil, err
		}
		if prior+a.AmountCents > sp.AmountCents {
			return nil, errf(409, "refund_cap_exceeded", "refund exceeds split refundable amount (%d remaining)", sp.AmountCents-prior)
		}
		spID := sp.ID
		allocs = append(allocs, pendingAlloc{&spID, a.AmountCents})
		switch sp.PartType {
		case "expense":
			subj, err := categorySubject(tx, in.LedgerID, "expense", sp.CategoryID)
			if err != nil {
				return nil, err
			}
			lines = append(lines, entryLine{subj, in.AccountID, "credit", a.AmountCents})
		case "receivable":
			// 代付部分的商户退款：冲减应收，不重复退款（T09）
			lines = append(lines, entryLine{"subject:receivable", in.AccountID, "credit", a.AmountCents})
		default:
			return nil, errf(400, "invalid_input", "cannot refund split of part type %s", sp.PartType)
		}
	}

	if err := insertLinkedTx(tx, linkedTx{
		id: refundID, ledgerID: in.LedgerID, typ: "refund", date: in.BusinessDate,
		amount: total, linkID: orig.ID, toAccount: in.AccountID,
		note: in.Note, actor: in.ActorID, operationID: in.OperationID, hash: refundHash(in),
	}); err != nil {
		return nil, err
	}
	for _, pa := range allocs {
		if err := insertAlloc(tx, refundID, pa.splitID, pa.amount); err != nil {
			return nil, err
		}
	}
	// 现金流入
	if err := insertEntry(tx, refundID, in.AccountID, "debit", total); err != nil {
		return nil, err
	}
	for _, l := range lines {
		if l.subject == "subject:receivable" {
			if err := insertEntrySubj(tx, refundID, in.LedgerID, "asset:receivable", "", "credit", l.amount); err != nil {
				return nil, err
			}
		} else if err := insertEntryID(tx, refundID, l.subject, "", "credit", l.amount); err != nil {
			return nil, err
		}
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.refund", "transaction", refundID,
		map[string]any{"original": orig.ID, "amount_cents": total}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: refundID}, nil
}

// ---------------------------------------------------------------------------
// 收入退回（关联原收入，不伪装成购物支出）
// ---------------------------------------------------------------------------

type IncomeRefundInput struct {
	LedgerID, ActorID string
	OriginalID        string
	BusinessDate      string
	AccountID         string // 从哪个资金账户退回
	AmountCents       int64
	Note              string
	OperationID       string
}

func (s *Service) IncomeRefund(in IncomeRefundInput) (*PostResult, error) {
	if in.AmountCents <= 0 {
		return nil, ErrInvalidAmount
	}
	if err := s.requireAccount(in.LedgerID, in.AccountID); err != nil {
		return nil, err
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	hash := fmt.Sprintf("income_refund|%s|%s|%d|%s", in.OriginalID, in.AccountID, in.AmountCents, in.BusinessDate)
	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, hash); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	orig, err := loadEffectiveTx(tx, in.LedgerID, in.OriginalID, "income")
	if err != nil {
		return nil, err
	}
	var prior int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transactions t
		WHERE t.link_id=? AND t.type='income_refund' AND `+effectiveClause, orig.ID).Scan(&prior); err != nil {
		return nil, err
	}
	if prior+in.AmountCents > orig.AmountCents {
		return nil, errf(409, "refund_cap_exceeded", "income return exceeds returnable amount (%d remaining)", orig.AmountCents-prior)
	}

	id := ids.New()
	if err := insertLinkedTx(tx, linkedTx{
		id: id, ledgerID: in.LedgerID, typ: "income_refund", date: in.BusinessDate,
		amount: in.AmountCents, linkID: orig.ID, fromAccount: in.AccountID,
		note: in.Note, actor: in.ActorID, operationID: in.OperationID, hash: hash,
	}); err != nil {
		return nil, err
	}
	// 借 收入:分类（冲减收入）；贷 资产:账户（资金流出）
	subj, err := categorySubject(tx, in.LedgerID, "income", orig.CategoryID)
	if err != nil {
		return nil, err
	}
	if err := insertEntryID(tx, id, subj, "", "debit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := insertEntry(tx, id, in.AccountID, "credit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.income_refund", "transaction", id,
		map[string]any{"original": orig.ID, "amount_cents": in.AmountCents}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

// ---------------------------------------------------------------------------
// 结算（代付回款/报销到账）：冲减应收，不是收入
// ---------------------------------------------------------------------------

type SettlementInput struct {
	LedgerID, ActorID string
	OriginalID        string // 产生应收的原单（代付账单或重分类来源）
	BusinessDate      string
	AccountID         string
	AmountCents       int64
	Counterparty      string
	Note              string
	OperationID       string
}

func (s *Service) Settle(in SettlementInput) (*PostResult, error) {
	if in.AmountCents <= 0 {
		return nil, ErrInvalidAmount
	}
	if err := s.requireAccount(in.LedgerID, in.AccountID); err != nil {
		return nil, err
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	hash := fmt.Sprintf("settle|%s|%s|%d|%s", in.OriginalID, in.AccountID, in.AmountCents, in.BusinessDate)
	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, hash); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	out, err := receivableOutstanding(tx, in.LedgerID, in.OriginalID)
	if err != nil {
		return nil, err
	}
	if in.AmountCents > out {
		return nil, errf(409, "settlement_cap_exceeded", "settlement exceeds outstanding receivable (%d)", out)
	}

	id := ids.New()
	if err := insertLinkedTx(tx, linkedTx{
		id: id, ledgerID: in.LedgerID, typ: "settlement", date: in.BusinessDate,
		amount: in.AmountCents, linkID: in.OriginalID, toAccount: in.AccountID,
		counterparty: in.Counterparty, note: in.Note, actor: in.ActorID,
		operationID: in.OperationID, hash: hash,
	}); err != nil {
		return nil, err
	}
	if err := insertEntry(tx, id, in.AccountID, "debit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := insertEntrySubj(tx, id, in.LedgerID, "asset:receivable", "", "credit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.settle", "transaction", id,
		map[string]any{"original": in.OriginalID, "amount_cents": in.AmountCents}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

// ---------------------------------------------------------------------------
// 重分类（普通支出转待报销）与核销（确认无法收回转自担费用）
// ---------------------------------------------------------------------------

type ReclassInput struct {
	LedgerID, ActorID string
	OriginalID        string
	BusinessDate      string
	AmountCents       int64
	Counterparty      string
	OperationID       string
}

// Reclass 把费用的一部分/全部转为应收，不产生现金流，保留原单链与
// 有效日期（T08）。仅支持无拆分支出单（拆分单在创建时直接拆应收）。
func (s *Service) Reclass(in ReclassInput) (*PostResult, error) {
	if in.AmountCents <= 0 || in.Counterparty == "" {
		return nil, errf(400, "invalid_input", "amount and counterparty required")
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	hash := fmt.Sprintf("reclass|%s|%d|%s", in.OriginalID, in.AmountCents, in.Counterparty)
	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, hash); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	orig, err := loadEffectiveTx(tx, in.LedgerID, in.OriginalID, "expense")
	if err != nil {
		return nil, err
	}
	splits, err := loadSplits(tx, orig.ID)
	if err != nil {
		return nil, err
	}
	if len(splits) > 0 {
		return nil, errf(400, "invalid_input", "split bills carry receivable parts at creation; reclass is for plain expenses")
	}
	// 剩余可转 = 原金额 - 有效退款 - 已转应收
	refunded, err := priorWholeRefunds(tx, orig.ID)
	if err != nil {
		return nil, err
	}
	var reclassed int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transactions t
		WHERE t.link_id=? AND t.type='reclass' AND `+effectiveClause, orig.ID).Scan(&reclassed); err != nil {
		return nil, err
	}
	if refunded+reclassed+in.AmountCents > orig.AmountCents {
		return nil, errf(409, "reclass_cap_exceeded", "reclass exceeds remaining expense (%d)", orig.AmountCents-refunded-reclassed)
	}

	id := ids.New()
	if err := insertLinkedTx(tx, linkedTx{
		id: id, ledgerID: in.LedgerID, typ: "reclass", date: in.BusinessDate,
		amount: in.AmountCents, linkID: orig.ID, counterparty: in.Counterparty,
		actor: in.ActorID, operationID: in.OperationID, hash: hash,
	}); err != nil {
		return nil, err
	}
	subj, err := categorySubject(tx, in.LedgerID, "expense", orig.CategoryID)
	if err != nil {
		return nil, err
	}
	if err := insertEntrySubj(tx, id, in.LedgerID, "asset:receivable", "", "debit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := insertEntryID(tx, id, subj, "", "credit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.reclass", "transaction", id,
		map[string]any{"original": orig.ID, "amount_cents": in.AmountCents}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

type WriteoffInput struct {
	LedgerID, ActorID string
	OriginalID        string
	BusinessDate      string
	AmountCents       int64
	Reason            string
	OperationID       string
}

// Writeoff 核销应收：转为自担费用（需明确原因，不自动核销）。
func (s *Service) Writeoff(in WriteoffInput) (*PostResult, error) {
	if in.AmountCents <= 0 || strings.TrimSpace(in.Reason) == "" {
		return nil, errf(400, "invalid_input", "amount and reason required")
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	hash := fmt.Sprintf("writeoff|%s|%d|%s", in.OriginalID, in.AmountCents, in.Reason)
	if res, done, err := s.idempotentReplay(tx, in.LedgerID, in.OperationID, hash); err != nil || done {
		if done {
			tx.Commit()
		}
		return res, err
	}

	out, err := receivableOutstanding(tx, in.LedgerID, in.OriginalID)
	if err != nil {
		return nil, err
	}
	if in.AmountCents > out {
		return nil, errf(409, "writeoff_cap_exceeded", "writeoff exceeds outstanding receivable (%d)", out)
	}
	// 核销费用计入原单的支出分类（或拆分首项）
	catID, err := expenseCategoryOf(tx, in.LedgerID, in.OriginalID)
	if err != nil {
		return nil, err
	}

	id := ids.New()
	if err := insertLinkedTx(tx, linkedTx{
		id: id, ledgerID: in.LedgerID, typ: "writeoff", date: in.BusinessDate,
		amount: in.AmountCents, linkID: in.OriginalID, reason: in.Reason,
		actor: in.ActorID, operationID: in.OperationID, hash: hash,
	}); err != nil {
		return nil, err
	}
	subj, err := categorySubject(tx, in.LedgerID, "expense", catID)
	if err != nil {
		return nil, err
	}
	if err := insertEntryID(tx, id, subj, "", "debit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := insertEntrySubj(tx, id, in.LedgerID, "asset:receivable", "", "credit", in.AmountCents); err != nil {
		return nil, err
	}
	if err := audit(tx, in.LedgerID, in.ActorID, "transaction.writeoff", "transaction", id,
		map[string]any{"original": in.OriginalID, "amount_cents": in.AmountCents, "reason": in.Reason}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: id}, nil
}

// ---------------------------------------------------------------------------
// 修订链：错账更正与作废（不可变，冲正留痕）
// ---------------------------------------------------------------------------

// Revise 对原单做技术冲正；replacement 非空时同事务过账替代版本
// （更正），为空时为作废。依赖检查：
//   - 已有有效退款/结算的原单不能作废；
//   - 更正后金额不得小于已有效退款额。
// original 在 revisions 上有唯一约束，作废不能重复冲正。
func (s *Service) Revise(ledgerID, actorID, originalID, reason string, replacement *PostInput) (*PostResult, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, errf(400, "invalid_input", "reason required")
	}

	s.wm.Lock()
	defer s.wm.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	orig, err := loadEffectiveTxAny(tx, ledgerID, originalID)
	if err != nil {
		return nil, err
	}
	if orig.Type == "reversal" {
		return nil, errf(400, "invalid_input", "cannot revise a reversal")
	}

	// 依赖：有效退款 / 结算
	var refunds int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transactions t
		WHERE t.link_id=? AND t.type='refund' AND `+effectiveClause, orig.ID).Scan(&refunds); err != nil {
		return nil, err
	}
	var settlements int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transactions t
		WHERE t.link_id=? AND t.type IN ('settlement','writeoff','reclass') AND `+effectiveClause, orig.ID).Scan(&settlements); err != nil {
		return nil, err
	}
	if replacement == nil && (refunds > 0 || settlements > 0) {
		return nil, errf(409, "dependency_blocked",
			"cannot void: effective refunds (%d) or settlements (%d) exist; handle them first", refunds, settlements)
	}
	if replacement != nil && replacement.AmountCents < refunds {
		return nil, errf(409, "dependency_blocked",
			"corrected amount (%d) is below effective refunds (%d)", replacement.AmountCents, refunds)
	}

	// 1) 技术冲正：镜像原单全部分录并反向
	reversalID := ids.New()
	if err := insertLinkedTx(tx, linkedTx{
		id: reversalID, ledgerID: ledgerID, typ: "reversal", date: orig.BusinessDate,
		amount: orig.AmountCents, linkID: orig.ID, reason: reason,
		actor: actorID, operationID: ids.New(), hash: "reversal|" + orig.ID,
	}); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT subject_id,COALESCE(account_id,''),direction,amount_cents FROM entries WHERE tx_id=?`, orig.ID)
	if err != nil {
		return nil, err
	}
	type oe struct{ subj, acct, dir string; amt int64 }
	var oes []oe
	for rows.Next() {
		var e oe
		if err := rows.Scan(&e.subj, &e.acct, &e.dir, &e.amt); err != nil {
			rows.Close()
			return nil, err
		}
		oes = append(oes, e)
	}
	rows.Close()
	for _, e := range oes {
		flip := "debit"
		if e.dir == "debit" {
			flip = "credit"
		}
		var acct any
		if e.acct != "" {
			acct = e.acct
		}
		if _, err := tx.Exec(`INSERT INTO entries(id,tx_id,subject_id,account_id,direction,amount_cents)
			VALUES(?,?,?,?,?,?)`, ids.New(), reversalID, e.subj, acct, flip, e.amt); err != nil {
			return nil, err
		}
	}

	// 2) 替代版本（更正）
	var replacementID *string
	if replacement != nil {
		replacement.LedgerID = ledgerID
		replacement.ActorID = actorID
		if err := s.postInTx(tx, *replacement); err != nil {
			return nil, err
		}
		rid := ""
		if err := tx.QueryRow(`SELECT id FROM transactions WHERE ledger_id=? AND operation_id=?`,
			ledgerID, replacement.OperationID).Scan(&rid); err != nil {
			return nil, err
		}
		replacementID = &rid
	}

	if _, err := tx.Exec(`INSERT INTO revisions(id,ledger_id,original_tx_id,reversal_tx_id,replacement_tx_id,reason,created_by)
		VALUES(?,?,?,?,?,?,?)`, ids.New(), ledgerID, orig.ID, reversalID, replacementID, reason, actorID); err != nil {
		return nil, err
	}
	action := "transaction.void"
	if replacement != nil {
		action = "transaction.correct"
	}
	if err := audit(tx, ledgerID, actorID, action, "transaction", orig.ID,
		map[string]any{"reversal": reversalID, "reason": reason}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PostResult{TxID: reversalID}, nil
}

// RevisionOf 返回某交易的修订信息（用于详情展示修改历史）。
type Revision struct {
	OriginalID    string  `json:"original_tx_id"`
	ReversalID    string  `json:"reversal_tx_id"`
	ReplacementID *string `json:"replacement_tx_id,omitempty"`
	Reason        string  `json:"reason"`
	CreatedBy     string  `json:"created_by"`
	CreatedAt     string  `json:"created_at"`
}

func (s *Service) RevisionOf(ledgerID, txID string) (*Revision, error) {
	var r Revision
	err := s.db.QueryRow(`SELECT original_tx_id,reversal_tx_id,replacement_tx_id,reason,created_by,created_at
		FROM revisions WHERE ledger_id=? AND (original_tx_id=? OR replacement_tx_id=?)`,
		ledgerID, txID, txID).Scan(&r.OriginalID, &r.ReversalID, &r.ReplacementID, &r.Reason, &r.CreatedBy, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// ---------------------------------------------------------------------------
// 内部助手
// ---------------------------------------------------------------------------

type txRow struct {
	ID          string
	Type        string
	AmountCents int64
	CategoryID  string
	BusinessDate string
}

// loadEffectiveTx 加载有效投影中的指定类型交易。
func loadEffectiveTx(tx *sql.Tx, ledgerID, id, wantType string) (*txRow, error) {
	var r txRow
	var cat sql.NullString
	err := tx.QueryRow(`SELECT t.id,t.type,t.amount_cents,t.category_id,t.business_date FROM transactions t
		WHERE t.id=? AND t.ledger_id=? AND `+effectiveClause, id, ledgerID).
		Scan(&r.ID, &r.Type, &r.AmountCents, &cat, &r.BusinessDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(404, "original_not_found", "original transaction not found or already reversed")
	}
	if err != nil {
		return nil, err
	}
	if r.Type != wantType {
		return nil, errf(400, "invalid_input", "original must be %s, got %s", wantType, r.Type)
	}
	r.CategoryID = cat.String
	return &r, nil
}

func loadEffectiveTxAny(tx *sql.Tx, ledgerID, id string) (*txRow, error) {
	var r txRow
	var cat sql.NullString
	err := tx.QueryRow(`SELECT t.id,t.type,t.amount_cents,t.category_id,t.business_date FROM transactions t
		WHERE t.id=? AND t.ledger_id=? AND `+effectiveClause, id, ledgerID).
		Scan(&r.ID, &r.Type, &r.AmountCents, &cat, &r.BusinessDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(404, "original_not_found", "transaction not found or already reversed")
	}
	if err != nil {
		return nil, err
	}
	r.CategoryID = cat.String
	return &r, nil
}

type splitRow struct {
	ID           string
	PartType     string
	CategoryID   string
	Counterparty string
	AmountCents  int64
}

func loadSplits(tx *sql.Tx, txID string) ([]splitRow, error) {
	rows, err := tx.Query(`SELECT id,part_type,COALESCE(category_id,''),COALESCE(counterparty,''),amount_cents
		FROM transaction_splits WHERE tx_id=? ORDER BY rowid`, txID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []splitRow
	for rows.Next() {
		var sp splitRow
		if err := rows.Scan(&sp.ID, &sp.PartType, &sp.CategoryID, &sp.Counterparty, &sp.AmountCents); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func findSplit(splits []splitRow, id string) (splitRow, bool) {
	for _, sp := range splits {
		if sp.ID == id {
			return sp, true
		}
	}
	return splitRow{}, false
}

// priorWholeRefunds：无拆分原单的累计有效退款。
func priorWholeRefunds(tx *sql.Tx, originalID string) (int64, error) {
	var n int64
	err := tx.QueryRow(`SELECT COALESCE(SUM(ra.amount_cents),0) FROM refund_allocations ra
		JOIN transactions t ON t.id = ra.refund_tx_id
		WHERE t.link_id=? AND ra.split_id IS NULL AND `+effectiveClause, originalID).Scan(&n)
	return n, err
}

// priorSplitRefunds：某拆分项的累计有效退款。
func priorSplitRefunds(tx *sql.Tx, splitID string) (int64, error) {
	var n int64
	err := tx.QueryRow(`SELECT COALESCE(SUM(ra.amount_cents),0) FROM refund_allocations ra
		JOIN transactions t ON t.id = ra.refund_tx_id
		WHERE ra.split_id=? AND t.status='posted' AND t.type <> 'reversal'
		AND NOT EXISTS (SELECT 1 FROM revisions rv WHERE rv.original_tx_id = t.id)`, splitID).Scan(&n)
	return n, err
}

func insertAlloc(tx *sql.Tx, refundID string, splitID *string, amount int64) error {
	_, err := tx.Exec(`INSERT INTO refund_allocations(id,refund_tx_id,split_id,amount_cents) VALUES(?,?,?,?)`,
		ids.New(), refundID, splitID, amount)
	return err
}

// receivableOutstanding：某原单的应收余额 = 应收拆分 + 重分类 - 结算 - 核销 - 应收部分退款。
func receivableOutstanding(tx *sql.Tx, ledgerID, originalID string) (int64, error) {
	var created, settled, writtenOff, refunded int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transaction_splits
		WHERE tx_id=? AND part_type='receivable'`, originalID).Scan(&created); err != nil {
		return 0, err
	}
	var reclassed int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount_cents),0) FROM transactions t
		WHERE t.link_id=? AND t.type='reclass' AND `+effectiveClause, originalID).Scan(&reclassed); err != nil {
		return 0, err
	}
	created += reclassed
	if created == 0 {
		return 0, errf(404, "receivable_not_found", "no receivable on this transaction")
	}
	if err := tx.QueryRow(`SELECT COALESCE(SUM(CASE WHEN t.type='settlement' THEN t.amount_cents END),0),
		COALESCE(SUM(CASE WHEN t.type='writeoff' THEN t.amount_cents END),0)
		FROM transactions t WHERE t.link_id=? AND t.type IN ('settlement','writeoff') AND `+effectiveClause,
		originalID).Scan(&settled, &writtenOff); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(`SELECT COALESCE(SUM(ra.amount_cents),0) FROM refund_allocations ra
		JOIN transactions t ON t.id = ra.refund_tx_id
		JOIN transaction_splits sp ON sp.id = ra.split_id
		WHERE t.link_id=? AND sp.part_type='receivable' AND `+effectiveClause, originalID).Scan(&refunded); err != nil {
		return 0, err
	}
	out := created - settled - writtenOff - refunded
	if out < 0 {
		return 0, errf(500, "internal_error", "receivable went negative; refusing")
	}
	return out, nil
}

// expenseCategoryOf：原单的支出分类（拆分单取首个费用部分）。
func expenseCategoryOf(tx *sql.Tx, ledgerID, originalID string) (string, error) {
	var cat sql.NullString
	if err := tx.QueryRow(`SELECT category_id FROM transactions WHERE id=?`, originalID).Scan(&cat); err != nil {
		return "", err
	}
	if cat.Valid {
		return cat.String, nil
	}
	var c string
	if err := tx.QueryRow(`SELECT category_id FROM transaction_splits WHERE tx_id=? AND part_type='expense'
		ORDER BY rowid LIMIT 1`, originalID).Scan(&c); err != nil {
		return "", errf(400, "invalid_input", "no expense category on original")
	}
	return c, nil
}

// categorySubject 返回分类对应科目 id。
func categorySubject(tx *sql.Tx, ledgerID, kind, categoryID string) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM subjects WHERE ledger_id=? AND code=?`,
		ledgerID, kind+":cat:"+categoryID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(400, "invalid_input", "category subject missing")
	}
	return id, err
}

// insertEntry：账户侧分录（账户 → 科目）。
func insertEntry(tx *sql.Tx, txID, accountID, direction string, amount int64) error {
	var subID string
	if err := tx.QueryRow(`SELECT subject_id FROM accounts WHERE id=?`, accountID).Scan(&subID); err != nil {
		return err
	}
	return insertEntryID(tx, txID, subID, accountID, direction, amount)
}

func insertEntrySubj(tx *sql.Tx, txID, ledgerID, code, accountID, direction string, amount int64) error {
	var subID string
	if err := tx.QueryRow(`SELECT id FROM subjects WHERE ledger_id=? AND code=?`, ledgerID, code).Scan(&subID); err != nil {
		return err
	}
	return insertEntryID(tx, txID, subID, accountID, direction, amount)
}

func insertEntryID(tx *sql.Tx, txID, subjectID, accountID, direction string, amount int64) error {
	var acct any
	if accountID != "" {
		acct = accountID
	}
	_, err := tx.Exec(`INSERT INTO entries(id,tx_id,subject_id,account_id,direction,amount_cents)
		VALUES(?,?,?,?,?,?)`, ids.New(), txID, subjectID, acct, direction, amount)
	return err
}

type linkedTx struct {
	id, ledgerID, typ, date string
	amount                  int64
	linkID                  string
	fromAccount, toAccount  string
	counterparty            string
	reason                  string
	note                    string
	actor                   string
	operationID             string
	hash                    string
}

func insertLinkedTx(tx *sql.Tx, in linkedTx) error {
	var from, to, cp, reason, note any
	if in.fromAccount != "" {
		from = in.fromAccount
	}
	if in.toAccount != "" {
		to = in.toAccount
	}
	if in.counterparty != "" {
		cp = in.counterparty
	}
	if in.reason != "" {
		reason = in.reason
	}
	if in.note != "" {
		note = in.note
	}
	_, err := tx.Exec(`INSERT INTO transactions
		(id,ledger_id,type,status,business_date,date_precision,amount_cents,link_id,from_account_id,to_account_id,counterparty,reason,note,created_by,operation_id,content_hash)
		VALUES(?,?,?,'posted',?,'datetime',?,?,?,?,?,?,?,?,?,?)`,
		in.id, in.ledgerID, in.typ, in.date, in.amount, in.linkID, from, to, cp, reason, note,
		in.actor, in.operationID, in.hash)
	return err
}

// idempotentReplay：相同 operation_id + 相同 hash → 返回原结果；
// 不同内容 → 409。done=true 表示已处理（原样返回或冲突）。
func (s *Service) idempotentReplay(tx *sql.Tx, ledgerID, opID, hash string) (*PostResult, bool, error) {
	var existingID, existingHash string
	err := tx.QueryRow(`SELECT id, content_hash FROM transactions WHERE ledger_id=? AND operation_id=?`,
		ledgerID, opID).Scan(&existingID, &existingHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if existingHash == hash {
		return &PostResult{TxID: existingID, Replayed: true}, true, nil
	}
	return nil, true, errf(409, "idempotency_conflict", "operation_id already used with different content")
}

func refundHash(in RefundInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "refund|%s|%s|%s|", in.OriginalID, in.AccountID, in.BusinessDate)
	for _, a := range in.Allocations {
		fmt.Fprintf(&b, "%s:%d;", a.SplitID, a.AmountCents)
	}
	return b.String()
}

func add64(a, b int64) (int64, error) {
	s := a + b
	if b > 0 && s < a {
		return 0, errors.New("overflow")
	}
	return s, nil
}

var _ = fmt.Sprintf // keep fmt imported in all build paths
