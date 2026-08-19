CREATE TABLE content_translations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type VARCHAR(32) NOT NULL,
    entity_id UUID NOT NULL,
    field_name VARCHAR(32) NOT NULL,
    target_locale VARCHAR(2) NOT NULL CHECK (target_locale IN ('zh', 'en')),
    source_locale VARCHAR(8) NOT NULL CHECK (source_locale IN ('zh', 'en', 'mixed', 'und')),
    source_hash CHAR(64) NOT NULL,
    translated_text TEXT,
    status VARCHAR(16) NOT NULL CHECK (status IN ('pending', 'processing', 'ready', 'failed', 'stale')),
    origin VARCHAR(16) NOT NULL DEFAULT 'ai' CHECK (origin IN ('ai', 'manual')),
    model VARCHAR(128),
    prompt_version VARCHAR(32),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until TIMESTAMPTZ,
    error_code VARCHAR(64),
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entity_type, entity_id, field_name, target_locale)
);

CREATE INDEX idx_content_translations_queue
    ON content_translations(status, next_attempt_at)
    WHERE status IN ('pending', 'processing');
