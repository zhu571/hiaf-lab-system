-- 052: 多人助手会话层（assistant 模块自有表）。
-- 只新增表；会话/消息 API 与运行时在后续迁移/PR 接入。
-- 跨模块只做 FK，不 join 业务表；会话删除走 status=archived，审计链不删。

CREATE TABLE assistant_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    project_id      UUID REFERENCES projects(id),
    title           VARCHAR(200) NOT NULL DEFAULT '',
    status          VARCHAR(16) NOT NULL DEFAULT 'active',
    summary         TEXT NOT NULL DEFAULT '',
    summary_upto    UUID,
    last_message_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assistant_sessions_user ON assistant_sessions(user_id, last_message_at DESC);
CREATE INDEX idx_assistant_sessions_project ON assistant_sessions(project_id, last_message_at DESC);

CREATE TABLE assistant_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES assistant_sessions(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    status          VARCHAR(16) NOT NULL DEFAULT 'running',
    model           VARCHAR(64) NOT NULL DEFAULT '',
    prompt_version  VARCHAR(32) NOT NULL DEFAULT '',
    input_tokens    INT NOT NULL DEFAULT 0,
    output_tokens   INT NOT NULL DEFAULT 0,
    error           TEXT,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ,
    request_id      VARCHAR(64) NOT NULL DEFAULT ''
);
CREATE INDEX idx_assistant_runs_session ON assistant_runs(session_id, started_at DESC);

CREATE TABLE assistant_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES assistant_sessions(id) ON DELETE CASCADE,
    run_id          UUID REFERENCES assistant_runs(id),
    role            VARCHAR(16) NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    content_json    JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assistant_messages_session ON assistant_messages(session_id, created_at);

CREATE TABLE assistant_tool_calls (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES assistant_runs(id) ON DELETE CASCADE,
    seq             INT NOT NULL,
    tool_name       VARCHAR(64) NOT NULL,
    params          JSONB NOT NULL DEFAULT '{}',
    result_digest   CHAR(64),
    status          VARCHAR(16) NOT NULL,
    duration_ms     INT NOT NULL DEFAULT 0,
    audit_request_id VARCHAR(64) NOT NULL DEFAULT '',
    candidate_id    UUID REFERENCES agent_candidate_actions(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, seq)
);

CREATE TABLE assistant_usage (
    user_id     UUID NOT NULL REFERENCES users(id),
    usage_date  DATE NOT NULL,
    runs        INT NOT NULL DEFAULT 0,
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    tool_calls  INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, usage_date)
);
