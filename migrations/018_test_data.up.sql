CREATE TABLE test_data (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    run_id UUID REFERENCES experiment_runs(id) ON DELETE SET NULL,
    data_type VARCHAR(32) NOT NULL
        CHECK (data_type IN ('cryo','pressure','voltage','rf_voltage','efficiency')),
    measurement VARCHAR(128) NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    unit VARCHAR(16) NOT NULL DEFAULT '',
    quality VARCHAR(16) NOT NULL DEFAULT 'normal'
        CHECK (quality IN ('normal','outlier','suspect','invalid')),
    source VARCHAR(16) NOT NULL DEFAULT 'manual'
        CHECK (source IN ('manual','instrument','import','agent','backfill')),
    measured_at TIMESTAMPTZ,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    recorded_by UUID REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_test_data_project ON test_data(project_id, created_at DESC);
CREATE INDEX idx_test_data_run ON test_data(run_id) WHERE run_id IS NOT NULL;
CREATE INDEX idx_test_data_type ON test_data(data_type);
