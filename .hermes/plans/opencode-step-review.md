# 审查结论：AI 辅助生成装配/实验步骤模板设计方案

> 审查对象：`.hermes/plans/step-templates-plan.md`
> 审查日期：2026-07-26
> 审查基准：代码仓库 `go-server/`（assembly、runs、instruments、middleware、main.go）、`py-agent/serve.py`、`docs/` 设计文档

---

## 总评

方案整体质量高。核心设计决策扎实——AI 候选不落库、human-in-the-loop、模板/应用分离、跨模块只读适配器——每一项都有明确的先例支撑和否决理由说明。集成切入点精准，架构改动范围小。以下审查按 5 个维度展开。

---

## 1. 逻辑漏洞或遗漏（8 项，3 项需要修）

### 1.1 [需修] step_order 校验规则与服务端重排逻辑冲突

- §2.2 说创建/替换 items 时"`step_order` 从 1 连续递增"。
- §3.4 说 Go 二次校验"`step_order` 重排为 1..N 连续（不信任模型给的序号，但保留相对顺序）"。

如果服务端总是按 sort 后重新编号，前端/API 的"连续递增"校验就会错误拒绝非连续但合法的输入（如 `[1, 3, 5]` 本可被重排）。**建议**：服务端接受任意正数 step_order（唯一即可），稳定排序后统一重排为 1..N；校验去掉"连续"要求，只要求 `step_order > 0` 且不重复。

### 1.2 [需修] "保存并应用"两步操作的原子性缺失

§4.1 的"保存并应用"按钮是先 `POST /step-templates` 再调 apply-template。如果第一步成功、第二步失败，留下一棵孤儿模板（无项目应用、用户可能不理解为什么模板库里多了一条）。**建议**：新增复合端点 `POST /api/v1/step-templates` 接受可选字段 `apply_to_project_id`，服务端单事务内完成创建 + apply；或至少在前端两步失败时明确告知，并提供清理入口。

### 1.3 [需修] generate 路由不在项目中间件组下，权限检查方式未定义

§2.4 规定 generate 要求"在任一项目有 member+"。但 `/api/v1/step-templates/generate` 在独立路由组下（不在 `/projects/{id}/` 下），无法复用 `RequireProjectPermission` 中间件。Plan 说 steptemplates service 负责 CRUD、限流，但没说明如何检查"在任一项目有 member+"。**建议**：steptemplates service 调 `ProjectAccessChecker` 时，用 `SELECT EXISTS (SELECT 1 FROM project_members WHERE user_id=$1 AND status='active' AND role IN ('member','maintainer','owner'))` 判断用户是否属于任何项目的 member+；或新增一个 `HasAnyProjectRole(userID, minRole)` 接口。

### 1.4 [提示] run_steps `ON DELETE RESTRICT` 与 runs SoftDelete 的矛盾

迁移 025 `run_steps.run_id REFERENCES experiment_runs(id) ON DELETE RESTRICT`。RESTRICT 在 PostgreSQL 中会阻止父行删除。如果 runs 走软删除（`deleted_at IS NOT NULL`），有 run_steps 的 run 的 SQL 软删除 UPDATE 不会触发 FK 检查，所以不会被阻止。但如果有物理删除 run 的路径，就会被阻止。**建议**：Phase 2 实现时，若 run 被软删除，run_steps 也要级联软删除（在 runs.SoftDelete 中同步处理）。

### 1.5 [提示] step_template_items 无 soft delete

`step_template_items` 表没有 `deleted_at`。模板被 CASCADE 删除或 PUT 整体替换时，旧 items 被硬删除，丢失历史。`assembly_steps` 有软删除。**建议**：items 加 `deleted_at` 列，PUT 替换时写 `deleted_at=now()` 再用新 items 插入；模板软删除时同步软删 items（handler/service 逻辑，非 CASCADE）。

### 1.6 [提示] inline steps 模式缺少 `source_prompt` 溯源

§4.1 "直接应用到本项目"不存模板，直接调 apply-template 内联 steps。但此时 `assembly_steps.source_template_id` 为 NULL，`source_prompt` 也不在 steps 表中。审计只能看到步骤被创建，看不到它们来自 AI。**建议**：apply-template 响应/审计 details 中携带 `source_prompt`/`ai_generated` 标记；或在 `assembly_steps` 加一个 `source_prompt TEXT` 字段。

### 1.7 [提示] "直接应用到本项目"按钮请求体设计不明

§4.1 说 apply-template "同时支持 `template_id` 或内联 `steps` 数组（二选一）"。但 §2.3 只给出了 `{ "template_id": "uuid" }` 的请求体。内联模式需要不同请求体形状。**建议**：明确两种模式的请求体 schema，handler 中严格互斥校验（两者都有 → 400，两者都无 → 400）。

### 1.8 [提示] 模板应用后无重复检测

