-- +no_tx
-- 0003_revisions.sql: 拆分、退款/结算关联、不可变修订链。
-- SQLite 无法修改 CHECK 约束，重建 transactions 以扩展业务类型；
-- 原表数据完整复制，禁止 drop-and-recreate 丢数据。
-- 本文件自带事务；foreign_keys 必须在事务外切换。

PRAGMA foreign_keys=OFF;

BEGIN;

-- 应收款项科目：每个已存在账本一个（确定性 id，幂等）
INSERT INTO subjects(id,ledger_id,kind,code,name)
SELECT 'seed-recv-' || l.id, l.id, 'asset', 'asset:receivable', '应收款项'
FROM ledgers l
WHERE NOT EXISTS (SELECT 1 FROM subjects s WHERE s.ledger_id=l.id AND s.code='asset:receivable');

CREATE TABLE transactions_new (
	id              TEXT PRIMARY KEY,
	ledger_id       TEXT NOT NULL REFERENCES ledgers(id),
	type            TEXT NOT NULL CHECK (type IN (
		'expense','income','transfer',
		'refund',         -- 真实退款（关联原消费）
		'income_refund',  -- 收入退回（关联原收入）
		'settlement',     -- 应收/应付结算（回款、还本金等）
		'reclass',        -- 重分类（费用转应收等，不产生现金流）
		'writeoff',       -- 核销（应收转自担费用，需原因）
		'reversal'        -- 技术冲正（不是现金流，不直接展示）
	)),
	status          TEXT NOT NULL DEFAULT 'posted' CHECK (status IN ('draft','posted','voided')),
	business_date   TEXT NOT NULL,
	date_precision  TEXT NOT NULL DEFAULT 'datetime' CHECK (date_precision IN ('datetime','day')),
	amount_cents    INTEGER NOT NULL CHECK (amount_cents > 0),
	category_id     TEXT REFERENCES categories(id),
	from_account_id TEXT REFERENCES accounts(id),
	to_account_id   TEXT REFERENCES accounts(id),
	link_id         TEXT REFERENCES transactions(id),  -- 关联原单（按 type 解释）
	reason          TEXT,                              -- 更正/作废/核销原因
	counterparty    TEXT,                              -- 交易对方（应收/结算）
	note            TEXT,
	merchant        TEXT,
	channel         TEXT,
	created_by      TEXT NOT NULL REFERENCES users(id),
	operation_id    TEXT NOT NULL,
	content_hash    TEXT NOT NULL,
	created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	UNIQUE (ledger_id, operation_id)
);

INSERT INTO transactions_new (id,ledger_id,type,status,business_date,date_precision,amount_cents,
	category_id,from_account_id,to_account_id,note,merchant,channel,created_by,operation_id,content_hash,created_at)
	SELECT id,ledger_id,type,status,business_date,date_precision,amount_cents,
	category_id,from_account_id,to_account_id,note,merchant,channel,created_by,operation_id,content_hash,created_at
	FROM transactions;

DROP TABLE transactions;
ALTER TABLE transactions_new RENAME TO transactions;

CREATE INDEX idx_tx_ledger_date ON transactions(ledger_id, business_date);
CREATE INDEX idx_tx_ledger_type ON transactions(ledger_id, type);
CREATE INDEX idx_tx_link ON transactions(link_id);

-- 拆分：一笔账单拆成多个费用/收入/应收部分
CREATE TABLE transaction_splits (
	id            TEXT PRIMARY KEY,
	tx_id         TEXT NOT NULL REFERENCES transactions(id),
	part_type     TEXT NOT NULL CHECK (part_type IN ('expense','income','receivable')),
	category_id   TEXT REFERENCES categories(id),
	counterparty  TEXT,
	amount_cents  INTEGER NOT NULL CHECK (amount_cents > 0)
);
CREATE INDEX idx_splits_tx ON transaction_splits(tx_id);

-- 退款分摊：退款金额如何对应原单（整单或按拆分项）
CREATE TABLE refund_allocations (
	id           TEXT PRIMARY KEY,
	refund_tx_id TEXT NOT NULL REFERENCES transactions(id),
	split_id     TEXT REFERENCES transaction_splits(id),  -- 空 = 整单（无拆分原单）
	amount_cents INTEGER NOT NULL CHECK (amount_cents > 0)
);
CREATE INDEX idx_refund_alloc ON refund_allocations(split_id);

-- 修订链：原单 → 技术冲正 →（可选）替代版本；original 唯一防重复冲正
CREATE TABLE revisions (
	id                TEXT PRIMARY KEY,
	ledger_id         TEXT NOT NULL REFERENCES ledgers(id),
	original_tx_id    TEXT NOT NULL UNIQUE REFERENCES transactions(id),
	reversal_tx_id    TEXT NOT NULL REFERENCES transactions(id),
	replacement_tx_id TEXT REFERENCES transactions(id),
	reason            TEXT NOT NULL,
	created_by        TEXT NOT NULL REFERENCES users(id),
	created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

COMMIT;

PRAGMA foreign_keys=ON;
