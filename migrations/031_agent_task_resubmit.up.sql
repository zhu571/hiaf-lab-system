-- 031 C10 递归防护（防御性加固）：放宽"一报告一任务"为"一报告一在途任务"。
-- 语义：退回再提交 → 新任务行（历史任务保留，pool_action_key 含 task id 任务内去重）；
--       任务在途时重复提交 → ON CONFLICT 静默跳过，不炸 UPDATE。
-- 当前仓库无 submitted→draft 回退路径（二次 INSERT 不可复现），本迁移是
-- 未来加回退路径时的安全网，同时是 032 规则派发触发器的前置。
ALTER TABLE pending_agent_tasks DROP CONSTRAINT unique_report_task;

CREATE UNIQUE INDEX uq_report_task_inflight
    ON pending_agent_tasks(report_id) WHERE status IN ('pending','processing');

-- 完整 ON CONFLICT 版触发器（032 将以规则派发版替换本版）。
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
