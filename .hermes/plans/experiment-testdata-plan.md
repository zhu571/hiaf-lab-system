# 实验步骤 AI 助手 + 仪器测量保存测试数据 — 设计方案

> 两个独立但都不大的功能，共用本方案。
>
> 设计基准：2026-07-28 仓库现状（迁移最新为 024）。本方案是 `.hermes/plans/step-templates-plan.md` Phase 2 的落地细化 + 仪器结果入库的前端补全。全程复用既有模式，不引入新架构。

## 0. 现状确认（代码证据）

| 能力 | 现状 |
|------|------|
| 模板表 / 模板 items 表 | 已有，迁移 024；`kind CHECK (kind IN ('assembly','experiment'))`，experiment kind **数据库层已支持** |
| AI 生成端点 | `POST /api/v1/step-templates/generate` 已实现，`go-server/steptemplates/service.go:96` 已接受 `kind=experiment`；py-agent `/v1/step-plan` + `prompts/step_plan.txt` 规则 6 已覆盖 experiment（准备/抽真空→充气→降温→稳定→测量→恢复） |
| 模板 CRUD | 已实现，创建/筛选均接受 experiment kind（`validateCreateRequest`，service.go:338） |
| 模板应用 | 仅 assembly：`POST /api/v1/projects/{pid}/assembly/apply-template`（assembly/service.go:336）。**注意：assembly 端不校验模板 kind**，experiment 模板目前也能被应用到装配 |
| run 步骤 | 完全没有：`experiment_runs` 无步骤概念，runs 模块只有 run CRUD + 状态机 + 日报关联 |
| 测试数据后端 | 完整可用：`POST /api/v1/projects/{pid}/test-data`，`source` 枚举含 `instrument`（testdata/service.go:253-258）；写接口自动过 `Idempotency-Key`、审计 `test_data.create` |
| 测试数据前端 | `TestDataView.vue` 录入/列表/趋势图齐全；`web-ui/src/api/testdata.ts` 的 `createTestData` 现成 |
| 仪器执行结果 | `InstrumentMeasureView.vue` 执行后仅存 `cmdResult`（command/response/duration，见 api/instruments.ts:39），无入库入口；`instrument_results` 表（迁移 022）由后端 NL 链路写，与手工执行面板无关联 |
| 步骤编辑器组件 | `StepItemsEditor.vue` 已抽取为通用组件（v-model: StepTemplateItem[]，含增删/上下移/依赖选择），RunDetailView 可直接复用 |
| 前端幂等 | `client.ts:43` 对所有写请求自动注入 `Idempotency-Key`，新调用零成本 |

**结论：功能一的缺口 = run_steps 表 + runs 模块步骤端点 + RunDetailView UI；功能二纯前端，后端零改动。**

## 1. 功能一：实验步骤 AI 助手

### 1.1 数据模型变更 — 迁移 025（唯一新增迁移）

`migrations/025_run_steps.up.sql`，镜像 `assembly_steps`（迁移 020）并把 024 的可选回溯列直接带上：

```sql
CREATE TABLE run_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES experiment_runs(id) ON DELETE RESTRICT,
    name VARCHAR(256) NOT NULL,
    description TEXT,
    depends_on UUID,                   -- 同 run 内另一步骤 id，不加 FK（与 assembly_steps 一致）
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

CREATE UNIQUE INDEX uq_run_steps_order
    ON run_steps(run_id, step_order) WHERE deleted_at IS NULL;
CREATE INDEX idx_run_steps_run ON run_steps(run_id) WHERE deleted_at IS NULL;
```

`025_run_steps.down.sql`：`DROP TABLE IF EXISTS run_steps;`

说明：

- `experiment_runs` 本体不动；run 的创建仍由用户手动完成，AI/模板只生成其下的步骤（step-templates-plan §7 已否决 AI 代填 run 安全参数）。
- 软删除级联：run 软删时同事务软删其 `run_steps`（改 `runs.Repository.SoftDelete`，单条 UPDATE 改为事务内两条 UPDATE）。不建 DB 级触发器，保持在 repository 层显式处理。
- 只追加迁移，不改 024 及以前（仓库铁律）。

### 1.2 API 设计

**新增端点**（全部挂在既有路由组，复用同一中间件链 AuthRequired → AgentContext → Audit → RequireIdempotencyKey）：

```
GET    /api/v1/experiment-runs/{id}/steps                  # 步骤列表（按 step_order 升序，含软删过滤）
POST   /api/v1/experiment-runs/{id}/steps                  # 手动新建步骤
POST   /api/v1/experiment-runs/{id}/steps/apply-template   # 应用模板或内联 steps
PATCH  /api/v1/run-steps/{id}                              # 改元数据 或 transition（与 assembly 同款互斥）
DELETE /api/v1/run-steps/{id}                              # 软删
POST   /api/v1/run-steps/reorder                           # 整体重排 {run_id, steps:[{id, step_order}]}
```

