package ledger

// assets.go: 资产负债口径（spec 9.2.6）。资产展示明确「按已录入账户
// 和已记账记录计算」；收支结余不等于银行卡余额变化。

type AssetsOverview struct {
	AssetCents      string `json:"asset_cents"`       // 已记录资产（资产类账户余额合计）
	LiabilityCents  string `json:"liability_cents"`   // 已记录负债（负债类账户余额合计，溢缴为负不取绝对值）
	ReceivableCents string `json:"receivable_cents"`  // 应收（代付/借出/押金待收）
	PayableCents    string `json:"payable_cents"`     // 应付（借入待还）
	NetWorthCents   string `json:"net_worth_cents"`   // 已记录净资产 = 资产 + 应收 - 负债 - 应付
	Unconfirmed     int    `json:"unconfirmed_count"` // 期初未确认账户数
	Scope           string `json:"scope"`
}

func (s *Service) AssetsOverview(ledgerID string) (*AssetsOverview, error) {
	accts, err := s.ListAccounts(ledgerID)
	if err != nil {
		return nil, err
	}
	var assets, liabs int64
	unconfirmed := 0
	for _, a := range accts {
		if a.Archived {
			continue
		}
		bal, _ := parseI64(a.Balance)
		if liabilityTypes[a.Type] {
			liabs += bal
		} else {
			assets += bal
		}
		if a.BalanceUnconfirmed {
			unconfirmed++
		}
	}

	// 应收 = 应收科目余额（借-贷）；应付 = 应付科目余额（贷-借）
	subjectBal := func(code string, kind string) (int64, error) {
		var net int64
		err := s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN e.direction='debit' THEN e.amount_cents ELSE -e.amount_cents END),0)
			FROM entries e JOIN transactions t ON t.id=e.tx_id
			JOIN subjects su ON su.id=e.subject_id
			WHERE t.ledger_id=? AND su.code=? AND t.status='posted'`, ledgerID, code).Scan(&net)
		if err != nil {
			return 0, err
		}
		if kind == "liability" {
			net = -net
		}
		return net, nil
	}
	recv, err := subjectBal("asset:receivable", "asset")
	if err != nil {
		return nil, err
	}
	pay, err := subjectBal("liability:payable", "liability")
	if err != nil {
		return nil, err
	}

	net, err := money2Add(assets, recv)
	if err != nil {
		return nil, err
	}
	if net, err = money2Sub(net, liabs); err != nil {
		return nil, err
	}
	if net, err = money2Sub(net, pay); err != nil {
		return nil, err
	}
	return &AssetsOverview{
		AssetCents: i64s(assets), LiabilityCents: i64s(liabs),
		ReceivableCents: i64s(recv), PayableCents: i64s(pay),
		NetWorthCents: i64s(net), Unconfirmed: unconfirmed,
		Scope: "按已录入账户和已记账记录计算",
	}, nil
}
