ALTER TABLE experiences
    DROP COLUMN IF EXISTS candidate_id;
ALTER TABLE issues
    DROP COLUMN IF EXISTS candidate_id;
ALTER TABLE pending_agent_tasks
    DROP COLUMN IF EXISTS raw_text_snapshot,
    DROP COLUMN IF EXISTS raw_text_sha256,
    DROP COLUMN IF EXISTS report_date;
