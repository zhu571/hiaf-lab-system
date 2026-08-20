-- Production cleanup: remove only the five disabled seed users and their data.
-- Run in one psql session. The guards intentionally abort on an unexpected user set
-- or a test-owned project, rather than guessing how to preserve project data.

BEGIN;
SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '5min';

CREATE TEMP TABLE cleanup_users ON COMMIT DROP AS
SELECT id, username
FROM users
WHERE disabled IS TRUE
  AND username IN ('testadmin', 'zhaoliu', 'wangwu', 'zhangsan', 'lisi')
  AND username <> 'agent@system';

DO $$
BEGIN
  IF (SELECT count(*) FROM cleanup_users) <> 5
     OR EXISTS (SELECT 1 FROM cleanup_users WHERE username IN ('haofan','agent@system','Yingfeng_Wu','yanhexuan')) THEN
    RAISE EXCEPTION 'cleanup guard failed: expected exactly five disabled target users';
  END IF;
  IF EXISTS (
    SELECT 1 FROM projects p
    WHERE p.owner_user_id IN (SELECT id FROM cleanup_users)
       OR p.created_by IN (SELECT id FROM cleanup_users)
  ) THEN
    RAISE EXCEPTION 'cleanup guard failed: target user owns/created a project; review manually';
  END IF;
END $$;

CREATE TEMP TABLE cleanup_reports ON COMMIT DROP AS
SELECT id FROM daily_reports WHERE author_id IN (SELECT id FROM cleanup_users);
CREATE TEMP TABLE cleanup_logs ON COMMIT DROP AS
SELECT id FROM logs WHERE author_id IN (SELECT id FROM cleanup_users);
CREATE TEMP TABLE cleanup_issues ON COMMIT DROP AS
SELECT id FROM issues WHERE author_id IN (SELECT id FROM cleanup_users);
CREATE TEMP TABLE cleanup_experiences ON COMMIT DROP AS
SELECT id FROM experiences WHERE author_id IN (SELECT id FROM cleanup_users);
CREATE TEMP TABLE cleanup_tasks ON COMMIT DROP AS
SELECT id FROM pending_agent_tasks
WHERE acting_user_id IN (SELECT id FROM cleanup_users)
   OR report_id IN (SELECT id FROM cleanup_reports);
CREATE TEMP TABLE cleanup_candidates ON COMMIT DROP AS
SELECT id FROM agent_candidate_actions
WHERE task_id IN (SELECT id FROM cleanup_tasks)
   OR id IN (SELECT candidate_id FROM issues WHERE id IN (SELECT id FROM cleanup_issues) AND candidate_id IS NOT NULL)
   OR id IN (SELECT candidate_id FROM experiences WHERE id IN (SELECT id FROM cleanup_experiences) AND candidate_id IS NOT NULL);
CREATE TEMP TABLE cleanup_attachments ON COMMIT DROP AS
SELECT DISTINCT al.attachment_id AS id
FROM attachment_links al
WHERE (al.entity_type = 'issue' AND al.entity_id IN (SELECT id FROM cleanup_issues))
   OR (al.entity_type = 'log' AND al.entity_id IN (SELECT id FROM cleanup_logs))
   OR (al.entity_type = 'daily_report' AND al.entity_id IN (SELECT id FROM cleanup_reports));
CREATE TEMP TABLE cleanup_assigned_issues ON COMMIT DROP AS
SELECT id FROM issues WHERE assignee_id IN (SELECT id FROM cleanup_users)
  AND author_id NOT IN (SELECT id FROM cleanup_users);

