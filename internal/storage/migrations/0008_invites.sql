-- 0008_invites.sql: 家庭邀请（单次使用、有失效期、可撤销，不依赖邮件）。

CREATE TABLE invites (
	id         TEXT PRIMARY KEY,
	ledger_id  TEXT NOT NULL REFERENCES ledgers(id),
	token_hash TEXT NOT NULL UNIQUE,          -- sha256(token)，令牌本体不落库
	role       TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('member')),
	expires_at TEXT NOT NULL,
	used_at    TEXT,
	revoked_at TEXT,
	created_by TEXT NOT NULL REFERENCES users(id),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