用户可能误将同一个模板多次应用到同一项目，产生重复步骤。Phase 3 提到做重复检测，但属于可选。**建议**：MVP 至少在前端做一个轻量提示——apply 时查询项目现有步骤名，如果有 >= 50% 匹配，显示确认框"此模板步骤与项目现有步骤高度重叠，是否继续？"。

---

## 2. API 设计是否符合现有规范（3 项，1 项需修）

### 2.1 [需修] `page` / `per_page` 与 `api-contract.md` 矛盾

api-contract.md §2.4 规定"列表接口使用 `page_size`、`cursor` 游标分页"。Plan 使用 `page` / `per_page`。**与现有代码一致**（assembly、runs 等全部用 `page`/`per_page`），但与文档契约矛盾。**建议**：同步更新 api-contract.md §2.4，改为 `page`/`per_page` 并注明"当前实现使用页码偏移分页，后续可能迁移为 cursor 分页"，消除文档-代码不一致。

### 2.2 [提示] `PUT /items` 全文替换与代码风格不一致

现有代码中所有更新都用 PATCH（assembly.Update、runs.Update、logs.UpdateLog 等）。Plan 对 items 用 PUT（整体替换）。路径上不标注 `/items` 为子资源将导致歧义。**建议**：保持 PATCH 语义一致性，改为 `PATCH /api/v1/step-templates/{id}/items`，body 带 `steps` 数组表示全量替换（因 items 无独立 ID，PATCH 整体替换在本场景比 PUT 更符合现有习惯）。

### 2.3 [符合] 其余 API 设计符合规范

- URL 模式 `/api/v1/step-templates`、`/api/v1/projects/{id}/assembly/apply-template` — 一致
- 响应信封 `{data, request_id}` / `{error: {code, message, details}, request_id}` — 一致
- 写接口要求 `Idempotency-Key` — 一致
- 错误码 snake_case — 一致
- 审计动作命名（`step_template.generated/.created/.updated/.deleted`、`assembly.template_applied`） — 符合先例

---

## 3. 与现有系统的集成可行性（4 项，1 项需修）

### 3.1 [需修] migration 编号 024/025 可能与其他并行分支冲突

现有 migrations 最大编号为 023（idempotency_keys）。若其他并行分支也在增加迁移，会产生编号冲突。**建议**：与该计划实施时确认迁移编号未被占用。如果被占用，可使用当前编号最大值 + 1。

### 3.2 [符合] TemplateReader 适配器模式完全可行

Plan 的五步组装方式与 `assembly.ProjectAccessAdapter` 同构：

```go
type TemplateReader interface {
    GetTemplateWithItems(id string) (*steptemplates.StepTemplate, []steptemplates.StepTemplateItem, error)
}
type TemplateReaderAdapter struct{ Repo *steptemplates.Repository }
```

再通过 `assemblySvc.ConfigureTemplates(adapter)` 注入 — 这是已有模式的最小外延，零风险。

### 3.3 [符合] AI 调用链路完全复用 `/v1/interpret` 模式

- 同一环境变量 `PY_AGENT_INTERPRET_URL` + `PY_AGENT_INTERNAL_TOKEN_FILE`
- 同一 HTTP client（15s timeout）
- 同一 Bearer token 校验（`secrets.compare_digest`）
- 同一 `ensure_safe()` + `DisallowUnknownFields()` + 限长读取
- 同一错误码映射（`ParseError → 422`、provider 异常 → 502）

py-agent `serve.py` 只需加一条路由，服务进程不变，Docker Compose 不变。

### 3.4 [符合] 中间件链复用正确

apply-template 站在 `/api/v1/projects/` 路由组下，自动享有 `AuthRequired → AgentContext → Audit → RequireIdempotencyKey`。steptemplates CRUD 站独立路由组，需要单独挂中间件链，与现有 `/api/v1/assembly` 路由组模式一致。

---

## 4. 安全边界（5 项，2 项需修）

### 4.1 [需修] Prompt 规则 4 与规则 5 语义暧昧

- 规则 4："安全关键操作（通气、升压、加高压、开束流）必须在 description 注明检查项与确认人" → 暗示 LLM 可以输出包含高危操作的步骤
- 规则 5："要求直接控制设备、绕过审批、或与装配/实验无关时返回 rejected"

边界在于"描述人工操作" vs "要求系统控制"。如果用户说"帮我制定通入氦气的步骤"，合理做法是输出包含安全备注的手动操作步骤（ok）。如果用户说"通入氦气到 10 mbar"，表述上像"要求系统控制"，但也可能是"帮我写这个步骤的描述"。**建议**：在 prompt 规则 4 与 5 之间插入一条明确的分界规则：

> 4a. 如果用户意图是让系统直接替代人工操作设备，返回 rejected。描述人工执行的步骤——即使内容涉及高危操作——属于 ok，只需按规则 4 在 description 中注明检查项。

### 4.2 [需修] 手动创建模板的 description 未经注入筛查