-- Baseline for the requested before/after comparison.
CREATE TEMP TABLE cleanup_baseline(table_name text PRIMARY KEY, row_count bigint) ON COMMIT DROP;
INSERT INTO cleanup_baseline VALUES
 ('users', (SELECT count(*) FROM users WHERE id IN (SELECT id FROM cleanup_users))),
 ('project_members', (SELECT count(*) FROM project_members WHERE user_id IN (SELECT id FROM cleanup_users) OR added_by IN (SELECT id FROM cleanup_users))),
 ('issues', (SELECT count(*) FROM issues WHERE id IN (SELECT id FROM cleanup_issues))),
 ('issue_comments', (SELECT count(*) FROM issue_comments WHERE issue_id IN (SELECT id FROM cleanup_issues) OR author_id IN (SELECT id FROM cleanup_users))),
 ('issue_log_links', (SELECT count(*) FROM issue_log_links WHERE issue_id IN (SELECT id FROM cleanup_issues) OR log_id IN (SELECT id FROM cleanup_logs))),
 ('issue_project_links', (SELECT count(*) FROM issue_project_links WHERE issue_id IN (SELECT id FROM cleanup_issues))),
 ('logs', (SELECT count(*) FROM logs WHERE id IN (SELECT id FROM cleanup_logs))),
 ('daily_reports', (SELECT count(*) FROM daily_reports WHERE id IN (SELECT id FROM cleanup_reports))),
 ('daily_report_log_links', (SELECT count(*) FROM daily_report_log_links WHERE daily_report_id IN (SELECT id FROM cleanup_reports) OR log_id IN (SELECT id FROM cleanup_logs))),
 ('daily_report_run_links', (SELECT count(*) FROM daily_report_run_links WHERE report_id IN (SELECT id FROM cleanup_reports))),
 ('experiences', (SELECT count(*) FROM experiences WHERE id IN (SELECT id FROM cleanup_experiences))),
 ('experience_project_links', (SELECT count(*) FROM experience_project_links WHERE experience_id IN (SELECT id FROM cleanup_experiences))),
 ('pending_agent_tasks', (SELECT count(*) FROM pending_agent_tasks WHERE id IN (SELECT id FROM cleanup_tasks))),
 ('agent_candidate_actions', (SELECT count(*) FROM agent_candidate_actions WHERE id IN (SELECT id FROM cleanup_candidates))),
 ('attachments', (SELECT count(*) FROM attachments WHERE uploaded_by IN (SELECT id FROM cleanup_users))),
 ('attachment_links', (SELECT count(*) FROM attachment_links WHERE created_by IN (SELECT id FROM cleanup_users))),
 ('ask_history', (SELECT count(*) FROM ask_history WHERE user_id IN (SELECT id FROM cleanup_users))),
 ('automation_rules', (SELECT count(*) FROM automation_rules WHERE created_by IN (SELECT id FROM cleanup_users))),
 ('instrument_results', (SELECT count(*) FROM instrument_results WHERE user_id IN (SELECT id FROM cleanup_users))),
 ('invitation_codes', (SELECT count(*) FROM invitation_codes WHERE created_by IN (SELECT id FROM cleanup_users))),
 ('todos', (SELECT count(*) FROM todos WHERE created_by IN (SELECT id FROM cleanup_users))),
 ('audit_log_refs', (SELECT count(*) FROM audit_log WHERE user_id IN (SELECT id FROM cleanup_users) OR acting_user_id IN (SELECT id FROM cleanup_users)));

CREATE TEMP TABLE real_baseline AS
SELECT u.username,
       (SELECT count(*) FROM project_members x WHERE x.user_id = u.id) AS members,
       (SELECT count(*) FROM issues x WHERE x.author_id = u.id) AS issues_authored,
       (SELECT count(*) FROM issues x WHERE x.assignee_id = u.id) AS issues_assigned,
       (SELECT count(*) FROM logs x WHERE x.author_id = u.id) AS logs,
       (SELECT count(*) FROM daily_reports x WHERE x.author_id = u.id) AS reports,
       (SELECT count(*) FROM experiences x WHERE x.author_id = u.id) AS experiences
FROM users u
WHERE u.username IN ('haofan','agent@system','Yingfeng_Wu','yanhexuan');

SELECT 'BEFORE' AS phase, table_name, row_count FROM cleanup_baseline ORDER BY table_name;

-- First remove rows that block deletion of reports/tasks/issues/logs/experiences.
DELETE FROM attachment_links
WHERE entity_type = 'issue' AND entity_id IN (SELECT id FROM cleanup_issues)
   OR entity_type = 'log' AND entity_id IN (SELECT id FROM cleanup_logs)
   OR entity_type = 'daily_report' AND entity_id IN (SELECT id FROM cleanup_reports)
;
DELETE FROM attachments WHERE id IN (SELECT id FROM cleanup_attachments);
DELETE FROM daily_report_run_links WHERE report_id IN (SELECT id FROM cleanup_reports);
DELETE FROM agent_candidate_actions WHERE id IN (SELECT id FROM cleanup_candidates);
DELETE FROM issue_comments WHERE author_id IN (SELECT id FROM cleanup_users);
UPDATE issues SET assignee_id = NULL WHERE id IN (SELECT id FROM cleanup_assigned_issues);

-- Keep the report/log/issue/experience rows strictly scoped to the captured IDs.
DELETE FROM issues WHERE id IN (SELECT id FROM cleanup_issues);
DELETE FROM logs WHERE id IN (SELECT id FROM cleanup_logs);
DELETE FROM experiences WHERE id IN (SELECT id FROM cleanup_experiences);
DELETE FROM pending_agent_tasks WHERE id IN (SELECT id FROM cleanup_tasks);
DELETE FROM daily_reports WHERE id IN (SELECT id FROM cleanup_reports);

-- Directly owned rows and nullable attribution references.
DELETE FROM ask_history WHERE user_id IN (SELECT id FROM cleanup_users);
DELETE FROM automation_rules WHERE created_by IN (SELECT id FROM cleanup_users);
DELETE FROM instrument_results WHERE user_id IN (SELECT id FROM cleanup_users);
DELETE FROM invitation_codes WHERE created_by IN (SELECT id FROM cleanup_users);
DELETE FROM todos WHERE created_by IN (SELECT id FROM cleanup_users);
DELETE FROM project_members WHERE user_id IN (SELECT id FROM cleanup_users);
UPDATE project_members pm SET added_by = p.owner_user_id
FROM projects p
WHERE pm.project_id = p.id AND pm.added_by IN (SELECT id FROM cleanup_users);
UPDATE experiences SET reviewer_id = NULL WHERE reviewer_id IN (SELECT id FROM cleanup_users);
UPDATE invitation_codes SET used_by = NULL WHERE used_by IN (SELECT id FROM cleanup_users);
UPDATE invitation_codes SET revoked_by = NULL WHERE revoked_by IN (SELECT id FROM cleanup_users);
UPDATE agent_candidate_actions SET reviewed_by = NULL WHERE reviewed_by IN (SELECT id FROM cleanup_users);
UPDATE todos SET completed_by = NULL WHERE completed_by IN (SELECT id FROM cleanup_users);
UPDATE audit_log SET user_id = NULL WHERE user_id IN (SELECT id FROM cleanup_users);
UPDATE audit_log SET acting_user_id = NULL WHERE acting_user_id IN (SELECT id FROM cleanup_users);

