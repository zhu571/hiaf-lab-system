-- 035 告警中心模块（方案 2026-08-09_alert-center，批 A）：
-- alerts 表：统一上报、聚合去重（部分唯一索引 + 事务内 upsert）、状态管理、
-- TTL 24h 兜底与 90 天滚动清理（维护任务见 alert/service.go）。
--
-- 关键约束：
--   uq_alerts_active_source_title 部分唯一索引 —— 任意时刻同 source+title 至多 1 条
--   active 行（并发防双发的最终防线）；resolved 行不受约束，历史逐条保留。
--   idx_alerts_resolved_cleanup —— 滚动清理（resolved 且 resolved_at 超 90 天）用。
--   level/source/status 枚举由 CHECK 约束强制（新增来源需下一迁移）。
CREATE TABLE alerts (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    level            VARCHAR(16) NOT NULL CHECK (level IN ('info','warning','error','critical')),
    source           VARCHAR(32) NOT NULL
                     CHECK (source IN ('security','instruments','todos','updater','agent','ioc','watchdog')),
    title            VARCHAR(256) NOT NULL,
    detail           TEXT NOT NULL DEFAULT '',
    status           VARCHAR(16) NOT NULL DEFAULT 'active'
                     CHECK (status IN ('active','resolved')),
    occurrence_count INTEGER NOT NULL DEFAULT 1,
    first_seen       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at      TIMESTAMPTZ,
    resolved_by      VARCHAR(64) NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_alerts_active_source_title ON alerts(source, title) WHERE status='active';
CREATE INDEX idx_alerts_status_time ON alerts(status, last_seen DESC);
CREATE INDEX idx_alerts_resolved_cleanup ON alerts(resolved_at) WHERE status='resolved';
