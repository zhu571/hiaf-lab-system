CREATE TABLE test_data_curves (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    run_id      UUID REFERENCES experiment_runs(id) ON DELETE SET NULL,
    name        VARCHAR(128) NOT NULL,
    curve_type  VARCHAR(32) NOT NULL DEFAULT 'impedance_sweep'
                CHECK (curve_type IN ('impedance_sweep','s11_sweep','custom')),
    x_label     VARCHAR(64) NOT NULL DEFAULT '频率 (Hz)',
    y_label     VARCHAR(64) NOT NULL DEFAULT '阻抗 |Z| (Ω)',
    unit        VARCHAR(16) NOT NULL DEFAULT '',
    points      JSONB NOT NULL,
    quality     VARCHAR(16) NOT NULL DEFAULT 'normal'
                CHECK (quality IN ('normal','outlier','suspect','invalid')),
    source      VARCHAR(16) NOT NULL DEFAULT 'import'
                CHECK (source IN ('manual','instrument','import','agent','backfill')),
    notes       TEXT NOT NULL DEFAULT '',
    is_void     BOOLEAN NOT NULL DEFAULT false,
    voided_at   TIMESTAMPTZ,
    voided_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    void_reason TEXT,
    measured_at TIMESTAMPTZ,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_curves_project ON test_data_curves(project_id, created_at DESC);
CREATE INDEX idx_curves_run ON test_data_curves(run_id) WHERE run_id IS NOT NULL;