§4.2 "手动新建模板"不经过 AI，description 可由用户自由填写。如果某个用户写了"执行 `DELETE FROM ...`"之类的 SQL 注入式文本，虽然不直接执行，但如果未来有其他 Agent 读 description 并做自然语言解析，可能触发意外行为（参考 `agent-auto-review.md` §7.3 的工具调用防护）。**建议**：在 steptemplates service 的 Create/Update 中对 description 做与 AI 输出同级的 `ensure_safe()` 注入筛查，标记高风险模板。

### 4.3 [符合] AI 不直接写数据库

Generate 仅返回候选 JSON，前端展示编辑，确认后走普通 CRUD/apply。AI 产出永远是候选，与 issues/experiences 的 `ai_generated` 标记 + 人工确认流程一致。

### 4.4 [符合] Agent 角色全链路拒绝

权限矩阵中 agent 列五项全 ❌。generate、CRUD、apply 全部由非 agent 角色操作。一致于现有 assembly/service.go:54 的 `if userRole == auth.RoleAgent { return ErrForbidden }`。

### 4.5 [符合] Go 侧二次校验全面

- 三态校验（ok / clarify / rejected）
- steps 数量 1–30 条
- name 去空白 ≤ 256，description ≤ 2000
- step_order 去重 + 服务端重排
- depends_on_order < step_order 且指向同模板已有 — 天然无环
- kind 枚举校验；experiment meta 枚举白名单核验
- 全部不过 → 502 或 422

---

## 5. 改进建议（优先级排序）

### 5.1 [高] 将"保存并应用"提供为复合端点

新增 `POST /api/v1/step-templates` 的可选请求字段 `apply_to_project_id`。原子操作避免两步间的孤儿模板。

### 5.2 [高] 修复 step_order 校验冲突

服务端接受任意正 int，稳定排序后重排为 1..N。API 校验只说 > 0 且不重复，去掉"连续"要求。这是 §1.1 所描述的必须修的项。

### 5.3 [高] 明确 inline steps apply-template 请求体

与 §1.7 关联。定义两组请求体形状（`template_id` 与 `steps` 互斥），handler 严格校验。

### 5.4 [中] 补充 generate 的用户权限检查路径

在 steptemplates service 增加 `HasAnyProjectRole(userID, minRole)` 逻辑，Generate 在调 py-agent 前完成鉴权。这是 §1.3 的实现细节补充。

### 5.5 [中] PUT items → PATCH items + 乐观锁

`PATCH /api/v1/step-templates/{id}/items`，步数 checked against `updated_at` 防止并发覆盖。对齐现有 PATCH 风格。

### 5.6 [中] `source_template_id` 标记在前端默认显示，而非可选

§4.1 说名称旁显示"模板"标记（可选）。建议改为默认显示"此步骤来自模板「XXX」"。有助于审计和用户理解步骤来源。

### 5.7 [低] step_template_items 增加 deleted_at

软删除保留历史，便于审计和恢复。与 system 中其余 SoftDelete 模式一致。

### 5.8 [低] apply 前提示重复步骤

查询目标项目中与模板步骤名 > 50% 相似度的已有步骤，弹出确认框。即使 MVP 不做后端去重，前端轻量提示也可降低重复录入风险。

### 5.9 [低] 同步 api-contract.md 分页规范

修改 api-contract.md §2.4 或补充说明：页码偏移分页为当前实现方式，后续可能迁移为 cursor。消除文档-代码不一致。

### 5.10 [低] inline steps 溯源

apply-template 内联模式在 assembly_steps 或 audit details 记录 AI 生成标记和 source_prompt，确保即使不存模板也能追溯步骤来源。

---

## 总结表

| 项目 | 状态 | 阻塞发布？ |
|------|------|:---:|
| step_order 校验冲突 | 需修 | 是 |
| "保存并应用"原子性缺失 | 需修 | 是 |
| generate 权限检查实现路径未定义 | 需修 | 是 |
| inline steps 请求体未定义 | 需修 | 是 |
| run_steps FK RESTRICT + SoftDelete | 提示 | 否 |
| items 无软删除 | 提示 | 否 |
| inline steps 溯源缺失 | 提示 | 否 |
| 模板重复应用无提示 | 提示 | 否 |
| page/per_page 与文档矛盾 | 提示 | 否 |
| PUT vs PATCH 风格不一致 | 提示 | 否 |
| prompt 规则 4/5 语义暧昧 | 安全 | 否 |
| 手动模板内容未注入筛查 | 安全 | 否 |
| 其余设计点 | 符合 | — |

**结论**：方案在核心架构决策、安全模型、集成路径上设计充分。4 个需修项集中在实现细节（API body 约定、校验一致性、原子操作路径），均可在实现阶段解决。建议在编码前修复上述 4 个需修项后即可进入开发。

审查人：opencode (deepseek-v4-pro)
