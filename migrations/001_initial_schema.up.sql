CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username        VARCHAR(64)  NOT NULL UNIQUE,
    password_hash   VARCHAR(256) NOT NULL,
    display_name    VARCHAR(128) NOT NULL DEFAULT '',
    role            VARCHAR(32)  NOT NULL DEFAULT 'member'
                    CHECK (role IN ('admin', 'member', 'viewer', 'agent')),
    must_change_pw  BOOLEAN      NOT NULL DEFAULT TRUE,
    failed_attempts INTEGER      NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_users_username ON users (username);

CREATE TABLE refresh_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      VARCHAR(128) NOT NULL UNIQUE,
    family          UUID         NOT NULL DEFAULT gen_random_uuid(),
    expires_at      TIMESTAMPTZ  NOT NULL,
    revoked         BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens (user_id);
CREATE INDEX idx_refresh_tokens_token_hash ON refresh_tokens (token_hash);

CREATE TABLE audit_log (
    id              BIGSERIAL PRIMARY KEY,
    request_id      TEXT         NOT NULL,
    user_id         UUID         REFERENCES users(id),
    username        VARCHAR(64),
    method          VARCHAR(10)  NOT NULL,
    path            VARCHAR(256) NOT NULL,
    action          VARCHAR(64)  NOT NULL,
    status_code     INTEGER      NOT NULL,
    client_ip       VARCHAR(64),
    detail          JSONB,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_request_id ON audit_log (request_id);
CREATE INDEX idx_audit_log_user_id ON audit_log (user_id);
CREATE INDEX idx_audit_log_created_at ON audit_log (created_at);
