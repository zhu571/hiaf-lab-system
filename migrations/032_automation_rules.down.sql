-- 回滚 032：触发器退回 031 的完整 ON CONFLICT 版，再删除规则表。
-- 注意：回滚后入队逻辑退回硬编码（单规则语义），规则表数据随 DROP 一并删除。
CREATE OR REPLACE FUNCTION trg_submit_enqueue_agent_task()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.content_status = 'submitted' AND OLD.content_status != 'submitted' THEN
        INSERT INTO pending_agent_tasks(report_id, acting_user_id)
        VALUES (NEW.id, NEW.author_id)
        ON CONFLICT (report_id) WHERE status IN ('pending','processing') DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TABLE IF EXISTS automation_rules;
