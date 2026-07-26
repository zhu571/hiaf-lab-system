CREATE TABLE step_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(256) NOT NULL,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('assembly','experiment')),
    description TEXT,
    source_prompt TEXT,
    ai_generated BOOLEAN NOT NULL DEFAULT false,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE step_template_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES step_templates(id) ON DELETE CASCADE,
    name VARCHAR(256) NOT NULL,
    description TEXT,
    step_order INTEGER NOT NULL,
    depends_on_order INTEGER,
    meta JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_template_item_order ON step_template_items(template_id, step_order);
CREATE INDEX idx_templates_kind ON step_templates(kind) WHERE deleted_at IS NULL;

ALTER TABLE assembly_steps ADD COLUMN source_template_id UUID REFERENCES step_templates(id) ON DELETE SET NULL;
