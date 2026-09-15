-- +no_tx
-- 0005_discount_split.sql: transaction_splits 增加 'discount'（支付优惠，
-- 应付 10 实付 9：费用按应付计、优惠冲减费用、实付出账）。
-- 本文件自带事务；foreign_keys 必须在事务外切换。

PRAGMA foreign_keys=OFF;

BEGIN;

CREATE TABLE transaction_splits_new (
	id            TEXT PRIMARY KEY,
	tx_id         TEXT NOT NULL REFERENCES transactions(id),
	part_type     TEXT NOT NULL CHECK (part_type IN ('expense','income','receivable','discount')),
	category_id   TEXT REFERENCES categories(id),
	counterparty  TEXT,
	amount_cents  INTEGER NOT NULL CHECK (amount_cents > 0)
);

INSERT INTO transaction_splits_new (id,tx_id,part_type,category_id,counterparty,amount_cents)
	SELECT id,tx_id,part_type,category_id,counterparty,amount_cents FROM transaction_splits;

DROP TABLE transaction_splits;
ALTER TABLE transaction_splits_new RENAME TO transaction_splits;
CREATE INDEX idx_splits_tx ON transaction_splits(tx_id);

COMMIT;

PRAGMA foreign_keys=ON;
