-- 0011_amortization.sql: 分摊计划（分期计提）。
-- 大额支出按期摊销为费用，关联原交易；每期生成一条正式费用分录。
-- 金额整数分，尾差落最后一期（不会累计漂移）。
CREATE TABLE amortization_plans (
	id            TEXT PRIMARY KEY,
	ledger_id     TEXT NOT NULL REFERENCES ledgers(id),
	tx_id         TEXT NOT NULL REFERENCES transactions(id) UNIQUE, -- 原大额支出
	category_id   TEXT NOT NULL REFERENCES categories(id),  -- 摊销费用分类
	account_id    TEXT NOT NULL REFERENCES accounts(id),    -- 摊销费用出账账户
	total_cents   INTEGER NOT NULL,                          -- 分摊总额（分）
	periods       INTEGER NOT NULL,                          -- 总期数
	period_cents  INTEGER NOT NULL,                          -- 每期金额（尾差落最后一期）
	anchor_date   TEXT NOT NULL,                             -- 首期计提日
	done_periods  INTEGER NOT NULL DEFAULT 0,                -- 已计提期数
	status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','done','cancelled')),
	created_by    TEXT NOT NULL REFERENCES users(id),
	created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_amort_ledger ON amortization_plans(ledger_id, status);

CREATE TABLE amortization_entries (
	id         TEXT PRIMARY KEY,
	plan_id    TEXT NOT NULL REFERENCES amortization_plans(id),
	period_no  INTEGER NOT NULL,
	tx_id      TEXT NOT NULL REFERENCES transactions(id),   -- 该期费用分录
	amount_cents INTEGER NOT NULL,
	posted_date TEXT NOT NULL,                               -- 计提业务日期
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
	UNIQUE (plan_id, period_no)
);
