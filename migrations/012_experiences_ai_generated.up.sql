ALTER TABLE experiences ADD COLUMN ai_generated BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE experiences ADD COLUMN agent_task_id VARCHAR(64);
