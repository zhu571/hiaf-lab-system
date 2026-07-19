CREATE TABLE daily_reports (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_date    DATE NOT NULL,
    author_id      UUID NOT NULL REFERENCES users(id),
    raw_text       TEXT NOT NULL DEFAULT '',
    summary        TEXT NOT NULL DEFAULT '',
    content_status VARCHAR(16) NOT NULL DEFAULT 'draft'
                   CHECK (content_status IN ('draft','submitted','confirmed','locked')),
    quality_status VARCHAR(16) NOT NULL DEFAULT 'unchecked'
                   CHECK (quality_status IN ('unchecked','passed','warnings')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (report_date, author_id)
);

CREATE INDEX idx_daily_reports_author_date ON daily_reports(author_id, report_date DESC);
CREATE INDEX idx_daily_reports_status ON daily_reports(content_status);

CREATE TABLE logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES projects(id),
    author_id       UUID NOT NULL REFERENCES users(id),
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    category        VARCHAR(32) NOT NULL DEFAULT 'general'
                    CHECK (category IN ('general','assembly','test','cryo','rf','vacuum','beam','data_analysis')),
    content         TEXT NOT NULL,
    source          VARCHAR(16) NOT NULL DEFAULT 'manual'
                    CHECK (source IN ('manual','agent','import','wechat')),
    content_status  VARCHAR(16) NOT NULL DEFAULT 'draft'
                    CHECK (content_status IN ('draft','confirmed','locked','voided')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_logs_project_occurred_at ON logs(project_id, occurred_at DESC);
CREATE INDEX idx_logs_author ON logs(author_id);
CREATE INDEX idx_logs_category ON logs(category);
CREATE INDEX idx_logs_status ON logs(content_status);

CREATE TABLE daily_report_log_links (
    daily_report_id UUID NOT NULL REFERENCES daily_reports(id) ON DELETE CASCADE,
    log_id          UUID NOT NULL REFERENCES logs(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (daily_report_id, log_id)
);

CREATE INDEX idx_links_log ON daily_report_log_links(log_id);
