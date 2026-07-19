# Go 后端 A-1: ai_generated + agent_task_id + pending_agent_tasks

> 给 Codex CLI (gpt-5.6-sol) 的任务文档。

## 项目

- 仓库: `/home/zhuhaofan/hiaf-lab-system`
- 分支: `develop`
- Go module: `github.com/zhu571/hiaf-lab-system`

## 任务：添加 3 个 migration + 改 Issue/Experience 模型 + service 层校验

### 1. Migration 011: issues 表加字段

文件: `migrations/011_issues_ai_generated.up.sql` + `.down.sql`

```sql
ALTER TABLE issues ADD COLUMN ai_generated BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE issues ADD COLUMN agent_task_id VARCHAR(64);
```

down: 删除这两列。

### 2. Migration 012: experiences 表加字段

同上，`migrations/012_experiences_ai_generated.up.sql` + `.down.sql`

```sql
ALTER TABLE experiences ADD COLUMN ai_generated BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE experiences ADD COLUMN agent_task_id VARCHAR(64);
```

### 3. Migration 013: pending_agent_tasks 表

```sql
CREATE TABLE pending_agent_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id UUID NOT NULL REFERENCES daily_reports(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',  -- pending/processing/done/failed/dead
    attempts INTEGER NOT NULL DEFAULT 0,
    claimed_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_pending_agent_tasks_status ON pending_agent_tasks(status, created_at);
```

### 4. Go 模型改动

- `issues/model.go`:
  - `Issue` struct 加 `AiGenerated bool` + `AgentTaskID *string`
  - `CreateIssueRequest` 加 `AiGenerated bool` + `AgentTaskID *string`

- `experiences/model.go`:
  - `Experience` struct 加 `AiGenerated bool` + `AgentTaskID *string`
  - `CreateExperienceRequest` 加 `AiGenerated bool` + `AgentTaskID *string`

### 5. Service 层校验

在 `issues/service.go` 的 `Create` 方法和 `experiences/service.go` 的 `Create` 方法中：

- 如果 `req.AiGenerated == true` 且调用者角色不是 `agent` → 返回 400
- 检查调用者的 JWT claims 中的 `role` 字段（需要从 context 获取）

### 6. 约束

- `ai_generated` 字段创建后不可变：PATCH/Update 接口不处理这个字段（不改动）
- `agent_task_id` 创建后不可变：同上
- 尽量小改动，不影响现有测试

### 7. 验证

- `migrations/*.up.sql` 语法正确（PostgreSQL 17+）
- `migrations/*.down.sql` 正确回滚
- `go build ./...` 通过
- `go test ./go-server/issues/... ./go-server/experiences/...` 通过（现有测试未破坏）

## 运行

```bash
cd /home/zhuhaofan/hiaf-lab-system
# Codex 改完之后运行验证:
go build ./...
go test ./go-server/issues/... ./go-server/experiences/...
```
