-- 009_test_data.down.sql
-- 回滚 009_test_data.up.sql：按固定 UUID 前缀删除测试数据。

BEGIN;

DELETE FROM issue_log_links        WHERE issue_id::text LIKE 'e0000000-%';
DELETE FROM issue_project_links     WHERE issue_id::text LIKE 'e0000000-%';
DELETE FROM issue_comments          WHERE issue_id::text LIKE 'e0000000-%';
DELETE FROM issues                  WHERE id::text LIKE 'e0000000-%';
DELETE FROM daily_report_log_links  WHERE daily_report_id::text LIKE 'c0000000-%'
                                     OR log_id::text LIKE 'd0000000-%';
DELETE FROM logs                    WHERE id::text LIKE 'd0000000-%';
DELETE FROM daily_reports           WHERE id::text LIKE 'c0000000-%';
DELETE FROM experience_project_links WHERE experience_id::text LIKE 'f0000000-%';
DELETE FROM experiences             WHERE id::text LIKE 'f0000000-%';
DELETE FROM project_members         WHERE project_id::text LIKE 'b0000000-%';
DELETE FROM projects                WHERE id::text LIKE 'b0000000-%';
DELETE FROM audit_log               WHERE request_id IN
    ('req_20260628_000001', 'req_20260701_000002', 'req_20260705_000003',
     'req_20260706_000004', 'req_20260715_000005');
DELETE FROM refresh_tokens          WHERE user_id::text LIKE 'a0000000-%';
DELETE FROM users                   WHERE id::text LIKE 'a0000000-%';

COMMIT;
