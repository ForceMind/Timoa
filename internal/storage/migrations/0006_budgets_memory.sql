-- 0006_budgets_memory.sql: 预算、推荐记忆（商户→分类学习、推荐屏蔽）。

-- 预算：总预算（category_id 空）与分类预算，按月。父分类预算覆盖子分类。
CREATE TABLE budgets (
	id          TEXT PRIMARY KEY,
	ledger_id   TEXT NOT NULL REFERENCES ledgers(id),
	month       TEXT NOT NULL,               -- YYYY-MM
	category_id TEXT REFERENCES categories(id),
	amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
	created_by  TEXT,
	created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	UNIQUE (ledger_id, month, category_id)
);
-- SQLite 唯一约束中 NULL 不冲突：总预算用空串哨兵
CREATE UNIQUE INDEX idx_budgets_total ON budgets(ledger_id, month) WHERE category_id IS NULL;

-- 商户 → 分类学习：用户确认过账即学习；用户纠正后更新后续建议，
-- 不自动改过去账单。
CREATE TABLE merchant_map (
	ledger_id   TEXT NOT NULL,
	merchant    TEXT NOT NULL,
	category_id TEXT NOT NULL REFERENCES categories(id),
	use_count   INTEGER NOT NULL DEFAULT 1,
	last_used_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	PRIMARY KEY (ledger_id, merchant)
);

-- 推荐屏蔽：用户手动「不再提醒」。key: habit:cat:<id> / rule:<id> / template:<id>
CREATE TABLE recommendation_dismissals (
	ledger_id  TEXT NOT NULL,
	kind       TEXT NOT NULL,
	key        TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	PRIMARY KEY (ledger_id, kind, key)
);

-- 便笺表已在 0004 创建；储蓄目标（存款制度）
CREATE TABLE savings_goals (
	id          TEXT PRIMARY KEY,
	ledger_id   TEXT NOT NULL REFERENCES ledgers(id),
	name        TEXT NOT NULL,
	target_cents INTEGER NOT NULL CHECK (target_cents > 0),
	target_date TEXT,
	account_id  TEXT REFERENCES accounts(id),  -- 绑定储蓄账户，进度=该账户余额增量
	note        TEXT,
	done        INTEGER NOT NULL DEFAULT 0,
	created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
