-- 0014_must_change_password.sql: 首次部署自动生成的平台超管使用初始随机密码，
-- 登录后必须立即改密。must_change_password=1 时前端强制弹改密框，改密成功即清除。
ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0;
