package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"xiaozhang/internal/ids"
)

// attachments.go: 凭证附件。校验真实类型（魔数），随机存储名，
// 不放公开静态目录，经授权接口访问。

var allowedTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"application/pdf": ".pdf",
}

const (
	MaxAttachmentSize = 5 << 20 // 5 MB
	MaxPerTx          = 5
)

// SniffType 按魔数识别真实类型，不信任客户端声明。
func SniffType(head []byte) string {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "image/jpeg"
	case len(head) >= 8 && head[0] == 0x89 && head[1] == 'P' && head[2] == 'N' && head[3] == 'G':
		return "image/png"
	case len(head) >= 12 && string(head[0:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "image/webp"
	case len(head) >= 5 && string(head[0:5]) == "%PDF-":
		return "application/pdf"
	}
	return ""
}

type Attachment struct {
	ID          string `json:"id"`
	TxID        string `json:"tx_id"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"created_at"`
}

// SaveAttachment 校验并保存附件，返回元数据。失败上传不产生持久引用。
func (s *Service) SaveAttachment(dataDir, ledgerID, actorID, txID, fileName string, data []byte) (*Attachment, error) {
	if int64(len(data)) > MaxAttachmentSize {
		return nil, errf(400, "file_too_large", "attachment exceeds 5 MB")
	}
	ct := SniffType(data)
	if ct == "" {
		return nil, errf(400, "invalid_file_type", "only JPEG/PNG/WebP/PDF supported (verified by content)")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM attachments WHERE tx_id=? AND ledger_id=?`, txID, ledgerID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= MaxPerTx {
		return nil, errf(409, "too_many_attachments", "at most 5 attachments per transaction")
	}
	// 交易必须属于本账本
	var owner string
	if err := s.db.QueryRow(`SELECT ledger_id FROM transactions WHERE id=?`, txID).Scan(&owner); err != nil {
		return nil, ErrNotFound
	}
	if owner != ledgerID {
		return nil, ErrCrossLedger
	}

	sum := sha256.Sum256(data)
	stored := hex.EncodeToString(sum[:16]) + allowedTypes[ct]
	dir := filepath.Join(dataDir, "attachments")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, stored)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("store attachment: %w", err)
	}

	a := &Attachment{ID: ids.New(), TxID: txID, FileName: fileName, ContentType: ct, Size: int64(len(data))}
	if _, err := s.db.Exec(`INSERT INTO attachments(id,ledger_id,tx_id,file_name,stored_name,content_type,size,created_by)
		VALUES(?,?,?,?,?,?,?,?)`, a.ID, ledgerID, txID, fileName, stored, ct, a.Size, actorID); err != nil {
		os.Remove(path) // 失败上传不产生永久垃圾引用
		return nil, err
	}
	return a, nil
}

func (s *Service) ListAttachments(ledgerID, txID string) ([]Attachment, error) {
	rows, err := s.db.Query(`SELECT id,tx_id,file_name,content_type,size,created_at
		FROM attachments WHERE ledger_id=? AND tx_id=? ORDER BY created_at`, ledgerID, txID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.TxID, &a.FileName, &a.ContentType, &a.Size, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AttachmentPath 返回附件磁盘路径（仅授权调用方使用）。
func (s *Service) AttachmentPath(dataDir, ledgerID, attachmentID string) (path, contentType string, err error) {
	var stored string
	err = s.db.QueryRow(`SELECT stored_name, content_type FROM attachments WHERE id=? AND ledger_id=?`,
		attachmentID, ledgerID).Scan(&stored, &contentType)
	if err != nil {
		return "", "", ErrNotFound
	}
	// 防路径穿越：stored_name 由系统生成（hex+ext），此处再兜一层。
	path = filepath.Join(dataDir, "attachments", filepath.Base(stored))
	if _, err := os.Stat(path); err != nil {
		return "", "", ErrNotFound
	}
	return path, contentType, nil
}
