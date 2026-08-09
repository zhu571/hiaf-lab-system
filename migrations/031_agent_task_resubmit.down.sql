-- 回滚 031：恢复 014 版触发器与"一报告一任务"唯一约束。
-- 前置检查（回滚前必须人工确认无 in-flight 重复行，否则 ADD CONSTRAINT 失败）：
--   SELECT report_id FROM pending_agent_tasks
--   WHERE status IN ('pending','processing')
--   GROUP BY report_id HAVING count(*) > 1;
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

DROP INDEX IF EXISTS uq_report_task_inflight;

ALTER TABLE pending_agent_tasks ADD CONSTRAINT unique_report_task UNIQUE(report_id);
