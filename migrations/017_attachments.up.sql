CREATE TABLE attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    storage_key TEXT NOT NULL UNIQUE,
    original_name VARCHAR(256),
    sha256 VARCHAR(64) UNIQUE NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    mime_type VARCHAR(64),
    file_size BIGINT NOT NULL CHECK (file_size >= 0),
    uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE attachment_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    attachment_id UUID NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    entity_type VARCHAR(32) NOT NULL
        CHECK (entity_type IN ('assembly_step','daily_report','issue','log','test_data','experiment_run','rf_matching_record')),
    entity_id UUID NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (attachment_id, entity_type, entity_id)
);

CREATE INDEX idx_links_entity ON attachment_links(entity_type, entity_id);
CREATE INDEX idx_links_attachment ON attachment_links(attachment_id);
CREATE INDEX idx_attachments_deleted ON attachments(deleted_at) WHERE deleted_at IS NOT NULL;
