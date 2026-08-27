ALTER TABLE instrument_leases
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE instrument_leases SET status = 'expired', updated_at = now()
WHERE status = 'active' AND expires_at <= now();

WITH duplicate_active AS (
    SELECT id, row_number() OVER (PARTITION BY instrument_id ORDER BY created_at DESC, id DESC) AS rn
    FROM instrument_leases WHERE status = 'active'
)
UPDATE instrument_leases SET status = 'revoked', revoked_at = now(), updated_at = now()
WHERE id IN (SELECT id FROM duplicate_active WHERE rn > 1);

CREATE UNIQUE INDEX instrument_leases_one_active
    ON instrument_leases (instrument_id) WHERE status = 'active';

ALTER TABLE instrument_approvals
    ALTER COLUMN approved_by DROP NOT NULL,
    ADD COLUMN acting_user_id UUID REFERENCES users(id),
    ADD COLUMN envelope JSONB,
    ADD COLUMN envelope_hash TEXT;

CREATE TABLE instrument_flow_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id TEXT NOT NULL,
    flow_kind TEXT NOT NULL,
    objective TEXT NOT NULL,
    object_type TEXT NOT NULL,
    status TEXT NOT NULL,
    limits JSONB NOT NULL,
    frequency_grid JSONB NOT NULL,
    lease_id UUID NOT NULL REFERENCES instrument_leases(id),
    approval_id UUID REFERENCES instrument_approvals(id),
    whitelist_version TEXT NOT NULL,
    actor_id UUID NOT NULL REFERENCES users(id),
    acting_user_id UUID NOT NULL REFERENCES users(id),
    request_id TEXT NOT NULL,
    step_count INTEGER NOT NULL DEFAULT 0,
    point_count INTEGER NOT NULL DEFAULT 0,
    stop_requested BOOLEAN NOT NULL DEFAULT false,
    error_code TEXT,
    result JSONB,
    deadline_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE instrument_approvals
    ADD COLUMN flow_session_id UUID REFERENCES instrument_flow_sessions(id);

CREATE TABLE instrument_flow_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES instrument_flow_sessions(id),
    step_no INTEGER NOT NULL,
    decision TEXT NOT NULL,
    command_name TEXT,
    params JSONB,
    status TEXT NOT NULL,
    reason TEXT,
    result JSONB,
    error_code TEXT,
    input_hash TEXT,
    output_hash TEXT,
    model TEXT,
    prompt_version TEXT,
    whitelist_version TEXT NOT NULL,
    duration_ms INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, step_no)
);

ALTER TABLE command_log
    ADD COLUMN flow_session_id UUID REFERENCES instrument_flow_sessions(id),
    ADD COLUMN step_no INTEGER,
    ADD COLUMN phase TEXT NOT NULL DEFAULT 'completed',
    ADD COLUMN result_hash TEXT;

CREATE INDEX instrument_flow_sessions_instrument_status
    ON instrument_flow_sessions (instrument_id, status);
CREATE INDEX instrument_flow_steps_session_step
    ON instrument_flow_steps (session_id, step_no);
CREATE INDEX command_log_flow_step
    ON command_log (flow_session_id, step_no);
