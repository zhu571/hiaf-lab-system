CREATE TABLE experiment_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    name VARCHAR(256) NOT NULL,
    campaign VARCHAR(128),
    run_type VARCHAR(16) NOT NULL DEFAULT 'test'
        CHECK (run_type IN ('cooldown','warmup','steady_state','test')),
    status VARCHAR(16) NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned','active','paused','completed','aborted')),
    gas_type VARCHAR(16) NOT NULL DEFAULT 'He',
    target_temp DOUBLE PRECISION,
    min_temp DOUBLE PRECISION,
    pressure_min DOUBLE PRECISION,
    pressure_max DOUBLE PRECISION,
    pressure_unit VARCHAR(8) NOT NULL DEFAULT 'mbar',
    has_beam BOOLEAN NOT NULL DEFAULT false,
    devices TEXT[] NOT NULL DEFAULT '{}'
        CHECK (devices <@ ARRAY['rf_carpet','rfq','qpig']::TEXT[]),
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    deleted_at TIMESTAMPTZ,
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE TABLE daily_report_run_links (
    report_id UUID NOT NULL REFERENCES daily_reports(id) ON DELETE RESTRICT,
    run_id UUID NOT NULL REFERENCES experiment_runs(id) ON DELETE RESTRICT,
    PRIMARY KEY (report_id, run_id)
);

CREATE INDEX idx_runs_project ON experiment_runs(project_id, status, created_at DESC);
CREATE INDEX idx_runs_campaign ON experiment_runs(campaign) WHERE campaign IS NOT NULL;
CREATE INDEX idx_runs_deleted ON experiment_runs(deleted_at) WHERE deleted_at IS NOT NULL;
