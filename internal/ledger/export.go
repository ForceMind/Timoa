package ledger

// export.go: 流水导出行（口径与界面一致）。

var typeLabels = map[string]string{
	"expense": "支出", "income": "收入", "transfer": "转账",
	"refund": "退款", "income_refund": "收入退回", "settlement": "回款",
	"reclass": "重分类", "writeoff": "核销", "lend": "借出/押金",
	"borrow": "借入", "repay": "还本金", "loan_repay": "贷款还款", "redeem": "理财赎回",
}

// ExportRows 返回 [from,to) 有效业务的导出行（字符串已备好，防注入在
// 写出层处理）。
func (s *Service) ExportRows(ledgerID, from, to string) ([][]string, error) {
	rows, err := s.db.Query(`SELECT t.business_date, t.type, t.amount_cents,
		COALESCE(fa.name,''), COALESCE(ta.name,''), COALESCE(c.name,''),
		COALESCE(t.channel,''), COALESCE(t.merchant,''), COALESCE(t.note,''), COALESCE(t.counterparty,''),
		COALESCE(t.source_tx_no,''),
		COALESCE((SELECT SUM(r.amount_cents) FROM transactions r WHERE r.link_id=t.id AND r.type='refund' AND r.status='posted'
			AND NOT EXISTS(SELECT 1 FROM revisions rv WHERE rv.original_tx_id=r.id)),0),
		COALESCE((SELECT SUM(r.amount_cents) FROM transactions r WHERE r.link_id=t.id AND r.type='income_refund' AND r.status='posted'
			AND NOT EXISTS(SELECT 1 FROM revisions rv WHERE rv.original_tx_id=r.id)),0)
		FROM transactions t
		LEFT JOIN categories c ON c.id=t.category_id
		LEFT JOIN accounts fa ON fa.id=t.from_account_id
		LEFT JOIN accounts ta ON ta.id=t.to_account_id
		WHERE t.ledger_id=? AND `+effectiveClause+` AND t.type NOT IN ('reclass','writeoff','reversal')
		AND t.business_date>=? AND t.business_date<?
		ORDER BY t.business_date, t.created_at`, ledgerID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	yuan := func(c int64) string { return formatYuan(c) }
	var out [][]string
	for rows.Next() {
		var date, typ, fa, ta, cat, channel, merchant, note, cp, srcNo string
		var amount, refunded, incRefunded int64
		if err := rows.Scan(&date, &typ, &amount, &fa, &ta, &cat, &channel, &merchant, &note, &cp, &srcNo, &refunded, &incRefunded); err != nil {
			return nil, err
		}
		acct := fa
		if typ == "income" || typ == "settlement" {
			acct = ta
		} else if typ == "transfer" {
			acct = fa + " → " + ta
		}
		net := amount
		if typ == "expense" {
			net = amount - refunded
		} else if typ == "income" {
			net = amount - incRefunded
		}
		status := "有效"
		label, ok := typeLabels[typ]
		if !ok {
			label = typ
		}
		out = append(out, []string{
			date[:min(10, len(date))], label,
			yuan(amount), yuan(net), acct, cat, channel, merchant, note, cp, status, srcNo,
		})
	}
	return out, rows.Err()
}

func formatYuan(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	s := i64s(cents/100) + "." + twoDigits(cents%100)
	if neg {
		return "-" + s
	}
	return s
}

func twoDigits(n int64) string {
	if n < 10 {
		return "0" + i64s(n)
	}
	return i64s(n)
}
