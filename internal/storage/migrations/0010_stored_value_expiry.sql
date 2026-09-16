-- 0010_stored_value_expiry.sql: 储值卡扩展——卡面额与到期日。
-- 面额仅记录展示；到期日用于首页待办与日历提醒（到期前 7 天）。
ALTER TABLE accounts ADD COLUMN face_value_cents INTEGER;
ALTER TABLE accounts ADD COLUMN expires_on TEXT; -- YYYY-MM-DD，储值卡到期日