apply-template 请求体与 assembly 完全一致（`template_id` 与 `steps` 二选一互斥；内联模式带 `source_prompt` 溯源）：

```json
{ "template_id": "uuid" }
// 或
{ "steps": [{ "name": "抽真空", "description": "...", "step_order": 1, "depends_on_order": null }],
  "source_prompt": "原始自然语言" }
```

响应：新建步骤完整列表（201）。行为镜像 `assembly.Service.ApplyTemplate`（service.go:336-403）：校验 → `MaxStepOrder(runID)` 续排 → `depends_on_order` 映射为真实 UUID → 批量插入（status=planned）→ 模板模式回写 `source_template_id`。**与 assembly 的差异：模板模式必须校验 `tmpl.Kind == "experiment"`，否则 400 `bad_request`。**

**复用端点（零改动）**：

```
POST /api/v1/step-templates/generate        # kind=experiment 已端到端支持
POST /api/v1/step-templates                 # kind=experiment 已支持（存模板 / 存并应用）
GET  /api/v1/step-templates?kind=experiment # 模板库筛选
```

generate 请求已支持 `context` 透传（steptemplates service.go:107）；前端在 RunDetailView 调 generate 时把当前 run 的 `{run_type, gas_type, devices}` 放进 context，让 prompt 规则 6 输出更贴合。**Go/py-agent 均无需改。**

**权限矩阵**（与 assembly 步骤对齐）：

| 动作 | viewer | member | maintainer/admin | agent |
|------|:--:|:--:|:--:|:--:|
| 看步骤 | ✅（项目 viewer+） | ✅ | ✅ | ❌ |
| 新建/改/删/排序/状态转移 | ❌ | ✅（项目 member+） | ✅ | ❌ |
| apply-template | ❌ | ✅（项目 member+，agent 显式拒绝） | ✅ | ❌ |

鉴权走 runs 既有 `getAccessible(id, userID, userRole, minRole, creatorAllowed)`（runs/service.go:180）+ `ProjectAccessAdapter`，不加新机制。

**审计动作**：`run_step.created` / `run_step.updated` / `run_step.deleted` / `run_step.reordered` / `run.template_applied`（经 `middleware.SetAuditAction`，与 assembly 命名风格一致）。

### 1.3 后端改动点（runs 模块内，镜像 assembly）

| 文件 | 改动 |
|------|------|
| `runs/model.go` | 加 `RunStep`、`CreateStepRequest`、`UpdateStepRequest`（含 `Transition`）、`ReorderRequest/Item`、`ApplyTemplateRequest`、`StepDef`、`StepListResult`；步骤状态常量 + `StepAllowedTransitions` map（**本地复制** assembly 的 6 状态/7 transition，不 import assembly，避免模块耦合） |
| `runs/repository.go` | 加步骤 CRUD：`CreateStep` / `GetStepByID` / `ListSteps(runID)` / `UpdateStep` / `UpdateStepStatus` / `SoftDeleteStep` / `Reorder` / `MaxStepOrder(runID)` / `CreateStepsMany` / `SetSourceTemplateID`；改 `SoftDelete` 为事务级联软删 run_steps。只访问 `run_steps` / `experiment_runs`，不碰模板表 |
| `runs/service.go` | 加步骤业务方法 + `ApplyTemplate`；加 `TemplateReader` 窄接口 + `SteptemplatesTemplate/Item` 本地类型（照抄 assembly/service.go:43-59 模式）；状态转移复用 `transitionTarget` 同款实现 |
| `runs/handler.go` | 加 6 个 handler，writeError 增补 `ErrStepNotFound` → 404 `run_step_not_found` |
| `main.go` | `/api/v1/experiment-runs/{id}` 组内挂 3 条 steps 路由；新建 `/api/v1/run-steps` 组挂 3 条；`templateReaderBridge` 增加返回 runs 类型的桥（现桥返回 assembly 类型，main.go:525-553），`runsSvc.ConfigureTemplates(...)` |
| `runs/service_test.go` `handler_test.go` | 新增（见 §4） |

MVP 取舍：

- `depends_on` 只存储和展示，**不做完成强制**（assembly 的依赖拦截 + override_reason 机制留到后续）；状态机转移不做依赖检查。
- 不加 `assigned_to`（run 步骤是指令序列不是任务分派）。

### 1.4 前端改动点

