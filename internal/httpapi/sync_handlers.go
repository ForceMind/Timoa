package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"xiaozhang/internal/ledger"
	"xiaozhang/internal/money"
)

// sync_handlers.go: 离线队列推送与变更拉取。
//
// 幂等：每个操作带 operation_id，服务端幂等是最终保障（关闭页面、
// 响应丢失、重复重试都不重复入账）。批量逐操作返回结果，每笔原子提交。
// 游标：服务端单调 rowid，不按客户端时间判定新旧。恢复世代变更时
// 客户端必须全量重同步，不能盲目重放旧队列。

type syncOp struct {
	OperationID string          `json:"operation_id"`
	Type        string          `json:"type"` // transaction
	Payload     json.RawMessage `json:"payload"`
}

type syncOpResult struct {
	OperationID string `json:"operation_id"`
	OK          bool   `json:"ok"`
	TxID        string `json:"tx_id,omitempty"`
	Replayed    bool   `json:"replayed,omitempty"`
	Code        string `json:"code,omitempty"`
	Message     string `json:"message,omitempty"`
}

func (s *server) syncPush(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	var body struct {
		Generation string   `json:"generation"`
		Ops        []syncOp `json:"ops"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if len(body.Ops) > 100 {
		writeErr(w, 400, "invalid_input", "at most 100 ops per batch")
		return
	}
	gen := s.restoreGeneration(m.ledgerID)
	if body.Generation != "" && gen != "" && body.Generation != gen {
		// 恢复世代不一致：客户端必须先全量重同步
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]string{"code": "generation_mismatch", "message": "server was restored; full re-sync required before pushing"},
			"generation": gen,
		})
		return
	}

	sess := sessionOf(r)
	results := make([]syncOpResult, 0, len(body.Ops))
	for _, op := range body.Ops {
		res := syncOpResult{OperationID: op.OperationID}
		if op.Type != "transaction" || op.OperationID == "" {
			res.Code = "invalid_input"
			res.Message = "unsupported op type or missing operation_id"
			results = append(results, res)
			continue
		}
		var p struct {
			Type          string `json:"type"`
			BusinessDate  string `json:"business_date"`
			DatePrecision string `json:"date_precision"`
			Amount        string `json:"amount"`
			CategoryID    string `json:"category_id"`
			FromAccountID string `json:"from_account_id"`
			ToAccountID   string `json:"to_account_id"`
			Note          string `json:"note"`
			Merchant      string `json:"merchant"`
			Channel       string `json:"channel"`
		}
		if err := json.Unmarshal(op.Payload, &p); err != nil {
			res.Code = "invalid_input"
			res.Message = "bad payload"
			results = append(results, res)
			continue
		}
		cents, err := money.ParseYuanRequired(p.Amount)
		if err != nil {
			res.Code = "invalid_amount"
			res.Message = err.Error()
			results = append(results, res)
			continue
		}
		out, err := s.ledger.Post(ledger.PostInput{
			LedgerID: m.ledgerID, ActorID: sess.UserID, Type: p.Type,
			BusinessDate: p.BusinessDate, DatePrecision: p.DatePrecision,
			AmountCents: cents, CategoryID: p.CategoryID,
			FromAccountID: p.FromAccountID, ToAccountID: p.ToAccountID,
			Note: p.Note, Merchant: p.Merchant, Channel: p.Channel,
			OperationID: op.OperationID,
		})
		if err != nil {
			if le, ok := err.(*ledger.Error); ok {
				res.Code = le.Code
				res.Message = le.Message
			} else {
				res.Code = "internal_error"
			}
			results = append(results, res)
			continue
		}
		res.OK = true
		res.TxID = out.TxID
		res.Replayed = out.Replayed
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"generation": gen, "results": results})
}

// syncPull 返回 cursor 之后的变更（交易按 rowid 单调游标）+ 账户与
// 分类快照 + 恢复世代。
func (s *server) syncPull(w http.ResponseWriter, r *http.Request) {
	m := s.mustMembership(w, r)
	if m == nil {
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	gen := s.restoreGeneration(m.ledgerID)

	txs, err := s.ledger.TransactionsSinceRowID(m.ledgerID, since, 500)
	if err != nil {
		writeError(w, err)
		return
	}
	cursor, err := s.ledger.MaxTxRowID(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	accts, err := s.ledger.ListAccounts(m.ledgerID)
	if err != nil {
		writeError(w, err)
		return
	}
	cats, err := s.ledger.ListCategories(m.ledgerID, "")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generation":   gen,
		"cursor":       cursor,
		"transactions": txs,
		"accounts":     accts,
		"categories":   cats,
	})
}

func (s *server) restoreGeneration(ledgerID string) string {
	var gen string
	_ = s.db.QueryRow(`SELECT COALESCE(
		(SELECT value FROM app_meta WHERE key='restore_generation'), '')`).Scan(&gen)
	return gen
}
