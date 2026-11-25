-- 048: 调度任务跨副本认领（AR-01）。
-- 单实例部署下无害；多副本时同一 job+run_key 只有一个副本能插入成功，
-- 其余副本跳过，避免周报/待办/清理重复执行。
CREATE TABLE scheduler_claims (
    job        TEXT NOT NULL,
    run_key    TEXT NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (job, run_key)
);

CREATE INDEX idx_scheduler_claims_claimed_at ON scheduler_claims(claimed_at);
