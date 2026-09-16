package ledger

import "xiaozhang/internal/ids"

// notes.go: 便笺——账本内非账务内容（购物清单、提醒事项等）。
// 不生成任何分录，不影响余额与统计；属于账本共享数据，
// 权限照旧走服务端成员校验（handler 层 mustMembership）。

type Note struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Pinned    bool   `json:"pinned"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListNotes 列出账本便笺（置顶在前，其后按更新时间倒序）。
func (s *Service) ListNotes(ledgerID string) ([]Note, error) {
	rows, err := s.db.Query(`SELECT id,content,pinned,created_by,created_at,updated_at
		FROM notes WHERE ledger_id=? ORDER BY pinned DESC, updated_at DESC`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		var pinned int
		if err := rows.Scan(&n.ID, &n.Content, &pinned, &n.CreatedBy, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Pinned = pinned != 0
		out = append(out, n)
	}
	return out, rows.Err()
}

// CreateNote 新增便笺（内容非空，最长 2000 字防滥用）。
func (s *Service) CreateNote(ledgerID, userID, content string) (*Note, error) {
	if content == "" || len([]rune(content)) > 2000 {
		return nil, errf(400, "invalid_input", "content required, at most 2000 chars")
	}
	n := &Note{ID: ids.New(), Content: content, CreatedBy: userID}
	err := s.db.QueryRow(`INSERT INTO notes (id,ledger_id,content,created_by) VALUES (?,?,?,?)
		RETURNING created_at, updated_at`, n.ID, ledgerID, content, userID).
		Scan(&n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// UpdateNote 改内容/置顶（账本内成员均可维护共享便笺）。
func (s *Service) UpdateNote(ledgerID, noteID, content string, pinned bool) error {
	if content == "" || len([]rune(content)) > 2000 {
		return errf(400, "invalid_input", "content required, at most 2000 chars")
	}
	res, err := s.db.Exec(`UPDATE notes SET content=?, pinned=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND ledger_id=?`, content, boolToInt(pinned), noteID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteNote 删除便笺。
func (s *Service) DeleteNote(ledgerID, noteID string) error {
	res, err := s.db.Exec(`DELETE FROM notes WHERE id=? AND ledger_id=?`, noteID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
