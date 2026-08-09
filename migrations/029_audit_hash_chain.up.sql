-- 029: audit_log SHA-256 hash 链防篡改（C7）。
-- prev_hash/hash 两列 + 规范化函数 + BEFORE INSERT 链触发器 + 历史回填。
--
-- 并发正确性（关键）：BIGSERIAL 的 nextval 在 BEFORE INSERT 触发器执行前已分配，
-- 仅触发器内加锁会产生"id 序 ≠ 锁序"分叉。因此 advisory lock 必须前置到应用层
-- insertAuditLog（go-server/middleware/audit.go，两条写入路径唯一汇聚点）的
-- INSERT 同一事务（SELECT pg_advisory_xact_lock(714001) 再 INSERT，id 分配在锁内、
-- id 序==链序）；触发器内锁保留（同事务可重入），防 psql 直插等非应用层写入；
-- 回填 DO 块用同 key 串行。pgcrypto 已由 001 启用。
--
-- 成本说明：审计写入被全局串行化——审计量级为"每写请求一行"，内网单站可接受。
-- 009_test_data 清表后新插入自动从创世块重建链，无需改 009。

ALTER TABLE audit_log
    ADD COLUMN prev_hash CHAR(64),
    ADD COLUMN hash      CHAR(64);

-- 规范化函数：写入、回填、校验共用同一实现（防漂移）。
-- created_at 固定 UTC 微秒格式；detail 用 JSONB::text（PG 对 jsonb 归一化 key 顺序，输出确定）。
CREATE OR REPLACE FUNCTION audit_chain_content(
    p_id BIGINT, p_request_id TEXT, p_user_id UUID, p_username TEXT, p_method TEXT,
    p_path TEXT, p_action TEXT, p_status_code INT, p_client_ip TEXT, p_detail JSONB,
    p_actor_type TEXT, p_acting_user_id UUID, p_agent_task_id UUID,
    p_idempotency_key TEXT, p_created_at TIMESTAMPTZ
) RETURNS TEXT AS $$
    SELECT p_id::text || '|' || p_request_id || '|' || COALESCE(p_user_id::text,'') || '|' ||
           COALESCE(p_username,'') || '|' || p_method || '|' || p_path || '|' || p_action || '|' ||
           p_status_code::text || '|' || COALESCE(p_client_ip,'') || '|' || COALESCE(p_actor_type,'user') || '|' ||
           COALESCE(p_acting_user_id::text,'') || '|' || COALESCE(p_agent_task_id::text,'') || '|' ||
           COALESCE(p_idempotency_key,'') || '|' || COALESCE(p_detail::text,'') || '|' ||
           to_char(p_created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
$$ LANGUAGE sql IMMUTABLE;

-- BEFORE INSERT 触发器：串行取上一条 hash 计算本条（NEW.id 由 BIGSERIAL 默认值先分配）。
CREATE OR REPLACE FUNCTION audit_log_chain_bi() RETURNS trigger AS $$
DECLARE
    prev TEXT;
BEGIN
    PERFORM pg_advisory_xact_lock(714001);
    SELECT hash INTO prev FROM audit_log WHERE id < NEW.id ORDER BY id DESC LIMIT 1;
    NEW.prev_hash := COALESCE(prev, repeat('0', 64));  -- 创世块固定 64 个 0
    NEW.hash := encode(digest(NEW.prev_hash || '|' ||
        audit_chain_content(NEW.id, NEW.request_id, NEW.user_id, NEW.username, NEW.method,
                            NEW.path, NEW.action, NEW.status_code, NEW.client_ip, NEW.detail,
                            NEW.actor_type, NEW.acting_user_id, NEW.agent_task_id,
                            NEW.idempotency_key, NEW.created_at), 'sha256'), 'hex');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_log_chain_bi
    BEFORE INSERT ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_chain_bi();

-- 历史回填（一次性；维护窗口执行；同 key advisory lock 串行）。
-- 触发器只拦 INSERT，本块的 UPDATE 不触发链重算。
DO $$
DECLARE
    r    RECORD;
    prev TEXT := repeat('0', 64);
    h    TEXT;
BEGIN
    PERFORM pg_advisory_xact_lock(714001);
    FOR r IN SELECT * FROM audit_log ORDER BY id LOOP
        h := encode(digest(prev || '|' ||
            audit_chain_content(r.id, r.request_id, r.user_id, r.username, r.method,
                                r.path, r.action, r.status_code, r.client_ip, r.detail,
                                r.actor_type, r.acting_user_id, r.agent_task_id,
                                r.idempotency_key, r.created_at), 'sha256'), 'hex');
        UPDATE audit_log SET prev_hash = prev, hash = h WHERE id = r.id;
        prev := h;
    END LOOP;
END $$;
