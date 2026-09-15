-- +no_tx
-- 0004_templates_recurrence.sql: 资金操作类型、应付款项科目、模板、
-- 标签、便笺、周期规则与实例。transactions 再次重建以扩展类型。
-- 本文件自带事务；foreign_keys 必须在事务外切换。

PRAGMA foreign_keys=OFF;

BEGIN;

-- 应付款项科目（借入负债）
INSERT INTO subjects(id,ledger_id,kind,code,name)
SELECT 'seed-pay-' || l.id, l.id, 'liability', 'liability:payable', '应付款项'
FROM ledgers l
WHERE NOT EXISTS (SELECT 1 FROM subjects s WHERE s.ledger_id=l.id AND s.code='liability:payable');

CREATE TABLE transactions_new (
	id              TEXT PRIMARY KEY,
	ledger_id       TEXT NOT NULL REFERENCES ledgers(id),
	type            TEXT NOT NULL CHECK (type IN (
		'expense','income','transfer',
		'refund','income_refund','settlement','reclass','writeoff','reversal',
		'lend',       -- 借出/支付押金（形成应收，带对方）
		'borrow',     -- 借入（形成应付负债，带对方）
		'repay',      -- 还本金（结清应付，利息单独记费用）
		'loan_repay', -- 房贷/车贷还款（本金减负债 + 利息计费用）
		'redeem'      -- 理财赎回（本金转回 + 已确认收益计收入）
	)),
	status          TEXT NOT NULL DEFAULT 'posted' CHECK (status IN ('draft','posted','voided')),
	business_date   TEXT NOT NULL,
	date_precision  TEXT NOT NULL DEFAULT 'datetime' CHECK (date_precision IN ('datetime','day')),
	amount_cents    INTEGER NOT NULL CHECK (amount_cents > 0),
	category_id     TEXT REFERENCES categories(id),
	from_account_id TEXT REFERENCES accounts(id),
	to_account_id   TEXT REFERENCES accounts(id),
	link_id         TEXT REFERENCES transactions(id),
	reason          TEXT,
	counterparty    TEXT,
	principal_cents INTEGER,          -- loan_repay/redeem：本金部分
	interest_category_id TEXT REFERENCES categories(id), -- loan_repay 利息分类 / redeem 收益分类
	note            TEXT,
	merchant        TEXT,
	channel         TEXT,
	template_id     TEXT,
	recurrence_instance_id TEXT,
	created_by      TEXT NOT NULL REFERENCES users(id),
	operation_id    TEXT NOT NULL,
	content_hash    TEXT NOT NULL,
	created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	UNIQUE (ledger_id, operation_id)
);

INSERT INTO transactions_new (id,ledger_id,type,status,business_date,date_precision,amount_cents,
	category_id,from_account_id,to_account_id,link_id,reason,counterparty,note,merchant,channel,
	created_by,operation_id,content_hash,created_at)
	SELECT id,ledger_id,type,status,business_date,date_precision,amount_cents,
	category_id,from_account_id,to_account_id,link_id,reason,counterparty,note,merchant,channel,
	created_by,operation_id,content_hash,created_at
	FROM transactions;

DROP TABLE transactions;
ALTER TABLE transactions_new RENAME TO transactions;

CREATE INDEX idx_tx_ledger_date ON transactions(ledger_id, business_date);
CREATE INDEX idx_tx_ledger_type ON transactions(ledger_id, type);
CREATE INDEX idx_tx_link ON transactions(link_id);
CREATE INDEX idx_tx_search ON transactions(ledger_id, note, merchant);

-- 分类图标（emoji，本地素材）
ALTER TABLE categories ADD COLUMN icon TEXT;

-- 标签
CREATE TABLE tags (
	id         TEXT PRIMARY KEY,
	ledger_id  TEXT NOT NULL REFERENCES ledgers(id),
	name       TEXT NOT NULL,
	is_seed    INTEGER NOT NULL DEFAULT 0,
	archived_at TEXT,
	UNIQUE (ledger_id, name)
);
CREATE TABLE transaction_tags (
	tx_id  TEXT NOT NULL REFERENCES transactions(id),
	tag_id TEXT NOT NULL REFERENCES tags(id),
	PRIMARY KEY (tx_id, tag_id)
);

