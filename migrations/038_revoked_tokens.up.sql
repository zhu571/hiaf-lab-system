-- 038 revoked_tokens 黑名单表（F+：refresh token 性能根治方案）。
-- 背景：FindRefreshToken/FindRevokedRefreshToken 用 crypt() bcrypt 全表扫描，revoked 行堆积
-- （1402 行时每次 refresh 401 要 3.6 秒）。根治：revoke 时物理删除主表行（不再 UPDATE
-- revoked=TRUE 堆积），并把 token 摘要写入本黑名单表；复用检测（IsRefreshTokenReuse）
-- 改查黑名单，按 sha256 摘要唯一索引 O(1) 命中，保留 P2-4 真复用重放检测能力。
-- token_lookup 为 encode(digest(rawToken,'sha256'),'hex')，与 Go 仓储层 SQL 内一致；
-- 黑名单行保留到 token 原过期时间（expires_at），由 scripts/cleanup_refresh_tokens.sql
-- 定期清理。
CREATE TABLE revoked_tokens (
    id          BIGSERIAL PRIMARY KEY,
    token_lookup TEXT        NOT NULL,
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ  NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_revoked_tokens_lookup ON revoked_tokens (token_lookup);
CREATE INDEX idx_revoked_tokens_expires ON revoked_tokens (expires_at);

-- 清理旧方案遗留的 revoked=TRUE 行：新代码 revoke 改为物理删除主表行，不再写
-- revoked=TRUE；复用检测已改查黑名单表。遗留 revoked 行对 FindRefreshToken
-- （只查 revoked=FALSE）与 FindRevokedRefreshToken（只查黑名单）都是死数据，
-- 仅拖慢 FindRefreshToken 的 crypt() 全表扫描——正是线上 1402 行导致 refresh
-- 401 耗时 3.6s 的根因，随迁移立即物理删除，部署即生效（过期行由
-- scripts/cleanup_refresh_tokens.sql 定期清理，此处一并处理）。
DELETE FROM refresh_tokens WHERE revoked = TRUE OR expires_at <= now();
