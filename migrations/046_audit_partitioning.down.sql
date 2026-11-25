-- 046 down：回到单表 audit_log。
-- 注意：分区化之后写入的新审计行会随新表删除而丢失；仅用于上线前回滚。
DROP TABLE IF EXISTS audit_log CASCADE;
DROP SEQUENCE IF EXISTS audit_log_id_seq;

ALTER TABLE audit_log_legacy RENAME TO audit_log;
ALTER SEQUENCE audit_log_legacy_id_seq RENAME TO audit_log_id_seq;
ALTER TABLE audit_log RENAME CONSTRAINT audit_log_legacy_pkey TO audit_log_pkey;
ALTER INDEX idx_audit_log_legacy_request_id RENAME TO idx_audit_log_request_id;
ALTER INDEX idx_audit_log_legacy_user_id RENAME TO idx_audit_log_user_id;
ALTER INDEX idx_audit_log_legacy_created_at RENAME TO idx_audit_log_created_at;
