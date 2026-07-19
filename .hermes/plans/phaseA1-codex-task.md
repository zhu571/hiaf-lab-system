# Phase A1：Go 后端安全与队列基础

> Codex 任务文档。按顺序完成，每完成一步验证 go build + go test 通过。

## 项目上下文

- 仓库: `/home/zhuhaofan/hiaf-lab-system`
- 分支: `develop`
- 工作区有未提交改动（A-1 的结果）
- `go test ./...` 和 `go vet ./...` 当前通过

---

## A-1.5：补 PostgreSQL 实测

当前 migrations 011-013 只在 CI 跑过。请在真实 PG 上跑 up/down（用 docker compose 里的 postgres）。

```bash
cd /home/zhuhaofan/hiaf-lab-system
docker compose up -d postgres  # 如果没跑
# 跑 migrate up 011-013，然后 down 013-011，确认无报错
```

---

## A-2：migration 014_agent_runtime_hardening

**不要修改 011-013**。新建 `migrations/014_agent_runtime_hardening.up.sql` 和 `.down.sql`：

### 014 up

```sql
-- 1. queue 硬化
ALTER TABLE pending_agent_tasks
    ADD COLUMN acting_user_id UUID REFERENCES users(id),
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN next_attempt_at TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD COLUMN result JSONB,
    ADD COLUMN model VARCHAR(64),
    ADD COLUMN prompt_version VARCHAR(32),
    ADD COLUMN agent_confidence DOUBLE PRECISION,
    ADD CONSTRAINT check_pending_status CHECK (status IN ('pending','processing','done','failed','dead')),
    ADD CONSTRAINT unique_report_task UNIQUE(report_id);

-- 2. candidate actions 表
CREATE TABLE agent_candidate_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES pending_agent_tasks(id),
    action_type VARCHAR(32) NOT NULL,  -- create_issue, add_comment, create_experience
    project_id UUID REFERENCES projects(id),
    pool_action_key VARCHAR(256) NOT NULL UNIQUE,  -- task_id + action_type + candidate_index
    payload JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending_review',  -- pending_review/approved/rejected/executed/execution_failed
    agent_confidence DOUBLE PRECISION,
    reviewed_by UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    review_reason TEXT,
    executed_at TIMESTAMPTZ,
    execution_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_candidate_actions_status ON agent_candidate_actions(status, created_at);
CREATE INDEX idx_candidate_actions_task ON agent_candidate_actions(task_id);

-- 3. audit 扩展
ALTER TABLE audit_log
    ADD COLUMN actor_type VARCHAR(16) DEFAULT 'user',  -- user / agent
    ADD COLUMN acting_user_id UUID REFERENCES users(id),
    ADD COLUMN agent_task_id UUID,
    ADD COLUMN idempotency_key VARCHAR(256);

-- 4. 日报提交 outbox trigger
CREATE OR REPLACE FUNCTION trg_submit_enqueue_agent_task()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'submitted' AND OLD.status != 'submitted' THEN
        INSERT INTO pending_agent_tasks(report_id, acting_user_id)
        VALUES (NEW.id, NEW.author_id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_submit_enqueue_agent
    AFTER UPDATE ON daily_reports
    FOR EACH ROW EXECUTE FUNCTION trg_submit_enqueue_agent_task();
```

### 014 down

删除新增列/约束/表/trigger，恢复原状。

---

## A-3：Agent 账号、acting-user middleware、权限交集

### 建 `go-server/agent/` 模块

```
go-server/agent/
├── model.go       # PendingAgentTask, AgentCandidateAction struct
├── repository.go  # DB 操作
├── service.go     # 业务逻辑
└── handler.go     # HTTP handler
```

### acting-user 中间件

在 `go-server/middleware/` 中新增或修改：

1. **Agent 请求验证**：如果 JWT role=agent，验证 `X-Acting-User-ID` 和 `X-Agent-Task-ID` 存在且 task 属于该 acting user 且 status=processing
2. **权限交集**：Agent 业务操作使用 acting_user 的权限做现有项目 ACL 检查
3. **审计**：`actor_type=agent`、`actor_id`=Agent 账号、`acting_user_id`、`agent_task_id`、`idempotency_key`

### Agent 账号 seed

创建一次性 seed 脚本或在 migration 中添加 `agent@system` 用户（角色 agent），密码从环境变量读取。

---

## A-4：queue claim/complete/fail API

在 `go-server/main.go` 注册路由：

```
POST /api/v1/agent/tasks/claim
POST /api/v1/agent/tasks/{id}/complete
POST /api/v1/agent/tasks/{id}/fail
```

实现按方案第 5 节的契约：
- claim: `FOR UPDATE SKIP LOCKED`，设 processing + lease
- complete: 验证当前 agent + lease 有效 + 同事务落候选
- fail: 记录错误 + attempts++ + dead 检测

---

## A-5：日报提交 outbox + `GET /daily-reports/{id}`

- 014 的 trigger 已处理同事务入队
- 新增 `GET /api/v1/daily-reports/{id}` → 返回日报及关联日志

---

## A-6：AI 字段严格校验 + `/experiences/candidates`

1. `go-server/issues/handler.go`：PATCH 请求若携带 `ai_generated` 或 `agent_task_id` → 返回 400
2. `go-server/experiences/handler.go`：同上
3. Issue/Experience service：Agent 创建时验证 `agent_task_id` 存在且状态正确
4. 新增 `POST /api/v1/experiences/candidates`，复用现有 create candidate 逻辑

---

## A-7：candidate 审核

```
GET    /api/v1/agent/candidates             # 审核列表（分页/按状态筛选）
POST   /api/v1/agent/candidates/{id}/approve # 批准 → 执行业务 API → executed
POST   /api/v1/agent/candidates/{id}/reject  # 拒绝 → 保存理由
```

- 批准后调用现有业务 API（Issues/Create、Issues/AddComment、Experiences/Create）
- 幂等执行：`pool_action_key` 保证不重复
- 写审批审计

---

## 全局约束

- 不要改已完成的 011-013 migration
- 不引入新的第三方依赖
- 每个 A 步骤完成后：`go build ./... && go test ./...`
- 如果任务太大，可以分批跑，但保持顺序
