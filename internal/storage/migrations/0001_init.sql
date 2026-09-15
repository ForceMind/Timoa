-- 0001_init.sql: application metadata only. Business schema lands in
-- later migrations (auth/ledger in 0002+).
CREATE TABLE IF NOT EXISTS app_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
