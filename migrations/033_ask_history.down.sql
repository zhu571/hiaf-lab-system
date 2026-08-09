-- 回滚 033：先删表，再回收 ask_reader 的表权限，最后删角色
-- （DROP ROLE 前必须 REVOKE，否则因表权限依赖报 "cannot be dropped because some objects depend on it"）。
DROP TABLE IF EXISTS ask_history;

REVOKE ALL PRIVILEGES ON daily_reports, logs, issues, issue_comments, experiences,
    test_data, rf_matching_records, assembly_steps, experiment_runs, run_steps,
    step_templates, step_template_items, instrument_results, attachments, todos,
    automation_rules, projects, project_members FROM ask_reader;

DROP ROLE IF EXISTS ask_reader;
