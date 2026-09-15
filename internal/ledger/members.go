package ledger

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"xiaozhang/internal/auth"
	"xiaozhang/internal/ids"
)

// members.go: 家庭协作。默认账本内成员共享可见；邀请令牌单次使用、
// 有失效期、可撤销，不要求邮件。撤销成员后服务端拒绝其新请求与同步。

const InviteTTL = 72 * time.Hour

type Invite struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
	Used      bool   `json:"used"`
	Revoked   bool   `json:"revoked"`
	CreatedAt string `json:"created_at"`
}

// CreateInvite 生成一次性邀请令牌（明文仅返回这一次）。
func (s *Service) CreateInvite(ledgerID, actorID string) (token string, inv *Invite, err error) {
	token = ids.Token(24)
	sum := sha256.Sum256([]byte(token))
	inv = &Invite{ID: ids.New(), ExpiresAt: time.Now().Add(InviteTTL).UTC().Format("2006-01-02T15:04:05.000Z")}
	_, err = s.db.Exec(`INSERT INTO invites(id,ledger_id,token_hash,role,expires_at,created_by)
		VALUES(?,?,?,'member',?,?)`, inv.ID, ledgerID, hex.EncodeToString(sum[:]), inv.ExpiresAt, actorID)
	if err != nil {
		return "", nil, err
	}
	if err := s.db.QueryRow(`SELECT created_at FROM invites WHERE id=?`, inv.ID).Scan(&inv.CreatedAt); err != nil {
		return "", nil, err
	}
	return token, inv, nil
}

func (s *Service) ListInvites(ledgerID string) ([]Invite, error) {
	rows, err := s.db.Query(`SELECT id,expires_at,used_at IS NOT NULL,revoked_at IS NOT NULL,created_at
		FROM invites WHERE ledger_id=? ORDER BY created_at DESC LIMIT 20`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var v Invite
		var used, revoked int
		if err := rows.Scan(&v.ID, &v.ExpiresAt, &used, &revoked, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.Used = used == 1
		v.Revoked = revoked == 1
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Service) RevokeInvite(ledgerID, inviteID string) error {
	res, err := s.db.Exec(`UPDATE invites SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND ledger_id=? AND used_at IS NULL AND revoked_at IS NULL`, inviteID, ledgerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AcceptInvite 凭有效令牌注册并加入账本：唯一允许创建账号的入口
// （公开注册关闭）。令牌单次使用、限期内有效。
func (s *Service) AcceptInvite(token, username, displayName, password string) (userID string, err error) {
	sum := sha256.Sum256([]byte(token))
	var inviteID, ledgerID, expires string
	var used, revoked sql.NullString
	err = s.db.QueryRow(`SELECT id,ledger_id,expires_at,used_at,revoked_at FROM invites WHERE token_hash=?`,
		hex.EncodeToString(sum[:])).Scan(&inviteID, &ledgerID, &expires, &used, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(403, "invalid_invite", "invite token invalid")
	}
	if err != nil {
		return "", err
	}
	if used.Valid || revoked.Valid {
		return "", errf(403, "invalid_invite", "invite token already used or revoked")
	}
	exp, err := time.Parse("2006-01-02T15:04:05.000Z", expires)
	if err != nil || time.Now().After(exp) {
		return "", errf(403, "invalid_invite", "invite token expired")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	userID = ids.New()
	if displayName == "" {
		displayName = username
	}
	if _, err := tx.Exec(`INSERT INTO users(id,username,display_name,password_hash) VALUES(?,?,?,?)`,
		userID, username, displayName, hash); err != nil {
		return "", errf(409, "username_taken", "username already exists")
	}
	if _, err := tx.Exec(`INSERT INTO ledger_members(ledger_id,user_id,role) VALUES(?,?,'member')`, ledgerID, userID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE invites SET used_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, inviteID); err != nil {
		return "", err
	}
	if err := audit(tx, ledgerID, userID, "member.join", "user", userID, map[string]any{"username": username}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return userID, nil
}

type Member struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Archived    bool   `json:"archived"`
	CreatedAt   string `json:"created_at"`
}

func (s *Service) ListMembers(ledgerID string) ([]Member, error) {
	rows, err := s.db.Query(`SELECT u.id,u.username,u.display_name,m.role,u.archived_at IS NOT NULL,m.created_at
		FROM ledger_members m JOIN users u ON u.id=m.user_id
		WHERE m.ledger_id=? ORDER BY m.created_at`, ledgerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var v Member
		var archived int
		if err := rows.Scan(&v.UserID, &v.Username, &v.DisplayName, &v.Role, &archived, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.Archived = archived == 1
		out = append(out, v)
	}
	return out, rows.Err()
}

// RevokeMember 撤销成员：归档账号、吊销全部会话、写审计。
// 服务端在其后的请求与同步中拒绝（登录与会话校验检查 archived_at）。
func (s *Service) RevokeMember(ledgerID, actorID, userID string) error {
	if actorID == userID {
		return errf(400, "invalid_input", "cannot revoke yourself")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE users SET archived_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND archived_at IS NULL`, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errf(404, "not_found", "member not found or already revoked")
	}
	if _, err := tx.Exec(`UPDATE sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id=? AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if err := audit(tx, ledgerID, actorID, "member.revoke", "user", userID, map[string]any{}); err != nil {
		return err
	}
	return tx.Commit()
}

// CanEditTx 普通成员只能更正自己创建的记录；管理员可全账更正。
func (s *Service) CanEditTx(ledgerID, userID, role, txID string) error {
	if role == "admin" {
		return nil
	}
	var creator string
	if err := s.db.QueryRow(`SELECT created_by FROM transactions WHERE id=? AND ledger_id=?`, txID, ledgerID).Scan(&creator); err != nil {
		return ErrNotFound
	}
	if creator != userID {
		return errf(403, "permission_denied", "members can only revise their own records")
	}
	return nil
}

var _ = fmt.Sprintf
