-- 033 AI 智能查询系统：ask_reader 只读角色 + 问答历史表（方案 §1）。
-- ask_reader：DB 权限级白名单——仅 GRANT SELECT 18 张主表（users/audit_log 等敏感表
-- 在 DB 层即不可读）；lab 为迁移执行者（owner），允许其在事务内 SET LOCAL ROLE ask_reader。
-- 后续 migrations 新增主表时，须同步 GRANT SELECT ON <新表> TO ask_reader。
CREATE ROLE ask_reader NOLOGIN;

GRANT SELECT ON daily_reports, logs, issues, issue_comments, experiences,
    test_data, rf_matching_records, assembly_steps, experiment_runs, run_steps,
    step_templates, step_template_items, instrument_results, attachments, todos,
    automation_rules, projects, project_members TO ask_reader;

GRANT ask_reader TO lab;

-- 问答历史表：append-only 全库查询记录，非业务表——豁免 updated_at/project_id 必含约定
-- （AGENTS.md §5 ask 全库只读例外条目）。rows JSONB 存结果快照（总字节 ≤256KB，见方案 §5）。
CREATE TABLE ask_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    request_id  VARCHAR(64) NOT NULL DEFAULT '',
    question    TEXT NOT NULL,
    answer      TEXT NOT NULL DEFAULT '',
    sql_text    TEXT NOT NULL DEFAULT '',
    table_name  TEXT NOT NULL DEFAULT '',
    columns     JSONB NOT NULL DEFAULT '[]',
    rows        JSONB NOT NULL DEFAULT '[]',
    row_count   INT  NOT NULL DEFAULT 0,
    duration_ms INT  NOT NULL DEFAULT 0,
    model       VARCHAR(64) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ask_history_user_time ON ask_history(user_id, created_at DESC);
