-- 047: 多副本共享限流计数（AR-01）。
-- 进程内 map 在多副本下失效；改为 Postgres UPSERT 原子计数。
-- 窗口按 UTC 分钟截断；过期行由 server 每日清理（StartRateLimitCleanup）。
CREATE TABLE rate_limit_counters (
    scope        TEXT NOT NULL,
    subject      TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    count        INT NOT NULL DEFAULT 0,
    PRIMARY KEY (scope, subject, window_start)
);

CREATE INDEX idx_rate_limit_window ON rate_limit_counters(window_start);