-- Nullable FKs are cleared explicitly for deterministic verification; CASCADE FKs
-- (refresh_tokens/revoked_tokens) are handled by the final user delete.
UPDATE attachments SET uploaded_by = NULL WHERE uploaded_by IN (SELECT id FROM cleanup_users);
UPDATE attachment_links SET created_by = NULL WHERE created_by IN (SELECT id FROM cleanup_users);
UPDATE assembly_steps SET created_by = NULL WHERE created_by IN (SELECT id FROM cleanup_users);
UPDATE assembly_steps SET assigned_to = NULL WHERE assigned_to IN (SELECT id FROM cleanup_users);
UPDATE experiment_runs SET created_by = NULL WHERE created_by IN (SELECT id FROM cleanup_users);
UPDATE rf_matching_records SET voided_by = NULL WHERE voided_by IN (SELECT id FROM cleanup_users);
UPDATE rf_matching_records SET measured_by = NULL WHERE measured_by IN (SELECT id FROM cleanup_users);
UPDATE run_steps SET created_by = NULL WHERE created_by IN (SELECT id FROM cleanup_users);
UPDATE step_templates SET created_by = NULL WHERE created_by IN (SELECT id FROM cleanup_users);
UPDATE test_data SET recorded_by = NULL WHERE recorded_by IN (SELECT id FROM cleanup_users);

DELETE FROM users WHERE id IN (SELECT id FROM cleanup_users);

-- Rebuild the audit hash chain because the preserved audit rows changed nullable
-- user references. This is metadata-only; no audit event rows are removed.
DO $$
DECLARE r record; prev text := repeat('0', 64); h text;
BEGIN
  PERFORM pg_advisory_xact_lock(714001);
  FOR r IN SELECT * FROM audit_log ORDER BY id LOOP
    h := encode(digest(prev || '|' || audit_chain_content(r.id, r.request_id, r.user_id, r.username, r.method,
      r.path, r.action, r.status_code, r.client_ip, r.detail, r.actor_type, r.acting_user_id,
      r.agent_task_id, r.idempotency_key, r.created_at), 'sha256'), 'hex');
    UPDATE audit_log SET prev_hash = prev, hash = h WHERE id = r.id;
    prev := h;
  END LOOP;
END $$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM users WHERE username IN ('testadmin','zhaoliu','wangwu','zhangsan','lisi')) THEN
    RAISE EXCEPTION 'verification failed: target users remain';
  END IF;
  IF EXISTS (SELECT 1 FROM project_members WHERE user_id IN (SELECT id FROM cleanup_users) OR added_by IN (SELECT id FROM cleanup_users)) THEN
    RAISE EXCEPTION 'verification failed: project_members references remain';
  END IF;
  IF EXISTS (SELECT 1 FROM audit_log WHERE user_id IN (SELECT id FROM cleanup_users) OR acting_user_id IN (SELECT id FROM cleanup_users)) THEN
    RAISE EXCEPTION 'verification failed: audit references remain';
  END IF;
END $$;

SELECT 'AFTER' AS phase, table_name, row_count FROM cleanup_baseline ORDER BY table_name;
SELECT username, count(*) AS remaining
FROM users
WHERE username IN ('haofan','agent@system','Yingfeng_Wu','yanhexuan')
GROUP BY username ORDER BY username;
SELECT b.username, b.members, (SELECT count(*) FROM project_members x JOIN users u ON u.id=x.user_id WHERE u.username=b.username) AS members_after,
       b.issues_authored, (SELECT count(*) FROM issues x JOIN users u ON u.id=x.author_id WHERE u.username=b.username) AS issues_authored_after,
       b.issues_assigned, (SELECT count(*) FROM issues x JOIN users u ON u.id=x.assignee_id WHERE u.username=b.username) AS issues_assigned_after,
       b.logs, (SELECT count(*) FROM logs x JOIN users u ON u.id=x.author_id WHERE u.username=b.username) AS logs_after,
       b.reports, (SELECT count(*) FROM daily_reports x JOIN users u ON u.id=x.author_id WHERE u.username=b.username) AS reports_after,
       b.experiences, (SELECT count(*) FROM experiences x JOIN users u ON u.id=x.author_id WHERE u.username=b.username) AS experiences_after
FROM real_baseline b ORDER BY b.username;

COMMIT;
