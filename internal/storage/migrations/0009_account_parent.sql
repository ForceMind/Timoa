-- 0009_account_parent.sql: 账户二类子账户（活期/定期/理财）。
-- parent_id 指向父账户（仅资产类、仅一级，不允许嵌套）；
-- 余额各自独立计算，父账户汇总展示在 listAccounts 内完成。
ALTER TABLE accounts ADD COLUMN parent_id TEXT REFERENCES accounts(id);
CREATE INDEX idx_accounts_parent ON accounts(parent_id);
