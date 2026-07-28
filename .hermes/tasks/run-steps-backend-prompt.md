实现实验步骤功能后端。根据以下决策实现：

## 决策
1. 步骤状态与装配一致：planned/in_progress/paused/completed/skipped/cancelled
2. 步骤依赖（depends_on）只存不强制
3. AI 生成时传 run_type/gas_type/devices 作为 context
4. 模板应用到实验时追加到末尾（续排 step_order）
5. 顺手修：assembly.ApplyTemplate 加 kind 校验（拒绝 experiment 模板）

## 改动清单

### 1. 迁移 025
migrations/025_run_steps.up.sql：
```sql
CREATE TABLE run_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES experiment_runs(id) ON DELETE RESTRICT,
    name VARCHAR(256) NOT NULL,
    description TEXT,
    depends_on UUID,
    status VARCHAR(16) NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned','in_progress','paused','completed','skipped','cancelled')),
    step_order INTEGER NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    source_template_id UUID REFERENCES step_templates(id) ON DELETE SET NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_run_steps_order ON run_steps(run_id, step_order) WHERE deleted_at IS NULL;
CREATE INDEX idx_run_steps_run ON run_steps(run_id) WHERE deleted_at IS NULL;
```

025_run_steps.down.sql：DROP TABLE IF EXISTS run_steps;

### 2. runs/model.go
新增类型（粘贴下文开头即可，不删现有代码）：
- RunStep struct
- CreateStepRequest
- UpdateStepRequest（含 Transition 字段）
- ApplyTemplateRequest（与 assembly 一致：template_id 与 steps 互斥）
- StepDef（内联步骤定义）
- ReorderRequest / ReorderItem
- StepListResult（items + total）
- 状态常量 + StepAllowedTransitions map（从 assembly 复制）

### 3. runs/repository.go
新增方法（不改现有方法，只追加）：
- ListSteps(runID) → 按 step_order ASC
- CreateStep / CreateStepsMany（批量插入）
- GetStepByID
- UpdateStep / UpdateStepStatus
- SoftDeleteStep
- Reorder(runID, items)
- MaxStepOrder(runID) → max step_order
- SetSourceTemplateID(stepID, tmplID)
- 改 SoftDelete(runID)：事务内级联软删 run_steps

### 4. runs/service.go
新增方法 + 修改：
- TemplateReader 窄接口（与 assembly 同构）
- ConfigureTemplates(tmplReader)
- ListSteps / CreateStep / UpdateStep / DeleteStep / ReorderSteps
- ApplyTemplate：校验模板 kind='experiment' → MaxStepOrder 续排 → 批量 CreateStepsMany → 设 source_template_id
- Step 状态转移逻辑（与 assembly transition 一致）

### 5. runs/handler.go
新增 6 个 handler：
- HandleListSteps
- HandleCreateStep  
- HandleApplyTemplate
- HandleUpdateStep（含状态转移）
- HandleDeleteStep
- HandleReorderSteps
错误补充：ErrStepNotFound → 404

### 6. assembly/service.go
在 ApplyTemplate 开头加 kind 校验：
```go
if tmpl.Kind != "assembly" {
    return nil, common.NewError("bad_request", "模板类型不匹配，期望 assembly", nil)
}
```

### 7. main.go
- 在已有 `/api/v1/experiment-runs/{id}` 路由组内挂：
  - GET /steps
  - POST /steps
  - POST /steps/apply-template
- 新建 `/api/v1/run-steps` 组：
  - PATCH /{id}
  - DELETE /{id}
  - POST /reorder
- templateReaderBridge 加 runs 版本，注入 runsSvc.ConfigureTemplates

### 验证
go build ./... && go test ./... && go vet ./...
