DROP INDEX IF EXISTS idx_pending_agent_tasks_queue;
ALTER TABLE pending_agent_tasks DROP CONSTRAINT IF EXISTS pending_agent_tasks_queue_class_check;
ALTER TABLE pending_agent_tasks DROP COLUMN IF EXISTS queue_class;
