CREATE TABLE experiences (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID REFERENCES projects(id),
    title           VARCHAR(256) NOT NULL,
    content         TEXT NOT NULL,
    tags_json       JSONB NOT NULL DEFAULT '[]',
    status          VARCHAR(16) NOT NULL DEFAULT 'candidate'
                    CHECK (status IN ('candidate','published','archived')),
    author_id       UUID NOT NULL REFERENCES users(id),
    reviewer_id     UUID REFERENCES users(id),
    published_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_experiences_status ON experiences(status);
CREATE INDEX idx_experiences_project ON experiences(project_id);
CREATE INDEX idx_experiences_tags ON experiences USING GIN (tags_json);

CREATE TABLE experience_project_links (
    experience_id UUID NOT NULL REFERENCES experiences(id) ON DELETE CASCADE,
    project_id    UUID NOT NULL REFERENCES projects(id),
    relation      VARCHAR(16) NOT NULL DEFAULT 'applicable'
                  CHECK (relation IN ('primary','applicable','derived_from')),
    PRIMARY KEY (experience_id, project_id)
);

CREATE INDEX idx_epl_project ON experience_project_links(project_id);
