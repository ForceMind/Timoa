package ledger

// sync.go: 变更拉取的单调游标查询（服务端 rowid，不用客户端时间）。

// TransactionsSinceRowID 返回 rowid > since 的交易（含冲正与关联类型，
// 客户端据此刷新本地缓存）。
func (s *Service) TransactionsSinceRowID(ledgerID string, since int64, limit int) ([]TxView, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
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
		WHERE t.ledger_id=? AND t.rowid>?
		ORDER BY t.rowid LIMIT ?`, ledgerID, since, limit)
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
		v.Amount = i64s(amount)
		out = append(out, v)
	}
	return out, rows.Err()
}

// MaxTxRowID 返回当前游标（账本内最新交易 rowid）。
func (s *Service) MaxTxRowID(ledgerID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(rowid),0) FROM transactions WHERE ledger_id=?`, ledgerID).Scan(&n)
	return n, err
}