-- 模板：负责填单；与分类（统计）、周期规则（推荐）独立建模
CREATE TABLE templates (
	id              TEXT PRIMARY KEY,
	ledger_id       TEXT NOT NULL REFERENCES ledgers(id),
	name            TEXT NOT NULL,
	icon            TEXT,
	tx_type         TEXT NOT NULL,        -- expense/income/transfer/lend/borrow/repay/loan_repay/redeem
	category_id     TEXT REFERENCES categories(id),
	default_account_id TEXT REFERENCES accounts(id),
	amount_policy   TEXT NOT NULL DEFAULT 'manual' CHECK (amount_policy IN ('manual','fixed','last','frequent')),
	fixed_amount_cents INTEGER,
	note_template   TEXT,
	pinned          INTEGER NOT NULL DEFAULT 0,
	sort            INTEGER NOT NULL DEFAULT 0,
	enabled         INTEGER NOT NULL DEFAULT 1,
	is_seed         INTEGER NOT NULL DEFAULT 0,
	created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_templates_ledger ON templates(ledger_id, enabled, pinned, sort);

-- 便笺（非账务内容，不进统计）
CREATE TABLE notes (
	id         TEXT PRIMARY KEY,
	ledger_id  TEXT NOT NULL REFERENCES ledgers(id),
	content    TEXT NOT NULL,
	pinned     INTEGER NOT NULL DEFAULT 0,
	created_by TEXT NOT NULL REFERENCES users(id),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- 周期规则（版本化；规则变更不改过去已完成实例）
CREATE TABLE recurrence_rules (
	id            TEXT PRIMARY KEY,
	ledger_id     TEXT NOT NULL REFERENCES ledgers(id),
	template_id   TEXT REFERENCES templates(id),
	name          TEXT NOT NULL,
	tx_type       TEXT NOT NULL DEFAULT 'expense',
	category_id   TEXT REFERENCES categories(id),
	account_id    TEXT REFERENCES accounts(id),
	frequency     TEXT NOT NULL CHECK (frequency IN ('daily','weekly','biweekly','monthly','month_end','quarterly','yearly','every_n_days')),
	interval_days INTEGER,               -- every_n_days
	by_weekday    INTEGER,               -- weekly/biweekly: 1..7（周一起）
	month_day     INTEGER,               -- monthly: 1..31（短月落月末，下月恢复锚定）
	anchor_date   TEXT NOT NULL,         -- 锚定日（YYYY-MM-DD）
	start_date    TEXT NOT NULL,
	end_date      TEXT,
	amount_policy TEXT NOT NULL DEFAULT 'manual',
	fixed_amount_cents INTEGER,
	shared        INTEGER NOT NULL DEFAULT 1,   -- 账本共享（不按成员重复生成）
	include_in_forecast INTEGER NOT NULL DEFAULT 1,
	enabled       INTEGER NOT NULL DEFAULT 1,
	version       INTEGER NOT NULL DEFAULT 1,
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- 周期实例：稳定唯一键 = 账本 + 规则 + 周期键
CREATE TABLE recurrence_instances (
	id              TEXT PRIMARY KEY,
	ledger_id       TEXT NOT NULL REFERENCES ledgers(id),
	rule_id         TEXT NOT NULL REFERENCES recurrence_rules(id),
	period_key      TEXT NOT NULL,
	planned_date    TEXT NOT NULL,
	planned_amount_cents INTEGER,
	status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','partial','done','skipped','postponed')),
	confirmed_amount_cents INTEGER NOT NULL DEFAULT 0,
	rule_version    INTEGER NOT NULL,
	created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	UNIQUE (ledger_id, rule_id, period_key)
);

COMMIT;

PRAGMA foreign_keys=ON;
