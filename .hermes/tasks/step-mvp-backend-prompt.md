实现步骤模板 MVP 后端。根据设计方案 .hermes/plans/step-templates-plan.md 实现。

## 改动清单

### 1. 迁移文件
新建 migrations/024_step_templates.up.sql 和 .down.sql：

```sql
-- up
CREATE TABLE step_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(256) NOT NULL,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('assembly','experiment')),
    description TEXT,
    source_prompt TEXT,
    ai_generated BOOLEAN NOT NULL DEFAULT false,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE step_template_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES step_templates(id) ON DELETE CASCADE,
    name VARCHAR(256) NOT NULL,
    description TEXT,
    step_order INTEGER NOT NULL,
    depends_on_order INTEGER,
    meta JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX uq_template_item_order ON step_template_items(template_id, step_order);
CREATE INDEX idx_templates_kind ON step_templates(kind) WHERE deleted_at IS NULL;
```

### 2. 新建 go-server/steptemplates/ 模块
按标准四件套（model.go, repository.go, service.go, handler.go + tests）：

**model.go**：StepTemplate, StepTemplateItem, GenerateRequest/Response 类型
- step_order > 0 且不重复，服务端重排为 1..N
- depends_on_order < step_order

**repository.go**：CRUD for step_templates + step_template_items
- Create: 事务内插入主表 + items
- Get: 含 items
- List: 分页 + kind 筛选
- Update: 更新 name/description
- ReplaceItems: 事务内软删旧 items + 插入新 items
- SoftDelete: 设置 deleted_at
- GetTemplateWithItems: 读取模板 + items（给 assembly 适配器用）

**service.go**：
- Generate(kind, prompt, projectID?): 调 py-agent /v1/step-plan，二次校验，返回候选
- Create/Get/List/Update/ReplaceItems/Delete: 代理 repository
- 限流：generate 每用户 10次/分钟（参考 instruments handler.go allowNL 实现）
- HasAnyProjectRole(userID): 检查用户是否属于任何项目的 member+

**handler.go**：
- POST /step-templates/generate - Generate handler
- GET/POST /step-templates - List/Create
- GET/PATCH/DELETE /step-templates/{id} - Get/Update/SoftDelete
- PATCH /step-templates/{id}/items - ReplaceItems
- 所有写操作要求 Idempotency-Key
- 权限：viewer 可读；maintainer/admin + 创建者本人可写；agent 拒绝

### 3. 修改 go-server/main.go
- 注册 steptemplates 路由
- 往 assembly 模块注入 TemplateReaderAdapter

### 4. 修改 go-server/assembly/service.go
- 新增 ApplyTemplate(templateID string, projectID string, userID string) 或 ApplySteps(steps []StepDef, projectID string, userID string)
- TemplateReader 接口 + ConfigureTemplates 注入
- 支持 template_id 和 inline steps 两种模式（互斥）
- 单事务：读取/解析步骤 → 续排 order → 建立依赖映射 → 批量插入
- 幂等：Idempotency-Key 重放

### 5. py-agent
- serve.py 加路由 /v1/step-plan
- 新建 py-agent/tools/stepplan.py: StepPlanner 类
- 新建 py-agent/prompts/step_plan.txt: prompt（规则 4a 已包含）

## 文件位置
- 新建 migrations/024_step_templates.up.sql / .down.sql
- 新建 go-server/steptemplates/{model,repository,service,handler,handler_test}.go
- 修改 go-server/main.go
- 修改 go-server/assembly/{service.go,handler.go,repository.go}
- 新建 py-agent/tools/stepplan.py
- 新建 py-agent/prompts/step_plan.txt
- 修改 py-agent/serve.py

## 验证
```bash
cd /home/zhuhaofan/hiaf-lab-system/go-server && go build ./... && go test ./...
```
