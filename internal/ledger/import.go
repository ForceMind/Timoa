package ledger

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xiaozhang/internal/ids"
	"xiaozhang/internal/impexp"
	"xiaozhang/internal/money"
)

// import.go: 导入批次。预览不改正式账；确认保留批次来源与逐行结果；
// 相同批次重复确认幂等；撤销检查依赖并留痕，不批量硬删除。

type ImportBatch struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Filename   string `json:"filename"`
	TotalRows  int    `json:"total_rows"`
	Importable int    `json:"importable"`
	Duplicates int    `json:"duplicates"`
	Skipped    int    `json:"skipped"`
	Failed     int    `json:"failed"`
	Imported   int    `json:"imported"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
}

// PreviewImport 解析并暂存批次（不写正式账），标注重复与跳过原因。
func (s *Service) PreviewImport(ledgerID, actorID, source, filename string, rows []impexp.RawRow) (*ImportBatch, []impexp.RawRow, error) {
	if len(rows) == 0 {
		return nil, nil, errf(400, "empty_import", "no rows parsed; check file format/source preset")
	}
	dup := 0
	skipped := 0
	failed := 0
	for i := range rows {
		r := &rows[i]
		if r.Err != "" {
			failed++
			continue
		}
		if r.Skip {
			skipped++
			continue
		}
		// 强去重：同来源单号
		if r.TxNo != "" {
			var n int
			if err := s.db.QueryRow(`SELECT COUNT(1) FROM transactions WHERE ledger_id=? AND source_tx_no=?`,
				ledgerID, r.TxNo).Scan(&n); err != nil {
				return nil, nil, err
			}
			if n > 0 {
				r.Skip = true
				r.SkipWhy = "重复（同来源单号已入账）"
				dup++
				continue
			}
		} else {
			// 疑似重复：同日同金额同方向（缺单号，不仅凭同金额删除，仅提示）
			var n int
			if err := s.db.QueryRow(`SELECT COUNT(1) FROM transactions t WHERE t.ledger_id=?
				AND substr(t.business_date,1,10)=? AND t.amount_cents=? AND t.type=? AND `+effectiveClause,
				ledgerID, r.Date, mustCents(r.Amount), r.Type).Scan(&n); err != nil {
				return nil, nil, err
			}
			if n > 0 {
				r.Skip = true
				r.SkipWhy = "疑似重复（同日同金额，已有记录）"
				dup++
				continue
			}
		}
	}

	buf, _ := json.Marshal(rows)
	b := &ImportBatch{ID: ids.New(), Source: source, Filename: filename,
		TotalRows: len(rows), Duplicates: dup, Skipped: skipped, Failed: failed, Status: "pending"}
	if _, err := s.db.Exec(`INSERT INTO import_batches(id,ledger_id,source,filename,total_rows,skipped,failed,rows_json,created_by)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		b.ID, ledgerID, source, filename, b.TotalRows, skipped+dup, failed, string(buf), actorID); err != nil {
		return nil, nil, err
	}
	b.Importable = b.TotalRows - skipped - dup - failed
	return b, rows, nil
}

func mustCents(yuan string) int64 {
	c, _ := money.ParseYuanRequired(yuan)
	return c
}

