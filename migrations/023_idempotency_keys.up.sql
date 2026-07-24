CREATE TABLE IF NOT EXISTS idempotency_keys (
    idempotency_key VARCHAR(256) NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    response_status INTEGER NOT NULL DEFAULT 200,
    response_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (idempotency_key)
);

CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys(created_at);
