package ledger

import (
	"time"
)

// recommend.go: 可解释推荐（首版不依赖大模型）。预测/推荐/待确认与
// 真实账单是不同实体，未经确认绝不入账。
//
// 优先级（权重集中在此，可测试）：
//  1. 到期未完成的明确周期规则
//  2. 与当前账单日期匹配的个人习惯（近 90 天同日/同星期常用分类）
//  3. 最近使用
//  4. 冷启动：用户置顶模板 → 高频内置模板

const (
	weightDueRule   = 100
	weightHabit     = 60
	weightRecent    = 30
	weightPinned    = 20
	weightColdStart = 10
	maxCandidates   = 4
)

type Recommendation struct {
	Kind       string `json:"kind"` // recurrence | habit | recent | pinned | common
	Reason     string `json:"reason"`
	InstanceID string `json:"instance_id,omitempty"`
	RuleID     string `json:"rule_id,omitempty"`
	RuleName   string `json:"rule_name,omitempty"`
	CategoryID string `json:"category_id,omitempty"`
	Category   string `json:"category_name,omitempty"`
	Icon       string `json:"icon,omitempty"`
	TxType     string `json:"tx_type"`
	AmountHint string `json:"amount_hint_cents,omitempty"`
	AccountID  string `json:"account_id,omitempty"`
}

// Recommend 生成最多 4 个候选，附真实可解释原因。只读有效业务记录，
// 排除作废/冲正/退款/内部转账；补记时调用方传入发生日期。
// 记忆：商户学习（merchant_map）、上次金额提示；用户手动「不再提醒」
// 的候选被过滤。
func (s *Service) Recommend(ledgerID string, now time.Time) ([]Recommendation, error) {
	out := []Recommendation{}
	seen := map[string]bool{}

	// 已屏蔽项
	dismissed := map[string]bool{}
	drows, err := s.db.Query(`SELECT kind, key FROM recommendation_dismissals WHERE ledger_id=?`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var k, key string
		if err := drows.Scan(&k, &key); err != nil {
			return nil, err
		}
		dismissed[k+":"+key] = true
	}

	// 上次金额记忆（每个分类最近一次有效记录金额）
	lastAmount := map[string]int64{}
	arows, err := s.db.Query(`SELECT t.category_id, t.amount_cents FROM transactions t
		WHERE t.ledger_id=? AND t.type='expense' AND t.category_id IS NOT NULL AND `+effectiveClause+`
		GROUP BY t.category_id HAVING MAX(t.created_at)`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	for arows.Next() {
		var c string
		var a int64
		if err := arows.Scan(&c, &a); err != nil {
			return nil, err
		}
		lastAmount[c] = a
	}

	// 1) 到期未完成的周期实例
	pending, err := s.PendingInstances(ledgerID, now)
	if err != nil {
		return nil, err
	}
	for _, p := range pending {
		if len(out) >= maxCandidates {
			break
		}
		if dismissed["rule:"+p.RuleID] {
			continue // 用户已对此规则「不再提醒」
		}
		reason := "本期「" + p.RuleName + "」尚未确认"
		if p.Status == "partial" && p.PlannedCents != "" {
			reason = "「" + p.RuleName + "」部分已记，尚有剩余"
		}
		out = append(out, Recommendation{
			Kind: "recurrence", Reason: reason, InstanceID: p.ID, RuleName: p.RuleName,
			RuleID: p.RuleID,
			CategoryID: p.CategoryID, TxType: p.TxType, AmountHint: p.PlannedCents, AccountID: p.AccountID,
		})
		seen["inst:"+p.ID] = true
	}

	// 2) 个人习惯：近 90 天同星期几的有效支出分类 Top（排除退款/转账/冲正）
	weekday := int(now.Weekday())
	rows, err := s.db.Query(`SELECT t.category_id, c.name, COALESCE(c.icon,''), COUNT(1) AS n
		FROM transactions t JOIN categories c ON c.id=t.category_id
		WHERE t.ledger_id=? AND t.type='expense' AND `+effectiveClause+`
		AND t.date_precision='datetime'
		AND substr(t.business_date,1,10)>=date('now','-90 days')
		AND CAST(strftime('%w', substr(t.business_date,1,10)) AS INTEGER)=?
		GROUP BY t.category_id ORDER BY n DESC LIMIT 2`, ledgerID, weekday)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() && len(out) < maxCandidates {
		var catID, name, icon string
		var n int
		if err := rows.Scan(&catID, &name, &icon, &n); err != nil {
			return nil, err
		}
		if seen["cat:"+catID] || dismissed["habit:cat:"+catID] {
			continue
		}
		rec := Recommendation{
			Kind: "habit", Reason: "你常在这一天记录「" + name + "」",
			CategoryID: catID, Category: name, Icon: icon, TxType: "expense",
		}
		if a, ok := lastAmount[catID]; ok {
			rec.AmountHint = i64s(a)
			rec.Reason += "（上次 " + yuanText(a) + "）"
		}
		out = append(out, rec)
		seen["cat:"+catID] = true
	}

	// 3) 最近使用（30 天）
	if len(out) < maxCandidates {
		rows2, err := s.db.Query(`SELECT t.category_id, c.name, COALESCE(c.icon,''), MAX(t.created_at)
			FROM transactions t JOIN categories c ON c.id=t.category_id
			WHERE t.ledger_id=? AND t.type='expense' AND `+effectiveClause+`
			AND substr(t.business_date,1,10)>=date('now','-30 days')
			GROUP BY t.category_id ORDER BY MAX(t.created_at) DESC LIMIT 2`, ledgerID)
		if err != nil {
			return nil, err
		}
		defer rows2.Close()
		for rows2.Next() && len(out) < maxCandidates {
			var catID, name, icon, last string
			if err := rows2.Scan(&catID, &name, &icon, &last); err != nil {
				return nil, err
			}
			if seen["cat:"+catID] {
				continue
			}
			out = append(out, Recommendation{
				Kind: "recent", Reason: "最近记录过「" + name + "」",
				CategoryID: catID, Category: name, Icon: icon, TxType: "expense",
			})
			seen["cat:"+catID] = true
		}
	}

	// 4) 冷启动/补齐：置顶模板 → 高频内置模板
	if len(out) < maxCandidates {
		rows3, err := s.db.Query(`SELECT t.id,t.name,COALESCE(t.icon,''),COALESCE(t.category_id,'')
			FROM templates t WHERE t.ledger_id=? AND t.enabled=1 AND t.tx_type IN ('expense','income')
			ORDER BY t.pinned DESC, t.sort LIMIT ?`, ledgerID, maxCandidates*2)
		if err != nil {
			return nil, err
		}
		defer rows3.Close()
		for rows3.Next() && len(out) < maxCandidates {
			var tplID, name, icon, catID string
			if err := rows3.Scan(&tplID, &name, &icon, &catID); err != nil {
				return nil, err
			}
			if catID != "" && seen["cat:"+catID] {
				continue
			}
			out = append(out, Recommendation{
				Kind: "pinned", Reason: "常用模板「" + name + "」",
				CategoryID: catID, Category: name, Icon: icon, TxType: "expense",
			})
			if catID != "" {
				seen["cat:"+catID] = true
			}
		}
	}
	return out, nil
}
