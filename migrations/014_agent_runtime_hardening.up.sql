ALTER TABLE pending_agent_tasks
    ADD COLUMN acting_user_id UUID REFERENCES users(id),
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN next_attempt_at TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD COLUMN result JSONB,
    ADD COLUMN model VARCHAR(64),
    ADD COLUMN prompt_version VARCHAR(32),
    ADD COLUMN agent_confidence DOUBLE PRECISION,
    ADD CONSTRAINT check_pending_status CHECK (status IN ('pending','processing','done','failed','dead')),
    ADD CONSTRAINT unique_report_task UNIQUE(report_id);

UPDATE pending_agent_tasks task
SET acting_user_id = report.author_id
FROM daily_reports report
WHERE task.report_id = report.id AND task.acting_user_id IS NULL;

CREATE TABLE agent_candidate_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES pending_agent_tasks(id),
    action_type VARCHAR(32) NOT NULL,
    project_id UUID REFERENCES projects(id),
    pool_action_key VARCHAR(256) NOT NULL UNIQUE,
    payload JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending_review',
    agent_confidence DOUBLE PRECISION,
    reviewed_by UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    review_reason TEXT,
    executed_at TIMESTAMPTZ,
    execution_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_candidate_actions_status ON agent_candidate_actions(status, created_at);
CREATE INDEX idx_candidate_actions_task ON agent_candidate_actions(task_id);

ALTER TABLE audit_log
    ADD COLUMN actor_type VARCHAR(16) DEFAULT 'user',
    ADD COLUMN acting_user_id UUID REFERENCES users(id),
    ADD COLUMN agent_task_id UUID,
    ADD COLUMN idempotency_key VARCHAR(256);

CREATE OR REPLACE FUNCTION trg_submit_enqueue_agent_task()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.content_status = 'submitted' AND OLD.content_status != 'submitted' THEN
        INSERT INTO pending_agent_tasks(report_id, acting_user_id)
        VALUES (NEW.id, NEW.author_id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_submit_enqueue_agent
    AFTER UPDATE ON daily_reports
    FOR EACH ROW EXECUTE FUNCTION trg_submit_enqueue_agent_task();
