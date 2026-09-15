-- 0002_auth_ledger.sql: 认证、账本、资金账户、科目、业务交易与复式分录。
-- 金额一律整数分；正式交易不可变（更正走后续迁移的冲正链）。

CREATE TABLE users (
	id            TEXT PRIMARY KEY,
	username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
	display_name  TEXT NOT NULL,
	password_hash TEXT NOT NULL,
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	archived_at   TEXT
);

CREATE TABLE ledgers (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	currency   TEXT NOT NULL DEFAULT 'CNY',
	timezone   TEXT NOT NULL DEFAULT 'Asia/Shanghai',
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE ledger_members (
	ledger_id  TEXT NOT NULL REFERENCES ledgers(id),
	user_id    TEXT NOT NULL REFERENCES users(id),
	role       TEXT NOT NULL CHECK (role IN ('admin','member')),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	PRIMARY KEY (ledger_id, user_id)
);

CREATE TABLE sessions (
	id         TEXT PRIMARY KEY,              -- sha256(token) hex，令牌本体不落库
	user_id    TEXT NOT NULL REFERENCES users(id),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	expires_at TEXT NOT NULL,
	revoked_at TEXT,
	user_agent TEXT
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

-- 内部科目：复式分录的载体。资金账户、收支分类都映射到科目。
CREATE TABLE subjects (
	id        TEXT PRIMARY KEY,
	ledger_id TEXT NOT NULL REFERENCES ledgers(id),
	kind      TEXT NOT NULL CHECK (kind IN ('asset','liability','income','expense','equity')),
	code      TEXT NOT NULL,                  -- asset:acct:<id> / expense:cat:<id> / equity:opening …
	name      TEXT NOT NULL,
	UNIQUE (ledger_id, code)
);

CREATE TABLE accounts (
	id                    TEXT PRIMARY KEY,
	ledger_id             TEXT NOT NULL REFERENCES ledgers(id),
	subject_id            TEXT NOT NULL REFERENCES subjects(id),
	name                  TEXT NOT NULL,
	type                  TEXT NOT NULL CHECK (type IN ('cash','bank_card','wechat_change','alipay_balance','stored_value','credit_card','huabei','other_asset','loan_liability')),
	holder_user_id        TEXT REFERENCES users(id),
	opening_balance_cents INTEGER NOT NULL DEFAULT 0,
	opening_date          TEXT,               -- 期初基准日；未确认可空
	balance_confirmed     INTEGER NOT NULL DEFAULT 0,
	include_in_funds      INTEGER NOT NULL DEFAULT 1,
	archived_at           TEXT,
	created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_accounts_ledger ON accounts(ledger_id);

CREATE TABLE categories (
	id          TEXT PRIMARY KEY,
	ledger_id   TEXT NOT NULL REFERENCES ledgers(id),
	parent_id   TEXT REFERENCES categories(id),
	kind        TEXT NOT NULL CHECK (kind IN ('expense','income')),
	name        TEXT NOT NULL,
	sort        INTEGER NOT NULL DEFAULT 0,
	is_seed     INTEGER NOT NULL DEFAULT 0,
	archived_at TEXT
);
CREATE INDEX idx_categories_ledger ON categories(ledger_id, kind);

CREATE TABLE transactions (
	id              TEXT PRIMARY KEY,
	ledger_id       TEXT NOT NULL REFERENCES ledgers(id),
	type            TEXT NOT NULL CHECK (type IN ('expense','income','transfer')),
	status          TEXT NOT NULL DEFAULT 'posted' CHECK (status IN ('draft','posted','voided')),
	business_date   TEXT NOT NULL,           -- 业务有效日期（发生时间）
	date_precision  TEXT NOT NULL DEFAULT 'datetime' CHECK (date_precision IN ('datetime','day')),
	amount_cents    INTEGER NOT NULL CHECK (amount_cents > 0),
	category_id     TEXT REFERENCES categories(id),
	from_account_id TEXT REFERENCES accounts(id),
	to_account_id   TEXT REFERENCES accounts(id),
	note            TEXT,
	merchant        TEXT,
	channel         TEXT,
	created_by      TEXT NOT NULL REFERENCES users(id),
	operation_id    TEXT NOT NULL,           -- 客户端幂等键
	content_hash    TEXT NOT NULL,           -- 操作内容摘要
	created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')), -- 操作审计时间
	UNIQUE (ledger_id, operation_id)
);
CREATE INDEX idx_tx_ledger_date ON transactions(ledger_id, business_date);
CREATE INDEX idx_tx_ledger_type ON transactions(ledger_id, type);

CREATE TABLE entries (
	id           TEXT PRIMARY KEY,
	tx_id        TEXT NOT NULL REFERENCES transactions(id),
	subject_id   TEXT NOT NULL REFERENCES subjects(id),
	account_id   TEXT REFERENCES accounts(id),
	direction    TEXT NOT NULL CHECK (direction IN ('debit','credit')),
	amount_cents INTEGER NOT NULL CHECK (amount_cents > 0)
);
CREATE INDEX idx_entries_tx ON entries(tx_id);
CREATE INDEX idx_entries_account ON entries(account_id);
CREATE INDEX idx_entries_subject ON entries(subject_id);

CREATE TABLE audit_log (
	id             TEXT PRIMARY KEY,
	ledger_id      TEXT NOT NULL,
	actor_user_id  TEXT,
	action         TEXT NOT NULL,
	entity_type    TEXT,
	entity_id      TEXT,
	detail         TEXT,                     -- JSON；不含密码、令牌、凭证内容
	created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_audit_ledger ON audit_log(ledger_id, created_at);
