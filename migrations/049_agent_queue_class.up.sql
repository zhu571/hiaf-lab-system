-- 049: Agent 队列分级（AR-04）。
-- batch：日报解析等后台任务；interactive：多人 Agent 的交互式任务（未来 assistant 写入）。
-- claim 时 interactive 优先，避免对话被批量解析饿死。
ALTER TABLE pending_agent_tasks
    ADD COLUMN queue_class VARCHAR(16) NOT NULL DEFAULT 'batch';

ALTER TABLE pending_agent_tasks
    ADD CONSTRAINT pending_agent_tasks_queue_class_check
    CHECK (queue_class IN ('batch', 'interactive'));

CREATE INDEX idx_pending_agent_tasks_queue
    ON pending_agent_tasks(status, queue_class, created_at);
