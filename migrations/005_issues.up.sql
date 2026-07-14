CREATE TABLE issues (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects(id),
    title        VARCHAR(256) NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    status       VARCHAR(16) NOT NULL DEFAULT 'open'
                 CHECK (status IN ('open','in_progress','resolved','closed')),
    severity     VARCHAR(16) NOT NULL DEFAULT 'medium'
                 CHECK (severity IN ('low','medium','high','critical')),
    author_id    UUID NOT NULL REFERENCES users(id),
    assignee_id  UUID REFERENCES users(id),
    report_date  DATE NOT NULL DEFAULT CURRENT_DATE,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_issues_project_status ON issues(project_id, status);
CREATE INDEX idx_issues_assignee ON issues(assignee_id);
CREATE INDEX idx_issues_author ON issues(author_id);
CREATE INDEX idx_issues_project_severity ON issues(project_id, severity);

CREATE TABLE issue_log_links (
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    log_id   UUID NOT NULL REFERENCES logs(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, log_id)
);

CREATE TABLE issue_project_links (
    issue_id   UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id),
    relation   VARCHAR(16) NOT NULL DEFAULT 'related'
               CHECK (relation IN ('primary','related','blocked_by','blocks')),
    PRIMARY KEY (issue_id, project_id)
);

CREATE INDEX idx_ipl_project ON issue_project_links(project_id);

CREATE TABLE issue_comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id   UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES users(id),
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_comments_issue_created_at ON issue_comments(issue_id, created_at);
