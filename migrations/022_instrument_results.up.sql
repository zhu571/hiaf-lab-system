CREATE TABLE instrument_results (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id   TEXT NOT NULL,
    command_name    TEXT NOT NULL,
    scpi            TEXT NOT NULL DEFAULT '',
    raw_response    TEXT NOT NULL DEFAULT '',
    parsed_value    DOUBLE PRECISION,
    parsed_points   JSONB,
    plot_type       TEXT,
    error_code      TEXT,
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    user_id         UUID NOT NULL REFERENCES users(id),
    request_id      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_instrument_results_instrument_id ON instrument_results(instrument_id);
CREATE INDEX idx_instrument_results_created_at ON instrument_results(created_at DESC);
