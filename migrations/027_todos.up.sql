-- Todolist 系统：个人/共享待办、issue 自动聚合、LLM 生成（方案 v13 §7）。
-- created_for = 最终归属日：顺延/推迟直接改写该字段，原计划日不保留（顺延链不可追溯，已接受）。
CREATE TABLE todos (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title        VARCHAR(256) NOT NULL CHECK (length(trim(title)) > 0),
    priority     VARCHAR(8)   NOT NULL DEFAULT 'medium'
                 CHECK (priority IN ('high','medium','low')),
    status       VARCHAR(16)  NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','done','deferred','cancelled')),
    source       VARCHAR(16)  NOT NULL DEFAULT 'manual'
                 CHECK (source IN ('manual','llm','issue','daily_llm')),
    created_by   UUID NOT NULL REFERENCES users(id),
    created_for  DATE NOT NULL,
    project_id   UUID REFERENCES projects(id) ON DELETE SET NULL,
    issue_id     UUID REFERENCES issues(id) ON DELETE SET NULL,
    completed_at TIMESTAMPTZ,
    completed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- issue 在途去重：同一 issue 只有一条在途待办（issue 被删除 SET NULL 后原约束不再命中，可重新生成——边缘情况接受）。
CREATE UNIQUE INDEX uq_todos_issue_inflight ON todos(issue_id) WHERE status IN ('pending','deferred') AND issue_id IS NOT NULL;

-- 用户今日/历史查询
CREATE INDEX idx_todos_user_day ON todos(created_by, created_for);
-- 共享查询
CREATE INDEX idx_todos_project_day ON todos(project_id, created_for) WHERE project_id IS NOT NULL;
-- 历史清理（普通索引：partial 谓词不能用 CURRENT_DATE——非 immutable）
CREATE INDEX idx_todos_cleanup ON todos(created_for);
