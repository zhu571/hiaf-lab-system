-- 029 down: 拆除 hash 链（触发器/函数/列）。
DROP TRIGGER IF EXISTS audit_log_chain_bi ON audit_log;
DROP FUNCTION IF EXISTS audit_log_chain_bi();
DROP FUNCTION IF EXISTS audit_chain_content(BIGINT, TEXT, UUID, TEXT, TEXT, TEXT, TEXT, INT, TEXT, JSONB, TEXT, UUID, UUID, TEXT, TIMESTAMPTZ);
ALTER TABLE audit_log
    DROP COLUMN IF EXISTS prev_hash,
    DROP COLUMN IF EXISTS hash;
