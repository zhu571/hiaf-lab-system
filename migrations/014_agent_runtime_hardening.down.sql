DROP TRIGGER IF EXISTS trigger_submit_enqueue_agent ON daily_reports;
DROP FUNCTION IF EXISTS trg_submit_enqueue_agent_task();

ALTER TABLE audit_log
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS agent_task_id,
    DROP COLUMN IF EXISTS acting_user_id,
    DROP COLUMN IF EXISTS actor_type;

DROP TABLE IF EXISTS agent_candidate_actions;

ALTER TABLE pending_agent_tasks
    DROP CONSTRAINT IF EXISTS unique_report_task,
    DROP CONSTRAINT IF EXISTS check_pending_status,
    DROP COLUMN IF EXISTS agent_confidence,
    DROP COLUMN IF EXISTS prompt_version,
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS result,
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS acting_user_id;