**`web-ui/src/api/runs.ts`**（新增，全部镜像 assembly.ts）：

```ts
export type RunStep = { id, run_id, name, description?, depends_on?, status,
  step_order, started_at?, completed_at?, created_by?, created_at, updated_at }
listRunSteps(runId)                       GET    /experiment-runs/{id}/steps
createRunStep(runId, data)                POST   /experiment-runs/{id}/steps
applyRunTemplate(runId, payload)          POST   /experiment-runs/{id}/steps/apply-template
updateRunStep(id, data)                   PATCH  /run-steps/{id}
transitionRunStep(id, transition)         PATCH  /run-steps/{id} {transition}
deleteRunStep(id)                         DELETE /run-steps/{id}
reorderRunSteps(runId, steps)             POST   /run-steps/reorder
```

**`RunDetailView.vue`**：

- 新增第四个 tab「步骤」（放在「概览」之后），内容为步骤面板：
  - 表格：序号、名称、描述、状态（复用 `StatusBadge`）、依赖步骤名、开始/完成时间；`canEdit` 时行内状态转移按钮（按 `StepAllowedTransitions` 计算可选动作，复用本文件 transitionMap 的写法）+ 删除。
  - 工具行：「AI 生成步骤」「从模板导入」「手动新建」（viewer 隐藏）。
- 「AI 生成步骤」对话框：**照抄 AssemblyView.vue:81-112 的 aiDialog 三态模式**（输入态 → 候选态 → 三按钮），差异仅三处：
  - `generateSteps('experiment', prompt)`（stepTemplates.ts:34 已支持 kind 参数；可选地带 context `{run_type, gas_type, devices}`，需给 generateSteps 加第三个可选参数）；
  - 「直接应用」调 `applyRunTemplate(runId, { steps, source_prompt })`；
  - 「存并应用」= `createTemplate({kind:'experiment', ...})` 后 `applyRunTemplate(runId, { template_id })`。
  - 候选态编辑器直接用现成 `<StepItemsEditor :key="aiKey" v-model="aiItems" />`。
- 「从模板导入」对话框：`listTemplates({kind:'experiment'})` → 选择 → 预览步骤 → `applyRunTemplate(runId, {template_id})`。
- 步骤变更后只需刷新步骤面板，不动现有 `load()` 主流程。

**`StepTemplatesView.vue`**（小改）：现有「应用到项目」固定调 `applyAssemblyTemplate`（:387）。改为按 `applyTarget.kind` 分流：

- `assembly` → 现状不变；
- `experiment` → 对话框加第二级「选择批次」下拉（选定项目后 `listRuns(projectId)`），确认调 `applyRunTemplate(runId, {template_id})`。

**不改**：`StepItemsEditor.vue`、`AssemblyView.vue`、`stepTemplates.ts`（除 generateSteps 加可选 context 参数）、py-agent、steptemplates 后端。

### 1.5 顺手修的小问题（建议带上，可拆单独 PR）

`assembly.Service.ApplyTemplate` 不校验模板 kind，experiment 模板可被应用到装配。加一行 `tmpl.Kind != "assembly"` → 400，与 runs 端对称。Frontend StepTemplatesView 分流后用户不会再踩到，但后端应兜底。

## 2. 功能二：仪器测量保存测试数据

### 2.1 数据模型 / 后端

**零改动。** `POST /api/v1/projects/{pid}/test-data` 已接受全部所需字段，`source='instrument'` 合法，权限（项目 member+）、幂等、审计（`test_data.create`）齐备。

### 2.2 前端改动点（仅 `InstrumentMeasureView.vue`）

关键约束：该页路由是 `/instrument-measure`（router/index.ts:59），**不在项目命名空间下，没有 project_id**；而 test-data 创建是项目路径。所以对话框必须让用户选项目（+可选批次）。

- 在 cmdResult 结果块（InstrumentMeasureView.vue:73-76）的耗时文案旁加「保存到测试数据」按钮：`v-if="cmdResult"`，非 viewer 可见（后端仍强校验 member+，无权限时按 `showApiError` 展示后端 message + request_id）。
- 点击打开 `el-dialog`「保存到测试数据」，字段与 `TestDataView` 录入表单同源：

| 字段 | 预填 | 说明 |
|------|------|------|
| 项目 * | 空 | `listProjects()` 下拉（projects.ts:25），必选 |
| 关联批次 | 空 | 选项目后 `listRuns(projectId, {per_page:100})`，clearable |
| 数据类型 * | 空 | 5 枚举下拉（cryo/pressure/voltage/rf_voltage/efficiency） |
| 测量项 * | `cmdResult.command` | 可改，≤128 |
| 数值 * | 从 `cmdResult.response` 用正则提取第一个浮点数 | 提取不到则留空，必须可手改 |
| 单位 | 空 | ≤16 |
| 测量时间 | 当前时间 | el-date-picker |
| 备注 | `instrument={ins.id} command={cmdResult.command}`，可追加 | |

