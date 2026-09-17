-- 0015_account_types.sql: 放宽 accounts.type 的 CHECK 约束，新增 stock（股票）、fund（基金）。
-- SQLite 不能直接改 CHECK，按官方做法重建表：建新表→拷贝→删旧→改名→重建索引。

PRAGMA foreign_keys=off;

CREATE TABLE accounts_new (
	id                    TEXT PRIMARY KEY,
	ledger_id             TEXT NOT NULL REFERENCES ledgers(id),
	subject_id            TEXT NOT NULL REFERENCES subjects(id),
	name                  TEXT NOT NULL,
	type                  TEXT NOT NULL CHECK (type IN ('cash','bank_card','wechat_change','alipay_balance','stored_value','credit_card','huabei','stock','fund','other_asset','loan_liability')),
	holder_user_id        TEXT REFERENCES users(id),
	opening_balance_cents INTEGER NOT NULL DEFAULT 0,
	opening_date          TEXT,
	balance_confirmed     INTEGER NOT NULL DEFAULT 0,
	include_in_funds      INTEGER NOT NULL DEFAULT 1,
	archived_at           TEXT,
	created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	parent_id             TEXT REFERENCES accounts_new(id),
	face_value_cents      INTEGER,
	expires_on            TEXT
);

INSERT INTO accounts_new (id,ledger_id,subject_id,name,type,holder_user_id,opening_balance_cents,opening_date,balance_confirmed,include_in_funds,archived_at,created_at,parent_id,face_value_cents,expires_on)
SELECT id,ledger_id,subject_id,name,type,holder_user_id,opening_balance_cents,opening_date,balance_confirmed,include_in_funds,archived_at,created_at,parent_id,face_value_cents,expires_on FROM accounts;

DROP TABLE accounts;
ALTER TABLE accounts_new RENAME TO accounts;

CREATE INDEX IF NOT EXISTS idx_accounts_parent ON accounts(parent_id);

PRAGMA foreign_keys=on;
