-- 045: 幂等语义 V2（AR-03）。
--
-- 旧实现：idempotency_key 全局唯一，重复请求直接 409，不保存响应；
-- 新实现：按 (user_id, method, path, key) 作用域，保存响应并原样重放；
--         5xx/panic 释放 key；expires_at 到期由 server 定时清理。
--
-- 注意：023 已建 response_status / response_body（当时未使用），本迁移复用，
-- 只补 response_content_type 与状态列；response_body 为 TEXT，存 JSON 文本。
--
-- 兼容：迁移前的存量行 user_id/method/path 为空，scope_key 形如 "|||key"，
-- 仍按旧语义返回 409（middleware 对 response_body 为 NULL 的 done 行做降级）。
ALTER TABLE idempotency_keys
    ADD COLUMN user_id UUID,
    ADD COLUMN method VARCHAR(10) NOT NULL DEFAULT '',
    ADD COLUMN path VARCHAR(256) NOT NULL DEFAULT '',
    ADD COLUMN state VARCHAR(16) NOT NULL DEFAULT 'done',
    ADD COLUMN response_content_type VARCHAR(128),
    ADD COLUMN locked_at TIMESTAMPTZ,
    ADD COLUMN expires_at TIMESTAMPTZ NOT NULL DEFAULT now() + interval '7 days';

ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;

ALTER TABLE idempotency_keys
    ADD COLUMN scope_key TEXT GENERATED ALWAYS AS (
        COALESCE(user_id::text, '') || '|' || method || '|' || path || '|' || idempotency_key
    ) STORED;

CREATE UNIQUE INDEX uq_idempotency_scope ON idempotency_keys(scope_key);
CREATE INDEX idx_idempotency_expires ON idempotency_keys(expires_at);

ALTER TABLE idempotency_keys
    ADD CONSTRAINT idempotency_state_check CHECK (state IN ('in_progress', 'done'));
