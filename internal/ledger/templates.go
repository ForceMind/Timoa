package ledger

import (
	"database/sql"

	"xiaozhang/internal/ids"
)

// templates.go: 模板查询与自定义模板管理。模板负责填单，与分类
// （统计）、周期规则（推荐）独立；模板修改不改变过去账单。

type Template struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Icon         string `json:"icon,omitempty"`
	TxType       string `json:"tx_type"`
	CategoryID   string `json:"category_id,omitempty"`
	CategoryName string `json:"category_name,omitempty"`
	AccountID    string `json:"default_account_id,omitempty"`
	AmountPolicy string `json:"amount_policy"`
	FixedCents   string `json:"fixed_amount_cents,omitempty"`
	Note         string `json:"note_template,omitempty"`
	Pinned       bool   `json:"pinned"`
	Sort         int    `json:"sort"`
	Enabled      bool   `json:"enabled"`
	Seed         bool   `json:"is_seed"`
}

// ListTemplates 返回账本模板（启用 + 停用，前端自行分组）。
func ListTemplates(db *sql.DB, ledgerID string) ([]Template, error) {
	rows, err := db.Query(`SELECT t.id,t.name,COALESCE(t.icon,''),t.tx_type,COALESCE(t.category_id,''),
		COALESCE(c.name,''),COALESCE(t.default_account_id,''),t.amount_policy,
		COALESCE(t.fixed_amount_cents,0),COALESCE(t.note_template,''),
		t.pinned,t.sort,t.enabled,t.is_seed
		FROM templates t LEFT JOIN categories c ON c.id=t.category_id
		WHERE t.ledger_id=? ORDER BY t.pinned DESC, t.sort, t.name`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		var tp Template
		var fixed int64
		var pinned, enabled, seed int
		if err := rows.Scan(&tp.ID, &tp.Name, &tp.Icon, &tp.TxType, &tp.CategoryID, &tp.CategoryName,
			&tp.AccountID, &tp.AmountPolicy, &fixed, &tp.Note, &pinned, &tp.Sort, &enabled, &seed); err != nil {
			return nil, err
		}
		if fixed > 0 {
			tp.FixedCents = i64s(fixed)
		}
		tp.Pinned = pinned == 1
		tp.Enabled = enabled == 1
		tp.Seed = seed == 1
		out = append(out, tp)
	}
	return out, rows.Err()
}

// SetTemplatePinned 置顶/取消置顶（首页常用模板固定排序，不被算法打乱）。
func (s *Service) SetTemplatePinned(ledgerID, templateID string, pinned bool) error {
	res, err := s.db.Exec(`UPDATE templates SET pinned=? WHERE id=? AND ledger_id=?`, boolToInt(pinned), templateID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTemplateEnabled 停用/启用模板（停用不影响历史账单）。
func (s *Service) SetTemplateEnabled(ledgerID, templateID string, enabled bool) error {
	res, err := s.db.Exec(`UPDATE templates SET enabled=? WHERE id=? AND ledger_id=?`, boolToInt(enabled), templateID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SearchTransactions 关键词搜索：备注/商户/对方/分类名（有效投影）。
func (s *Service) SearchTransactions(ledgerID, keyword string, limit int) ([]TxView, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	like := "%" + keyword + "%"
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
		WHERE t.ledger_id=? AND `+effectiveClause+`
		AND (t.note LIKE ? OR t.merchant LIKE ? OR t.counterparty LIKE ? OR c.name LIKE ?)
		ORDER BY t.business_date DESC LIMIT ?`, ledgerID, like, like, like, like, limit)
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

// ListTags 返回账本标签。
type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Seed bool   `json:"is_seed"`
}

func (s *Service) ListTags(ledgerID string) ([]Tag, error) {
	rows, err := s.db.Query(`SELECT id,name,is_seed FROM tags WHERE ledger_id=? AND archived_at IS NULL ORDER BY rowid`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var t Tag
		var seed int
		if err := rows.Scan(&t.ID, &t.Name, &seed); err != nil {
			return nil, err
		}
		t.Seed = seed == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateTag 创建自定义标签。
func (s *Service) CreateTag(ledgerID, name string) (*Tag, error) {
	if name == "" || len(name) > 20 {
		return nil, errf(400, "invalid_input", "tag name required (<=20 chars)")
	}
	t := &Tag{ID: ids.New(), Name: name}
	_, err := s.db.Exec(`INSERT INTO tags(id,ledger_id,name,is_seed) VALUES(?,?,?,0)`, t.ID, ledgerID, name)
	if err != nil {
		return nil, errf(409, "duplicate_tag", "tag already exists")
	}
	return t, nil
}
