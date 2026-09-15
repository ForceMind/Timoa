-- 0007_import_attachments.sql: 导入批次、来源单号强去重、附件。

ALTER TABLE transactions ADD COLUMN source_tx_no TEXT;
-- 同来源交易编号强去重（编号以来源为前缀存储，如 wechat:123）
CREATE UNIQUE INDEX idx_tx_source_no ON transactions(ledger_id, source_tx_no) WHERE source_tx_no IS NOT NULL;

CREATE TABLE import_batches (
	id          TEXT PRIMARY KEY,
	ledger_id   TEXT NOT NULL REFERENCES ledgers(id),
	source      TEXT NOT NULL,                -- generic_csv | wechat | alipay
	filename    TEXT NOT NULL,
	total_rows  INTEGER NOT NULL DEFAULT 0,
	imported    INTEGER NOT NULL DEFAULT 0,
	skipped     INTEGER NOT NULL DEFAULT 0,
	failed      INTEGER NOT NULL DEFAULT 0,
	status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','undone')),
	rows_json   TEXT NOT NULL,                -- 解析后的行（预览阶段不入正式账）
	created_by  TEXT NOT NULL,
	created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	confirmed_at TEXT,
	undone_at   TEXT
);

CREATE TABLE attachments (
	id           TEXT PRIMARY KEY,
	ledger_id    TEXT NOT NULL REFERENCES ledgers(id),
	tx_id        TEXT NOT NULL REFERENCES transactions(id),
	file_name    TEXT NOT NULL,               -- 原始文件名（仅展示，不用于存储路径）
	stored_name  TEXT NOT NULL,               -- 随机存储名
	content_type TEXT NOT NULL,
	size         INTEGER NOT NULL,
	created_by   TEXT NOT NULL,
	created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_attach_tx ON attachments(tx_id);
