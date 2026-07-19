CREATE TABLE instrument_leases (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id TEXT NOT NULL,
    user_id       UUID NOT NULL REFERENCES users(id),
    purpose       TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at    TIMESTAMPTZ,
    revoked_by    UUID REFERENCES users(id)
);

CREATE TABLE instrument_approvals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_id     UUID REFERENCES instrument_leases(id),
    command_name TEXT NOT NULL,
    params_hash  TEXT NOT NULL,
    requested_by UUID NOT NULL REFERENCES users(id),
    approved_by  UUID NOT NULL REFERENCES users(id),
    status       TEXT NOT NULL DEFAULT 'pending',
    approved_at  TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE command_log (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id     TEXT NOT NULL,
    command_name      TEXT NOT NULL,
    risk_level        TEXT NOT NULL,
    params_raw        JSONB,
    params_normalized JSONB,
    user_id           UUID NOT NULL REFERENCES users(id),
    acting_user_id    UUID,
    lease_id          UUID REFERENCES instrument_leases(id),
    approval_id       UUID REFERENCES instrument_approvals(id),
    whitelist_version TEXT NOT NULL,
    before_snapshot   JSONB,
    result_summary    TEXT,
    error_code        TEXT,
    duration_ms       INTEGER,
    request_id        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