- 提交：`createTestData(projectId, { data_type, measurement, value, unit?, measured_at?, run_id?, notes?, source: 'instrument', quality: 'normal' })`。`quality`/`source` 不给用户改，固定值。
- 成功后 ElMessage + 对话框关闭；`cmdResult` 保留可再次保存（重复保存由用户负责，幂等键由 client.ts 自动生成，防网络重试不防人工重复点击——与 TestDataView 录入行为一致）。
- MVP 只覆盖手动「执行命令」面板；AI 对话抽屉里的执行结果不入库（聊天气泡内嵌按钮复杂度高，留作后续）。

`api/instruments.ts` / `api/testdata.ts` 均无需改（`TestDataPayload.source` 已在类型里，testdata.ts:28）。

## 3. 复用清单汇总

| 复用对象 | 用在哪 |
|----------|--------|
| `StepItemsEditor.vue` | RunDetailView AI 候选编辑（零改动） |
| AssemblyView aiDialog 三态模式（:81-112） | RunDetailView AI 对话框照抄改三处 |
| assembly 模块 ApplyTemplate / CreateMany / MaxStepOrder / SetSourceTemplateID / TemplateReader 适配器 | runs 模块镜像实现 |
| assembly 步骤状态机（6 状态 7 transition） | runs/model.go 本地复制 |
| main.go `templateReaderBridge`（:525） | 加 runs 版本桥 |
| steptemplates generate + py-agent `/v1/step-plan` + prompt 规则 6 | experiment 生成（已支持，零改动） |
| testdata 模块 CRUD + `source='instrument'` 枚举 | 功能二（零后端改动） |
| `client.ts` 自动 Idempotency-Key（:43） | 所有新写调用 |
| TestDataView 的 dataTypes 常量、projects.ts/runs.ts 列表 API | 功能二对话框 |

## 4. 测试计划

- Go：`runs` 步骤 service/handler 测试 —— 正常 CRUD、状态机合法/非法转移、apply 两种模式 + 互斥 400 + kind 校验 400、depends_on_order → UUID 映射、续排 order、软删 run 级联软删 steps、agent 角色拒绝、viewer 写拒绝。`go test ./...` + `go vet ./...` 全绿。
- 前端：`npm run build` 通过，产物全量同步 `go-server/static/`（PR 清单既有要求）。
- 手工验收：
  1. RunDetailView → AI 生成（描述一次降温测量流程）→ 编辑候选 → 直接应用 → 步骤 tab 出现、可转移状态；
  2. 存模板后在 StepTemplatesView 选 experiment 模板 → 应用到指定 run；
  3. 仪器页执行查询类命令 → 保存到测试数据 → TestDataView 列表可见 source=instrument 的记录，RunDetailView「测试数据」tab 按 run_id 关联可见。

## 5. 文档同步

- `docs/api-contract.md`：补 6 个 run-steps 端点 + test-data 创建的来源说明（instrument）。
- `AGENTS.md`：目录结构无需改（无新模块）；迁移计数「21 个版本」改为最新。

## 6. 预估工作量

| 项 | 内容 | 预估 |
|----|------|------|
| 功能一后端 | 迁移 025 + runs 步骤 6 端点 + apply + 级联软删 + 测试 | 1.5–2 人日 |
| 功能一前端 | runs.ts + RunDetailView 步骤 tab/AI 对话框/模板导入 + StepTemplatesView 分流 | 1–1.5 人日 |
| 功能二前端 | InstrumentMeasureView 保存按钮 + 对话框（含项目/批次选择） | 0.5 人日 |
| 文档 + 联调验收 | api-contract.md、AGENTS.md、手工验收三条路径 | 0.5 人日 |
| **合计** | | **3.5–4.5 人日** |

风险点：runs 步骤 repository 基本是 assembly 的翻版，工作量主要在测试与前端对话框编排；功能二唯一的不确定性是仪器 `response` 文本的数值提取正则，提取失败时靠手填兜底，不阻塞。

## 7. 明确不做（本方案外）

- run 步骤的依赖完成强制 / override_reason 机制、`assigned_to`。
- AI 对话抽屉执行结果的入库入口。
- `instrument_results`（迁移 022）与 `test_data` 的后端关联（如引用 result id）——MVP 用 notes 文本溯源即可。
- 多轮追问式生成（generate 带 history）——step-templates-plan Phase 3 内容。
