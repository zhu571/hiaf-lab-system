ALTER TABLE projects DROP COLUMN IF EXISTS comment_policy;
ALTER TABLE project_members DROP COLUMN IF EXISTS overrides;
ALTER TABLE project_members DROP COLUMN IF EXISTS muted;
DROP INDEX IF EXISTS idx_project_members_role;
