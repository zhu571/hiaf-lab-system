ALTER TABLE projects ADD COLUMN comment_policy VARCHAR(16) NOT NULL DEFAULT 'members'
    CHECK (comment_policy IN ('everyone','members','disabled'));

ALTER TABLE project_members ADD COLUMN overrides JSONB NOT NULL DEFAULT '{}';
ALTER TABLE project_members ADD COLUMN muted BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX idx_project_members_role ON project_members(project_id, role);
