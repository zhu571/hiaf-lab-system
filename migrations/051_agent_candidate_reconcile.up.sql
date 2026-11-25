-- 051: 候选执行对账（AR-17）。
-- approved 后进程崩溃/执行器卡住时，由 reconciler 重新认领；attempts/lease 防止重复并发执行。
ALTER TABLE agent_candidate_actions
    ADD COLUMN execution_attempts INTEGER NOT NULL DEFAULT 0
        CHECK (execution_attempts >= 0),
    ADD COLUMN last_attempt_at TIMESTAMPTZ;

CREATE INDEX idx_agent_candidate_stale
    ON agent_candidate_actions (reviewed_at)
    WHERE status = 'approved';
