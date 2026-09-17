-- 0013_ops_path.sql: 运营面板的随机入口路径。
-- ops_path 首次由 CLI/serve 启动时生成（如 ops-x7k9p2qm），持久化固定不变，
-- 让后台入口不可被猜到；访问该路径仍需登录且为平台超管。
-- 仅插入缺失键；已有值不被覆盖（保证路径稳定）。
INSERT INTO platform_settings(key, value) VALUES('ops_path', '')
	ON CONFLICT(key) DO NOTHING;
