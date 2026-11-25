-- 046: audit_log 按月原生分区（AR-02 归档路线 A）。
--
-- 目标：为审计表提供可滚动的时间分区，后续可按月 detach/归档；本迁移只做
-- “分区化 + 数据搬迁”，不删除任何审计行，hash 链保持不变。
--
-- 关键点：
--   - 分区键 created_at；PK 必须是 (id, created_at)（PG 对分区表的唯一约束要求）。
--   - 旧表改名为 audit_log_legacy 保留，便于回滚/核对；确认无误后可另行 DROP。
--   - 旧索引/约束改名，避免与新表同名对象冲突。
--   - 先复制数据、后建触发器，避免复制时重算 hash。
--   - 维护窗口：ALTER TABLE 取 ACCESS EXCLUSIVE，复制期间阻塞写入。
--
-- 触发器函数 audit_log_chain_bi() 沿用 029，内部按表名 audit_log 查询；
-- 新表建好后函数自然指向分区父表。未来 detach 分区前必须先解决链锚点
-- （见 docs/architecture-remediation-plan.md AR-02），否则新行 prev_hash 会断链。

ALTER TABLE audit_log RENAME TO audit_log_legacy;
ALTER SEQUENCE audit_log_id_seq RENAME TO audit_log_legacy_id_seq;
ALTER TABLE audit_log_legacy RENAME CONSTRAINT audit_log_pkey TO audit_log_legacy_pkey;
ALTER INDEX idx_audit_log_request_id RENAME TO idx_audit_log_legacy_request_id;
ALTER INDEX idx_audit_log_user_id RENAME TO idx_audit_log_legacy_user_id;
ALTER INDEX idx_audit_log_created_at RENAME TO idx_audit_log_legacy_created_at;

CREATE SEQUENCE audit_log_id_seq;

CREATE TABLE audit_log (
    id              BIGINT       NOT NULL DEFAULT nextval('audit_log_id_seq'),
    request_id      TEXT         NOT NULL,
    user_id         UUID         REFERENCES users(id),
    username        VARCHAR(64),
    method          VARCHAR(10)  NOT NULL,
    path            VARCHAR(256) NOT NULL,
    action          VARCHAR(64)  NOT NULL,
    status_code     INTEGER      NOT NULL,
    client_ip       VARCHAR(64),
    detail          JSONB,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    actor_type      VARCHAR(16)  DEFAULT 'user',
    acting_user_id  UUID         REFERENCES users(id),
    agent_task_id   UUID,
    idempotency_key VARCHAR(256),
    prev_hash       CHAR(64),
    hash            CHAR(64),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- 为存量数据覆盖的每个月建分区（从最早一行到当前月+1）。
DO $$
DECLARE
    start_month DATE;
    end_month   DATE;
    cur         DATE;
BEGIN
    SELECT date_trunc('month', COALESCE(min(created_at), now() - interval '5 years'))::date
      INTO start_month FROM audit_log_legacy;
    end_month := (date_trunc('month', now()) + interval '1 year')::date;
    cur := start_month;
    WHILE cur < end_month LOOP
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF audit_log FOR VALUES FROM (%L) TO (%L)',
            'audit_log_' || to_char(cur, 'YYYYMM'),
            cur,
            cur + interval '1 month'
        );
        cur := cur + interval '1 month';
    END LOOP;
END $$;

INSERT INTO audit_log (
    id, request_id, user_id, username, method, path, action, status_code, client_ip,
    detail, created_at, actor_type, acting_user_id, agent_task_id, idempotency_key,
    prev_hash, hash
)
SELECT id, request_id, user_id, username, method, path, action, status_code, client_ip,
       detail, created_at, actor_type, acting_user_id, agent_task_id, idempotency_key,
       prev_hash, hash
FROM audit_log_legacy;

-- 序列推进到旧表最大 id；空表时下一次从 1 开始。
SELECT setval(
    'audit_log_id_seq',
    COALESCE((SELECT max(id) FROM audit_log), 1),
    (SELECT max(id) IS NOT NULL FROM audit_log)
);

CREATE INDEX idx_audit_log_request_id ON audit_log (request_id);
CREATE INDEX idx_audit_log_user_id ON audit_log (user_id);
CREATE INDEX idx_audit_log_created_at ON audit_log (created_at);

CREATE TRIGGER audit_log_chain_bi
    BEFORE INSERT ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_chain_bi();
