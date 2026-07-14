CREATE TABLE projects (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code             VARCHAR(32)  NOT NULL UNIQUE,
    name             VARCHAR(128) NOT NULL,
    short_name       VARCHAR(64)  NOT NULL DEFAULT '',
    description      TEXT         NOT NULL DEFAULT '',
    status           VARCHAR(16)  NOT NULL DEFAULT 'draft'
                     CHECK (status IN ('draft','active','completed','archived')),
    visibility       VARCHAR(16)  NOT NULL DEFAULT 'restricted'
                     CHECK (visibility IN ('restricted','workspace')),
    owner_user_id    UUID         NOT NULL REFERENCES users(id),
    start_date       DATE,
    target_end_date  DATE,
    completed_at     TIMESTAMPTZ,
    archived_at      TIMESTAMPTZ,
    default_category VARCHAR(64)  NOT NULL DEFAULT '',
    tags_json        JSONB        NOT NULL DEFAULT '[]',
    created_by       UUID         NOT NULL REFERENCES users(id),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_projects_status ON projects(status);
CREATE INDEX idx_projects_owner ON projects(owner_user_id);
CREATE INDEX idx_projects_code ON projects(code);

CREATE TABLE project_members (
    project_id UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id),
    role       VARCHAR(16) NOT NULL DEFAULT 'member'
               CHECK (role IN ('owner','maintainer','member','viewer')),
    status     VARCHAR(16) NOT NULL DEFAULT 'active'
               CHECK (status IN ('active','suspended')),
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by   UUID        NOT NULL REFERENCES users(id),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX idx_project_members_user ON project_members(user_id);
