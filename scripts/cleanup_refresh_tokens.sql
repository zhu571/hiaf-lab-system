-- cleanup_refresh_tokens.sql —— refresh token 过期行清理（供部署后 cron 定时执行，不放进 server 代码）。
-- F+ 方案（迁移 038）：revoke 时物理删除主表行并把摘要写入 revoked_tokens 黑名单；
-- 黑名单行保留到 token 原过期时间。本脚本定期清理两表的过期行，防止长期运行膨胀：
--   * revoked_tokens：expires_at 过期后复用检测已不再命中（FindRevokedRefreshToken 带
--     expires_at > now() 过滤），可安全删除；
--   * refresh_tokens：expires_at 过期后 FindRefreshToken 已过滤，可安全删除。
-- 用法示例（cron，每天 03:30 执行）：
--   PGPASSWORD=xxx psql -h 127.0.0.1 -U lab -d lab -f scripts/cleanup_refresh_tokens.sql
-- 注意：refresh_tokens 的 revoked=TRUE 行（迁移 038 之前的遗留）不在两个查找路径
-- （FindRefreshToken 只查 revoked=FALSE、复用检测查黑名单表），属死数据，
-- 一并删除（滚动部署窗口内旧代码 revoke 仍会写 revoked=TRUE，本脚本兜底）。
DELETE FROM revoked_tokens WHERE expires_at <= now();
DELETE FROM refresh_tokens WHERE revoked = TRUE OR expires_at <= now();
