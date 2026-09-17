-- 0012_platform_role.sql: 平台级角色，支持多租户 SaaS 形态。
--
-- platform_role 与账本内的 ledger_members.role 是两个维度：
--   - ledger_members.role ('admin'/'member')：某个账本内的权限，不变；
--   - users.platform_role ('user'/'superadmin')：整个部署实例的平台级身份。
-- superadmin 只看跨用户统计/元数据（用户列表、账本数、交易笔数、存储占用），
-- 不看任何账本明细；明细仍严格按账本成员隔离。
ALTER TABLE users ADD COLUMN platform_role TEXT NOT NULL DEFAULT 'user'
	CHECK (platform_role IN ('user','superadmin'));

-- 注册开关（可配置关闭公开注册）；放配置表而非硬编码，超管可在后台切换。
CREATE TABLE IF NOT EXISTS platform_settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
INSERT INTO platform_settings(key,value) VALUES('registration_open','1')
	ON CONFLICT(key) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_users_platform_role ON users(platform_role);
