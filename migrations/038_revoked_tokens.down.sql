-- 038 down：回滚黑名单表（仅 DROP；主表 refresh_tokens 的物理删除行为由代码控制，
-- 回滚后新 revoke 走回 UPDATE revoked=TRUE 语义，无需数据迁移）。
DROP TABLE IF EXISTS revoked_tokens;