// ConfirmImport 正式入账：每行 operation_id = import:<batch>:<idx>（幂等
// 重试安全）；导入记录 date_precision=day，不伪造小时；分类按行内
// 名称匹配（通用 CSV）或批次默认分类。
func (s *Service) ConfirmImport(ledgerID, actorID, batchID, defaultCategoryID, accountID string) (*ImportBatch, error) {
	var status, rowsJSON, source string
	err := s.db.QueryRow(`SELECT status, rows_json, source FROM import_batches WHERE id=? AND ledger_id=?`,
		batchID, ledgerID).Scan(&status, &rowsJSON, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status == "confirmed" {
		// 幂等重试：直接返回批次结果
		return s.GetImportBatch(ledgerID, batchID)
	}
	if status != "pending" {
		return nil, errf(409, "import_not_pending", "batch already %s", status)
	}
	if err := s.requireAccount(ledgerID, accountID); err != nil {
		return nil, err
	}
	var rows []impexp.RawRow
	if err := json.Unmarshal([]byte(rowsJSON), &rows); err != nil {
		return nil, err
	}

	catByName := map[string]string{}
	if cats, err := s.ListCategories(ledgerID, ""); err == nil {
		for _, c := range cats {
			catByName[c.Kind+":"+c.Name] = c.ID
		}
	}

	imported := 0
	failed := 0
	for _, r := range rows {
		if r.Skip || r.Err != "" {
			continue
		}
		catID := defaultCategoryID
		if r.Category != "" {
			if id, ok := catByName[r.Type+":"+r.Category]; ok {
				catID = id
			}
		}
		if catID == "" {
			failed++
			continue
		}
		cents := mustCents(r.Amount)
		in := PostInput{
			LedgerID: ledgerID, ActorID: actorID, Type: r.Type,
			BusinessDate: r.Date, DatePrecision: "day", AmountCents: cents,
			CategoryID: catID, Note: r.Note, Merchant: r.Merchant, Channel: r.Channel,
			OperationID: fmt.Sprintf("import:%s:%d", batchID, r.Index),
			SourceTxNo:  r.TxNo,
		}
		if r.Type == "expense" {
			in.FromAccountID = accountID
		} else {
			in.ToAccountID = accountID
		}
		res, err := s.Post(in)
		if err != nil {
			failed++
			continue
		}
		_ = res
		imported++
	}
	if _, err := s.db.Exec(`UPDATE import_batches SET status='confirmed', imported=?, failed=failed+?, confirmed_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=?`, imported, failed, batchID); err != nil {
		return nil, err
	}
	return s.GetImportBatch(ledgerID, batchID)
}

// UndoImportBatch 撤销批次：逐单留痕冲正；有下游关联的单据列出并阻止，
// 不批量硬删除。
func (s *Service) UndoImportBatch(ledgerID, actorID, batchID string) (*ImportBatch, []string, error) {
	var status string
	if err := s.db.QueryRow(`SELECT status FROM import_batches WHERE id=? AND ledger_id=?`, batchID, ledgerID).Scan(&status); err != nil {
		return nil, nil, ErrNotFound
	}
	if status != "confirmed" {
		return nil, nil, errf(409, "import_not_confirmed", "batch not confirmed")
	}
	rows, err := s.db.Query(`SELECT id FROM transactions WHERE ledger_id=? AND operation_id LIKE ? AND status='posted'
		AND NOT EXISTS(SELECT 1 FROM revisions rv WHERE rv.original_tx_id=transactions.id)`,
		ledgerID, "import:"+batchID+":%")
	if err != nil {
		return nil, nil, err
	}
	var txIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, nil, err
		}
		txIDs = append(txIDs, id)
	}
	rows.Close()

	var blocked []string
	for _, id := range txIDs {
		if _, err := s.Revise(ledgerID, actorID, id, "导入批次撤销", nil); err != nil {
			var le *Error
			if errors.As(err, &le) && le.Code == "dependency_blocked" {
				blocked = append(blocked, id)
				continue
			}
			return nil, nil, err
		}
	}
	if len(blocked) == 0 {
		if _, err := s.db.Exec(`UPDATE import_batches SET status='undone', undone_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, batchID); err != nil {
			return nil, nil, err
		}
	}
	b, err := s.GetImportBatch(ledgerID, batchID)
	return b, blocked, err
}

func (s *Service) GetImportBatch(ledgerID, batchID string) (*ImportBatch, error) {
	var b ImportBatch
	err := s.db.QueryRow(`SELECT id,source,filename,total_rows,imported,skipped,failed,status,created_at
		FROM import_batches WHERE id=? AND ledger_id=?`, batchID, ledgerID).
		Scan(&b.ID, &b.Source, &b.Filename, &b.TotalRows, &b.Imported, &b.Skipped, &b.Failed, &b.Status, &b.CreatedAt)
	return &b, err
}

func (s *Service) ListImportBatches(ledgerID string) ([]ImportBatch, error) {
	rows, err := s.db.Query(`SELECT id,source,filename,total_rows,imported,skipped,failed,status,created_at
		FROM import_batches WHERE ledger_id=? ORDER BY created_at DESC LIMIT 50`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImportBatch{}
	for rows.Next() {
		var b ImportBatch
		if err := rows.Scan(&b.ID, &b.Source, &b.Filename, &b.TotalRows, &b.Imported, &b.Skipped, &b.Failed, &b.Status, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ImportRows 返回批次的逐行预览。
func (s *Service) ImportRows(ledgerID, batchID string) ([]impexp.RawRow, error) {
	var rowsJSON string
	if err := s.db.QueryRow(`SELECT rows_json FROM import_batches WHERE id=? AND ledger_id=?`, batchID, ledgerID).Scan(&rowsJSON); err != nil {
		return nil, err
	}
	var rows []impexp.RawRow
	return rows, json.Unmarshal([]byte(rowsJSON), &rows)
}

var _ = strings.TrimSpace
