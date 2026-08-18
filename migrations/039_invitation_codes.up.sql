CREATE TABLE invitation_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), code_hash TEXT NOT NULL,
    code_prefix TEXT NOT NULL, created_by UUID NOT NULL REFERENCES users(id),
    used_by UUID REFERENCES users(id), expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ, revoked_at TIMESTAMPTZ, revoked_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT invitation_codes_hash_unique UNIQUE (code_hash),
    CONSTRAINT invitation_codes_use_pair CHECK ((used_at IS NULL AND used_by IS NULL) OR (used_at IS NOT NULL AND used_by IS NOT NULL)),
    CONSTRAINT invitation_codes_revoke_pair CHECK ((revoked_at IS NULL AND revoked_by IS NULL) OR (revoked_at IS NOT NULL AND revoked_by IS NOT NULL)),
    CONSTRAINT invitation_codes_terminal_once CHECK (NOT (used_at IS NOT NULL AND revoked_at IS NOT NULL)),
    CONSTRAINT invitation_codes_expiry_after_create CHECK (expires_at > created_at)
);
CREATE INDEX idx_invitation_codes_created_at ON invitation_codes (created_at DESC);
CREATE INDEX idx_invitation_codes_expires_at ON invitation_codes (expires_at) WHERE used_at IS NULL AND revoked_at IS NULL;
