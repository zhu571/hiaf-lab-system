-- 045 down：恢复 idempotency_keys 的旧主键与旧列集。
-- 注意：V2 期间保存的响应快照会随列删除而丢失；回滚前确认不需要重放。
-- 023 已有的 response_status / response_body 保留不动。
-- V2 允许不同用户/端点复用同一个 key，回滚到“key 全局唯一”前先去重。
DELETE FROM idempotency_keys a
USING idempotency_keys b
WHERE a.ctid < b.ctid AND a.idempotency_key = b.idempotency_key;

ALTER TABLE idempotency_keys DROP CONSTRAINT IF EXISTS idempotency_state_check;

DROP INDEX IF EXISTS idx_idempotency_expires;
DROP INDEX IF EXISTS uq_idempotency_scope;

ALTER TABLE idempotency_keys
    DROP COLUMN IF EXISTS scope_key,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS locked_at,
    DROP COLUMN IF EXISTS response_content_type,
    DROP COLUMN IF EXISTS state,
    DROP COLUMN IF EXISTS path,
    DROP COLUMN IF EXISTS method,
    DROP COLUMN IF EXISTS user_id;

ALTER TABLE idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (idempotency_key);
