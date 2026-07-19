-- 回滚 010_user_disabled.up.sql

ALTER TABLE users DROP COLUMN disabled;
