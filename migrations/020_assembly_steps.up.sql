CREATE TABLE assembly_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    name VARCHAR(256) NOT NULL,
    description TEXT,
    depends_on UUID,
    status VARCHAR(16) NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned','in_progress','paused','completed','skipped','cancelled')),
    assigned_to UUID REFERENCES users(id) ON DELETE SET NULL,
    step_order INTEGER NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_assembly_project_order
    ON assembly_steps(project_id, step_order) WHERE deleted_at IS NULL;
CREATE INDEX idx_assembly_project ON assembly_steps(project_id, step_order);
