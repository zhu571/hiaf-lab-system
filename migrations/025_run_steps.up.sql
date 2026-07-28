CREATE TABLE run_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES experiment_runs(id) ON DELETE RESTRICT,
    name VARCHAR(256) NOT NULL,
    description TEXT,
    depends_on UUID,
    status VARCHAR(16) NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned','in_progress','paused','completed','skipped','cancelled')),
    step_order INTEGER NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    source_template_id UUID REFERENCES step_templates(id) ON DELETE SET NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_run_steps_order ON run_steps(run_id, step_order) WHERE deleted_at IS NULL;
CREATE INDEX idx_run_steps_run ON run_steps(run_id) WHERE deleted_at IS NULL;
