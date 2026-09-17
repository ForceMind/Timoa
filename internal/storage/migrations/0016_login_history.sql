-- 0016_login_history.sql: 访问记录（登录历史）+ 会话 IP 追踪。
-- 用于运营后台「查看用户的访问记录：IP、设备、浏览器」。

-- 会话表补 IP（登录时的来源 IP）。
ALTER TABLE sessions ADD COLUMN ip TEXT;

-- 登录历史：每次登录/登出一条，含 IP、User-Agent、解析后的设备与浏览器。
CREATE TABLE login_history (
	id         TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id),
	action     TEXT NOT NULL CHECK (action IN ('login','logout')),
	ip         TEXT,
	user_agent TEXT,
	device     TEXT,                -- 解析：手机/平板/桌面
	os         TEXT,                -- iOS/Android/Windows/macOS/Linux…
	browser    TEXT,                -- Chrome/Safari/WeChat…
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_login_history_user ON login_history(user_id);
CREATE INDEX idx_login_history_time ON login_history(created_at DESC);
