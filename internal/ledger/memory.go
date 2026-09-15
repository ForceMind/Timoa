package ledger

import "fmt"

// memory.go: 推荐记忆——商户建议与「不再提醒」。

// DismissRecommendation 手动屏蔽后续推荐（habit:cat:<id> / rule:<id>）。
// 可随时联系恢复（删除屏蔽记录）；周期实例单次的「跳过」走 SkipInstance。
func (s *Service) DismissRecommendation(ledgerID, kind, key string) error {
	if kind != "habit" && kind != "rule" && kind != "template" {
		return errf(400, "invalid_input", "kind must be habit|rule|template")
	}
	k := key
	if kind == "habit" {
		k = "cat:" + key
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO recommendation_dismissals(ledger_id,kind,key) VALUES(?,?,?)`,
		ledgerID, kind, k)
	return err
}

// RestoreRecommendation 取消屏蔽。
func (s *Service) RestoreRecommendation(ledgerID, kind, key string) error {
	k := key
	if kind == "habit" {
		k = "cat:" + key
	}
	_, err := s.db.Exec(`DELETE FROM recommendation_dismissals WHERE ledger_id=? AND kind=? AND key=?`,
		ledgerID, kind, k)
	return err
}

// MerchantSuggestion 商户名 → 分类/金额建议（基于学习记录与历史）。
type MerchantSuggestion struct {
	Merchant   string `json:"merchant"`
	CategoryID string `json:"category_id"`
	Category   string `json:"category_name"`
	UseCount   int    `json:"use_count"`
	AmountHint string `json:"amount_hint_cents,omitempty"`
}

// SuggestMerchant 输入商户后调整候选；不覆盖用户已手动选择的内容
// （前端只在用户未手动选分类时预填）。
func (s *Service) SuggestMerchant(ledgerID, q string) (*MerchantSuggestion, error) {
	if q == "" {
		return nil, nil
	}
	var m MerchantSuggestion
	err := s.db.QueryRow(`SELECT mm.merchant, mm.category_id, c.name, mm.use_count
		FROM merchant_map mm JOIN categories c ON c.id=mm.category_id
		WHERE mm.ledger_id=? AND (mm.merchant=? OR mm.merchant LIKE ?)
		ORDER BY mm.merchant=? DESC, mm.use_count DESC, mm.last_used_at DESC LIMIT 1`,
		ledgerID, q, q+"%", q).
		Scan(&m.Merchant, &m.CategoryID, &m.Category, &m.UseCount)
	if err != nil {
		// 未学习过时，从有效历史的商户精确匹配找最近分类
		err = s.db.QueryRow(`SELECT t.merchant, t.category_id, c.name, 1
			FROM transactions t JOIN categories c ON c.id=t.category_id
			WHERE t.ledger_id=? AND t.merchant LIKE ? AND t.category_id IS NOT NULL AND `+effectiveClause+`
			ORDER BY t.created_at DESC LIMIT 1`, ledgerID, "%"+q+"%").
			Scan(&m.Merchant, &m.CategoryID, &m.Category, &m.UseCount)
		if err != nil {
			return nil, nil // 无建议
		}
	}
	// 该商户上次金额
	var amt int64
	if err := s.db.QueryRow(`SELECT amount_cents FROM transactions t
		WHERE t.ledger_id=? AND t.merchant=? AND t.type IN ('expense','income') AND `+effectiveClause+`
		ORDER BY t.created_at DESC LIMIT 1`, ledgerID, m.Merchant).Scan(&amt); err == nil {
		m.AmountHint = fmt.Sprintf("%d", amt)
	}
	return &m, nil
}

func yuanText(cents int64) string {
	return fmt.Sprintf("%.2f 元", float64(cents)/100)
}
