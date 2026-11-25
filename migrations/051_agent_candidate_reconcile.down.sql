-- 051 down
DROP INDEX IF EXISTS idx_agent_candidate_stale;
ALTER TABLE agent_candidate_actions
    DROP COLUMN IF EXISTS last_attempt_at,
    DROP COLUMN IF EXISTS execution_attempts;
