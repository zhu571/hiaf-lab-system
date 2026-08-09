-- 032 C9 自动化规则声明化（规则引擎一期）：日报提交入队 agent 任务从硬编码改为规则派发。
-- 一期边界：CHECK 白名单锁死 1 事件（daily_report.submitted）+ 1 动作（enqueue_agent_task），
-- 防规则引擎扩张（if_expr/多步/webhook 不做）；todos/notify 硬编码不纳入一期（YAGNI）。
CREATE TABLE automation_rules (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(128) NOT NULL,
    trigger_event VARCHAR(64)  NOT NULL,
    action        JSONB        NOT NULL,
    enabled       BOOLEAN      NOT NULL DEFAULT true,
    created_by    UUID REFERENCES users(id),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CHECK (trigger_event IN ('daily_report.submitted')),
    CHECK (action->>'type' IN ('enqueue_agent_task'))
);

CREATE INDEX idx_automation_rules_event ON automation_rules(trigger_event) WHERE enabled;

-- 种子规则：等价 014 硬编码触发器行为（迁移后行为不变）。
INSERT INTO automation_rules (name, trigger_event, action, created_by)
VALUES ('日报提交→issue 候选', 'daily_report.submitted',
        '{"type":"enqueue_agent_task","mode":"parse_issues"}'::jsonb, NULL);

-- 触发器改为规则派发版（替换 031 的完整 ON CONFLICT 版；
-- ON CONFLICT 部分索引语义依赖 031 的 uq_report_task_inflight）。
CREATE OR REPLACE FUNCTION trg_submit_enqueue_agent_task()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.content_status = 'submitted' AND OLD.content_status != 'submitted' THEN
        INSERT INTO pending_agent_tasks(report_id, acting_user_id)
        SELECT NEW.id, NEW.author_id
        FROM automation_rules
        WHERE trigger_event = 'daily_report.submitted' AND enabled
              AND action->>'type' = 'enqueue_agent_task'
        ON CONFLICT (report_id) WHERE status IN ('pending','processing') DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
