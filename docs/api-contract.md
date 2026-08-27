# 模块边界与 API 契约

> 版本：v2（2026-08-11 与 main.go 路由全量核对同步）
> 适用范围：实验室日志系统扩展方案 v5
>
> 同步说明：本章所有端点以 `go-server/main.go` 路由注册为唯一事实源（main.go:249-609），
> 请求/响应/状态码以各模块 handler.go 实现为准（正文附 `文件:行号` 标注）。
> 每节标注「已实现」/「未实现（预告）」状态；未实现端点保留文档超前于实现的记录。

## 1. 边界原则

- 模块之间只通过 HTTP REST API 通信。
- 任何模块不得直接读取、写入或 join 另一个模块的数据库表。
- Go API 网关负责统一鉴权、审计上下文注入、请求 ID、限流和错误格式。
- Python LightAgent、OCR、EPICS/PLC 接入、仪器控制服务都视为独立服务客户端。
- 所有写接口必须带 `Idempotency-Key`，防止 Agent 或前端重试造成重复写入。

## 2. 通用约定

### 2.1 URL 与版本

所有业务 API 使用：

```text
/api/v1/{module}/{resource}
```

### 2.3.2 翻译旁路（一期）

日志和日报保留原文字段，并可附 `translations`：字段键为 `content`、`raw_text` 或 `summary`，variant 状态为 `ready/pending/failed/stale/missing`。只有 `ready` 且 hash 匹配的译文可展示。
`POST/PATCH /api/v1/logs/{id}/translations` 与 `/api/v1/daily-reports/{id}/translations` 接收 `{field,target_locale,force?,translated_text?}`；POST 按需入队，PATCH 保存人工译文。两者均需对象既有权限、CSRF、`Idempotency-Key` 和审计。

### 2.2 通用请求头

| Header | 必填 | 说明 |
|--------|------|------|
| `Authorization: Bearer <access_token>` | 否（Cookie 通道二选一） | 用户 JWT；登录成功同时种 `access_token`/`refresh_token` HttpOnly Cookie（Path=/api），JWT 头与 Cookie 二选一均可 |
| `X-CSRF-Token` | 写请求必填（白名单豁免） | 登录响应与 `csrf_token` Cookie（Path=/）携带；`/api/v1/auth/*`、`/api/v1/agent/*` 与 SERVICE_TOKEN 调用豁免（middleware/csrf.go） |
| `Idempotency-Key` | 写接口必填 | 缺失 → 400 `missing_idempotency_key`；同一 key 重复 → 409 `duplicate_idempotency_key`（middleware/idempotency.go:27,37）；GET/HEAD/OPTIONS 与 SERVICE_TOKEN 调用豁免 |
| `X-Request-ID` | 否 | 客户端请求 ID；缺省由网关生成 `request_id` |
| `X-Acting-User-ID` | Agent 必填 | Agent 代表的真实用户（`X-Agent-Task-ID` 同理）；无任务上下文时 middleware 返回 403 并上报告警 |
| `X-Device-ID` | 未实现 | 设备推送预留（当前无设备推送端点） |
| `X-Signature` | 未实现 | HMAC-SHA256 签名（预留） |
| `X-Timestamp` | 未实现 | 毫秒时间戳（预留） |

### 2.3 通用响应

成功：

```json
{
  "data": {},
  "request_id": "req_20260714_000001"
}
```

失败：

```json
{
  "error": {
    "code": "permission_denied",
    "message": "当前用户无权访问该项目",
    "details": {}
  },
  "request_id": "req_20260714_000001"
}
```

### 2.3.1 冲突类错误码（409）

写接口在状态机/乐观锁/幂等重放下返回 `409 Conflict`，`error.code` 区分原因：

| code | 语义 | 典型场景 |
|------|------|----------|
| `state_conflict` | 状态已变更，操作不再适用 | 对已 done 待办重复 done；对 deferred 待办重复 defer |
| `version_conflict` | 乐观锁版本过期 | `PATCH /todos/{id}` 携带的 `updated_at` 不是最新值 |
| `duplicate_idempotency_key` | 同一 Idempotency-Key 已被使用 | 同 key 重放写请求（响应含 `existing_request_id`） |
| `invalid_agent_lease` | Agent claim_token 不匹配/过期 | complete/fail 携带的 claim_token 非本次领取值 |
| `status_conflict` | 运行/步骤状态流转非法 | runs/assembly 的状态迁移违反状态机 |
| `transition_warning` | 项目状态迁移有未处理警告 | 项目 transition 带 warnings（409 且 details 含 warnings） |

### 2.4 分页、过滤与时间

- 时间字段使用 RFC3339：`2026-07-14T10:30:00+08:00`。
- 列表接口以 `page`/`per_page` 为主（默认 1/20；`per_page` 上限各端点不同：audit 100、ask 50、alert 200、agents 无显式上限）；`todos` 用 `date/scope/status/limit`，`alerts` 用 `limit/offset`。无 cursor 游标分页。
- 删除接口默认软删除/作废，响应仍返回被删除对象 `{id}`（HTTP 200，非 204）。

## 3. 模块 API

## 3.1 认证与用户模块（已实现）

路由：`go-server/auth/handler.go:69-77`（挂载 main.go:249），管理员用户路由 main.go:250-258。
CSRF 豁免路径（`/api/v1/auth/*`）；写端点 handler 级校验 Idempotency-Key（400 缺失）。

### `POST /api/v1/auth/register`（默认关闭）

请求：`{username, password, invitation_code}`。`ALLOW_REGISTER!=true` 时返回 403 `registration_disabled`；开启时缺码返回 400 `invitation_code_required`，无效/已用/过期/撤销统一返回 400 `invalid_invitation_code`。邀请码消费与用户创建在同一事务完成。
用户已存在 → 409 `username_taken`；IP 限流 5 次/小时 → 429。响应 201：`UserInfo`。

### `POST /api/v1/auth/login`

请求：

```json
{
  "username": "zhangsan",
  "password": "********"
}
```

响应（200）：

```json
{
  "data": {
    "access_token": "jwt",
    "expires_in": 900,
    "refresh_token": "opaque_refresh_token",
    "refresh_expires_in": 2592000,
    "csrf_token": "csrf_token",
    "must_change_password": false,
    "user": {
      "id": "usr_001",
      "name": "张三",
      "roles": ["member"],
      "language": "zh"
    }
  },
  "request_id": "req_001"
}
```

同时 Set-Cookie：`access_token`/`refresh_token`（HttpOnly，Path=/api）、`csrf_token`（Path=/）。
错误：401 `invalid_credentials`、403 `account_disabled`、429（IP 限流 20 次/15min，跨用户名聚合，`account_locked`）。
新 IP 登录、admin 登录失败、账户锁定会触发安全告警（auth/handler.go）。

### `POST /api/v1/auth/refresh`

请求：`{refresh_token}`（或 refresh_token Cookie 兜底）。响应同登录（refresh token 轮换）。
与 login 共享 IP 级滑动窗口限流。

### `POST /api/v1/auth/logout`

撤销当前 refresh token，清三个 Cookie。响应：`{success: true}`。

### `GET /api/v1/auth/me`

返回当前用户资料、角色、语言。401（未登录或用户不存在）。

### `POST /api/v1/auth/change-password`

请求：`{old_password, new_password}`。响应：`{success: true}`。错误：400 `password_too_short`、429 `account_locked`。

### `PATCH /api/v1/auth/profile`

更新当前用户自己的资料。请求：

```json
{
  "language": "zh"
}
```

`language` 仅支持 `zh` / `en`，用于前端界面语言；登录、刷新、`/auth/me` 返回的 user 对象均携带 `language` 字段。非法值返回 400 `invalid_language`。

### 管理员用户管理（`/api/v1/admin/users`，admin only）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/admin/users/` | 用户列表（main.go:254） |
| POST | `/api/v1/admin/users/` | 创建用户 `{username, display_name, role, password}`；响应 201 `{user, temporary_password}`；409 `username_taken`（main.go:255） |
| PATCH | `/api/v1/admin/users/{id}` | 修改 `{display_name, role, disabled}`；400 `cannot_modify_self`、409 `last_active_admin`（main.go:256） |
| POST | `/api/v1/admin/users/{id}/reset-password` | 重置密码；响应 `{temporary_password}`（main.go:257） |

> 契约历史：旧文档 `/api/v1/users/me`、`POST /api/v1/users` 路径不存在，已更正为 `/api/v1/auth/me` 与 `/api/v1/admin/users/*`。

## 3.2 日报模块（已实现）

路由：main.go:312-326。组级中间件：AuthRequired + AgentContext + Audit + RequireIdempotencyKey。

### `GET /api/v1/daily-reports`

参数：`status`、`keyword`、`date`、`page`、`per_page`（`per_page` 超限 → 400 `per_page_too_large`）。
响应：`{items: [DailyReport], total, page}`。`AuthorID` 强制为当前用户（只能看自己的日报）。

### `POST /api/v1/daily-reports/today`

无 body，幂等获取/创建今日日报（非标准语义：返回 200 而非 201）。响应：`DailyReport`。

### `GET /api/v1/daily-reports/by-date`

参数：`date`、`latest=true`（回溯最近一份非空日报）、`user_id`。
SERVICE_TOKEN 调用（todos scheduler 拉全量日报）：必须显式传 `user_id`（缺失 400）；
普通 JWT 调用时 `user_id` 被忽略、强制取自己（防越权）。
唯一显式设置审计 action 的 GET（`daily_report.by_date`，属敏感读）。service token 审计行 `actor_type='system'`。

### `GET /api/v1/daily-reports/{id}`

单份日报。404 `report_not_found`。

### `PATCH /api/v1/daily-reports/{id}`

仅作者本人。Body：`{raw_text?, summary?}`，两字段均可省略（空 body 允许，即只获取）；显式 `summary: ""` 清空摘要。`raw_text` ≤4000、`summary` ≤1000 Unicode 字符。响应：`DailyReport`。403 `ErrNotReportOwner`。

### `POST /api/v1/daily-reports/{id}/submit`

仅作者本人。Body：`{force}`（可选）。响应：`{report, warnings, blocked}`。
错误：400 `already_submitted`、`no_log_entries`、`log_project_missing`、`project_lifecycle_blocked`。

### `POST /api/v1/daily-reports/{id}/ai-parse`

把日报 raw_text、已关联日志交给 py-agent 整理为结构化日志草稿和事实摘要。**结果不落库**，日志仅返回给前端由用户逐条编辑确认后走 `POST /api/v1/projects/{id}/logs` 入库；摘要由日报 PATCH 保存。

要求：`Idempotency-Key` 头（组级中间件强制）；仅日报作者本人（admin 除外）且日报为 `draft` 状态；agent 角色被 middleware 白名单拦截（403 `agent_action_forbidden`）。每用户限流 10 次/分钟（429，全库唯一限流点之一）。

请求体：无（`projects` 由服务端按当前用户 `create_log` 权限注入，不透传前端）。

响应（三态，对齐 py-agent `/v1/daily-parse`；`ok` 的 `summary` 非空且 ≤1000 字符，其他状态为 `null`）：

```json
{
  "data": {
    "status": "ok",
    "logs": [
      {
        "category": "assembly",
        "project_id": "prj_rf_001",
        "content": "装配匹配电路",
        "raw_snippet": "今天装配了匹配电路",
        "occurred_at": "2026-08-06T09:00:00+08:00"
      }
    ],
    "question": null,
    "reason": null,
    "model": "deepseek-v4-pro",
    "prompt_version": "1.2"
  },
  "request_id": "req_001"
}
```

`status=ok` 的每条日志必须包含 `raw_snippet`：它是日报原文按 `。！？；` 或换行分隔、仅裁掉首尾 Unicode 空白后的完整非空分段（1–4000 Unicode 字符），逐字保留大小写和其余字符；逗号（，）、顿号（、）、英文句点（.）不是分隔符，短子串或改写均无效，重复分段按出现次数覆盖。有效分段超过 20 个时返回 `clarify`，不静默漏段。

`status=clarify` 时 `question` 非空、`logs` 为空；`status=rejected` 时 `reason` 非空、`logs` 为空。

错误码：

| HTTP | code | 场景 |
|------|------|------|
| 400 | missing_idempotency_key | 缺少 Idempotency-Key |
| 409 | duplicate_idempotency_key | Idempotency-Key 重复 |
| 404 | report_not_found | 日报不存在 |
| 403 | permission_denied | 非日报作者 |
| 400 | already_submitted | 日报已提交，不可再整理 |
| 400 | empty_raw_text | 日报原文为空 |
| 400 | bad_request | raw_text 超 4000 字符或 py-agent 判定请求参数错误 |
| 400 | ai_parse_failed | 模型输出非法或响应未通过二次校验，请修改描述后重试 |
| 429 | too_many_requests | 超过 10 次/分钟限流 |
| 502 | upstream_error | py-agent 未配置/不可达/超时/其他非 2xx |

审计：动作 `daily_report.ai_parsed`，明细只记 report_id、status 与 log_count，不记 raw_text 全文。

## 3.3 日志模块（已实现）

路由：项目级 main.go:340,368；顶层 main.go:453-462。
> 契约历史：旧文档 `POST/GET /api/v1/logs`、`DELETE /api/v1/logs/{log_id}`、`POST /api/v1/logs:parse` 路径不存在。
> 日志列表/创建挂在 `/api/v1/projects/{id}/logs` 下；parse 能力由 `POST /api/v1/daily-reports/{id}/ai-parse` 取代。

### `GET /api/v1/projects/{id}/logs`

参数：`page`、`per_page`、`category`、`date_from`、`date_to`、`status`。
响应：`{items: [Log], total, page}`。权限：项目 `PermRead`（viewer 可读）。

### `POST /api/v1/projects/{id}/logs`

请求：`{category, content, occurred_at?, source?, daily_report_id?, raw_snippet?}`（main.go:368，`PermCreateLog` member+）。
`raw_snippet` 是 AI 日志的来源证据，必须是关联日报当前 `raw_text` 按 `。！？；` 或换行分隔、仅裁掉首尾 Unicode 空白后的一个完整非空分段（1–4000 Unicode 字符）；提供时 `daily_report_id` 必填且后端会校验。数据库字段可空，手工及历史日志可不提供。
响应 201：`Log`。错误：400 `log_project_missing`、`project_lifecycle_blocked`。

### `GET /api/v1/logs/{id}`

返回日志正文、附件、关联 issue、关联仪器数据。404 `log_not_found`；权限在 service 内按日志归属校验。

### `PATCH /api/v1/logs/{id}`

Body：`{category?, content?, occurred_at?, content_status?}`。只允许作者、项目管理员、admin 修改。
`raw_snippet` 不在 PATCH 契约中，创建后不可修改。
Agent 只能修改自己创建且未人工确认的草稿。错误：400 `log_voided`、403 `log_not_draft`/`log_owner_mismatch`。

## 3.4 项目模块（已实现）

路由：main.go:327-376。组级中间件：AuthRequired + AgentContext + Audit + RequireIdempotencyKey。

### `GET /api/v1/projects`

参数：`status`。响应：`[ProjectWithStats{id, code, name, status, visibility, member_count, open_issue_count, log_count, ...}]`。
admin 看全部；其余用户只列自己 active 成员的项目（projects/handler.go:131 起）。

### `POST /api/v1/projects`

**限 maintainer/admin**（main.go:334 `RequireRole(admin, maintainer)`，service 内同语义纵深校验；viewer 不可创建）。
请求：`{code, name, short_name?, description?, visibility?, start_date?, target_end_date?, default_category?, tags?}`。
响应 201：`Project`。错误：409 `project_code_taken`。

### `GET /api/v1/projects/{id}`

项目详情（含统计）。权限：`PermRead`（active 成员；admin 直通）。404 `project_not_found`。

### `PATCH /api/v1/projects/{id}`

Body：`{name?, short_name?, description?, visibility?, comment_policy?, start_date?, target_end_date?, default_category?, tags?}`。
权限：`PermManageProject`（maintainer/owner）。审计 `projects.update`。

### `POST /api/v1/projects/{id}/transition`

Body：`{action, ignore_warnings?, reason?}`。响应：`{project, warnings}`。
权限：`PermManageProject`。错误：400 `invalid_transition`、409 `transition_warning`（details 含 warnings）。
成功后异步发 ntfy `lab-alerts`。审计 `projects.transition`。

### 项目成员管理（S1-S6 group access，main.go:359-364）

| 方法 | 路径 | 权限 | 说明 |
|------|------|------|------|
| GET | `/api/v1/projects/{id}/members` | `PermRead`（viewer 也可读） | 响应 `[ProjectMember{project_id, user_id, role, status, muted, joined_at, added_by}]`（projects/handler.go:131-174） |
| POST | `/api/v1/projects/{id}/members` | `PermManageMembers`（owner/maintainer；admin 直通） | Body `{user_id, role}`；响应 201；404 `user_not_found`；审计 `projects.members.add` |
| PATCH | `/api/v1/projects/{id}/members/{userID}` | `PermManageMembers` | Body `{role}`；审计 `projects.members.update` |
| DELETE | `/api/v1/projects/{id}/members/{userID}` | `PermManageMembers` | 响应 200 `{success: true}`；400 `last_owner`（不可移除最后 owner） |

## 3.5 问题管理模块（已实现）

路由：项目级 main.go:340,373；顶层 main.go:463-474。

### `GET /api/v1/projects/{id}/issues`

参数：`status`、`severity`、`assignee`、`author`、`search`、`page`、`per_page`、`sort`、`order`。
响应：`{items: [Issue], total, page}`。权限：项目 `PermRead`。

### `POST /api/v1/projects/{id}/issues`

请求（main.go:373，`PermCreateIssue` member+）：

```json
{
  "project_id": "prj_rf_001",
  "title": "RF 匹配在 4.2 MHz 附近反射异常",
  "description": "S11 曲线出现尖峰",
  "severity": "medium",
  "assignee_id": "usr_002",
  "related_log_ids": ["log_001"]
}
```

响应 201：`Issue`。错误：404 `related_log_not_found`；`ai_generated`/`agent_task_id`/`candidate_id` 仅 agent 角色可携带（非 agent 携带 → 400）。创建后异步 ntfy。

### `GET /api/v1/issues/{id}`

单条详情。权限：service 内校验。

### `PATCH /api/v1/issues/{id}`

Body：`{title?, description?, severity?, assignee_id?}`。`ai_generated`/`agent_task_id` 不可修改（解码层 400）。

### `POST /api/v1/issues/{id}/transition`

请求：

```json
{
  "target_status": "resolved",
  "reason": "已更换匹配电容并复测通过"
}
```

错误：400 `invalid_transition`、`reason_required`、`issue_closed`；403 `transition_forbidden`。
resolved/reopen 时异步 ntfy。

### `POST /api/v1/issues/{id}/comments`

Body：`{content}`。响应 201：`Comment`。错误：403 `comments_disabled`。

## 3.6 实验运行模块（已实现）

路由：项目级 main.go:342-343；顶层 main.go:377-403。

### `GET /api/v1/projects/{id}/experiment-runs`

参数：`campaign`、`status`、`run_type`、`page`、`per_page`。响应：`{items, total, page, per_page}`。权限：`PermRead`。

### `POST /api/v1/projects/{id}/experiment-runs`

Body：`{name, campaign?, run_type, gas_type, target_temp?, min_temp?, pressure_min?, pressure_max?, pressure_unit, has_beam, devices, description?}`。
响应 201：`ExperimentRun`。审计 `experiment_run.create`。

### `GET /api/v1/experiment-runs/{id}`

单条详情。

### `PATCH /api/v1/experiment-runs/{id}`

Body：`{name?, campaign?, run_type?, gas_type?, ..., transition?}`。错误：400 `invalid_transition`、409 `status_conflict`。
审计 `experiment_run.update`。

### `DELETE /api/v1/experiment-runs/{id}`

软删除。响应 200：`{id}`。审计 `experiment_run.delete`。

### `POST/DELETE /api/v1/experiment-runs/{id}/daily-reports/{report_id}`

关联/解除日报。响应：`{run_id, report_ids: [...]}`（DELETE 200 非 204，404 `report_link_not_found`）。
审计 `experiment_run.link.create` / `experiment_run.link.delete`。

### `GET /api/v1/experiment-runs/{id}/steps`

响应：`{items: [RunStep], total}`。

### `POST /api/v1/experiment-runs/{id}/steps`

Body：`{name, description?, depends_on?, step_order}`。响应 201：`RunStep`。审计 `run_step.create`。

### `POST /api/v1/experiment-runs/{id}/steps/apply-template`

Body：`{template_id?, steps?, source_prompt?}`。响应 201：`[RunStep]`（批量）。审计 `run_step.template_applied`。

### `POST /api/v1/run-steps/reorder`

Body：`{run_id, steps: [{id, step_order}]}`。响应 200 原样回显请求体。审计 `run_step.reorder`。

### `PATCH /api/v1/run-steps/{id}` / `DELETE /api/v1/run-steps/{id}`

Body（PATCH）：`{name?, description?, depends_on?, transition?}`（含 transition 时审计 action 升级为 `run_step.transition`）。
DELETE 软删除响应 200 `{id}`。

## 3.7 装配模块（已实现）

路由：项目级 main.go:344-346；顶层 main.go:404-415。

### `GET /api/v1/projects/{id}/assembly`

参数：`status`、`page`、`per_page`。响应：`{items, total, page, per_page}`。权限：`PermRead`。

### `POST /api/v1/projects/{id}/assembly`

Body：`{name, description?, depends_on?, assigned_to?, step_order}`（DisallowUnknownFields）。
响应 201：`AssemblyStep`。错误：400 `dependency_cycle`、`dependency_pending`。审计 `assembly.create`。

### `POST /api/v1/projects/{id}/assembly/apply-template`

Body：`{template_id?, steps?, source_prompt?}`。响应 201：`[AssemblyStep]`。审计 `assembly.template_applied`。

### `POST /api/v1/assembly/reorder`

Body：`{project_id, steps: [{id, step_order}]}`。响应 200 原样回显。审计 `assembly.reorder`。

### `GET /api/v1/assembly/{id}`

详情。404 `assembly_step_not_found`。

### `PATCH /api/v1/assembly/{id}`

Body：`{name?, description?, assigned_to?, transition?, override_reason?}`。
transition 存在时审计 action 升级 `assembly.transition`，带 override_reason 再升级 `assembly.transition.override`。

### `DELETE /api/v1/assembly/{id}`

软删除。响应 200：`{id}`。审计 `assembly.delete`。

## 3.8 步骤模板模块（已实现）

路由：main.go:416-430。

### `GET /api/v1/step-templates`

参数：`kind`、`q`、`page`、`per_page`。登录即可（按全局角色过滤）。

### `POST /api/v1/step-templates`

Body：`{name, kind, description?, source_prompt?, ai_generated?, items: [{name, description?, step_order, depends_on_order?}], apply_to_project_id?}`。
响应 201：`StepTemplate`。审计 `step_template.created`。

### `POST /api/v1/step-templates/generate`

AI 生成模板（不落库直接返回）。Body：`{kind, prompt, context?}`。
响应：`{status, name_suggestion, steps: [{name, description, step_order, depends_on_order}], question?, reason?, model, prompt_version}`。
权限：**必须至少在一个项目担任 member 或以上角色**（否则 403）；错误：502 `upstream_error`（interpret 降级）、body 限 256KB。审计 `step_template.generated`。

### `GET /api/v1/step-templates/{id}`

详情（含 items）。404。

### `PATCH /api/v1/step-templates/{id}`

Body：`{name?, description?}`。审计 `step_template.updated`。

### `PATCH /api/v1/step-templates/{id}/items`

整体替换 items。Body：`{items: [ItemDef]}`。响应 200：`{id}`。审计 `step_template.items_replaced`。

### `DELETE /api/v1/step-templates/{id}`

软删除。响应 200：`{id}`。审计 `step_template.deleted`。

## 3.9 RF 匹配模块（已实现）

路由：项目级 main.go:350-351；顶层 main.go:442-452。

### `GET /api/v1/projects/{id}/rf-matching`

参数：`device`、`status`、`page`、`per_page`。响应：`{items, total, page, per_page}`。权限：`PermRead`。

### `POST /api/v1/projects/{id}/rf-matching`

Body：`{device, frequency_mhz, s11?, input_*?, output_*?, transformer_turns?, capacitance_text?, transformer_material?, shunt_inductance?, series_capacitor?, status?, notes?, measured_at?}`（DisallowUnknownFields）。
响应 201：`RFMatchingRecord`。审计 `rf_matching.create`。

### `GET /api/v1/rf-matching/{id}`

详情。404。

### `PATCH /api/v1/rf-matching/{id}`

仅限可变字段白名单：`s11`/`input_*`/`output_*`/`transformer_turns`/`capacitance_text`/`transformer_material`/`shunt_inductance`/`series_capacitor`/`status`/`notes`；
`device`/`frequency_mhz`/`measured_at` 不可改（传入 → 400）。审计 `rf_matching.update`。

### `DELETE /api/v1/rf-matching/{id}`

软作废（MarkVoid）。Body 可选 `{reason}`（DELETE 带 body）。响应 200：`{id}`。审计 `rf_matching.delete`。

## 3.10 测试数据模块（已实现）

路由：项目级 main.go:347-349；顶层 main.go:431-441。

### `GET /api/v1/projects/{id}/test-data`

按项目分页列出测试数据。Query：`run_id`/`data_type`/`quality`（过滤，均需合法值，
否则 400）、`page`/`per_page`（默认 1/20，per_page ≤100）。默认过滤 `quality='invalid'`。
Resp 200：`{items: TestData[], total, page, per_page}`。权限：项目 viewer 及以上。

### `POST /api/v1/projects/{id}/test-data`

单条录入。Body 为 `TestDataPayload`（**开启 DisallowUnknownFields，未知字段 → 400**）：

```json
{ "data_type": "cryo", "measurement": "target_temp", "value": 4.2, "unit": "K",
  "quality": "normal", "measured_at": "2026-08-09T10:30:00+08:00",
  "run_id": "70000000-0000-4000-8000-000000000001", "notes": "稳定后读数" }
```

必填：`data_type`（5 枚举）、`measurement`（≤128）、`value`（数值）；`quality`/`source`
缺省 `normal`/`manual`；`unit` ≤16；`run_id` 可选，须为存在的实验批次 UUID。
Resp 201：完整 `TestData`。错误：400 `bad_request`（参数无效/未知字段）、403 `permission_denied`、
404 `project_not_found`、409 `duplicate_idempotency_key`。需 `Idempotency-Key`，写审计
（action `test_data.create`）。权限：项目 member 及以上（仪器/Agent 自动录入路径亦走此端点）。

### `GET /api/v1/test-data/{id}`

单条详情。权限：service 校验。

### `PATCH /api/v1/test-data/{id}`

更新单条（仅 `measurement`/`value`/`unit`/`quality`/`measured_at`/`notes` 可改；
`data_type`/`run_id` 不可改，传入 → 400）。需 `Idempotency-Key`。Resp 200：更新后的 `TestData`。审计 `test_data.update`。

### `DELETE /api/v1/test-data/{id}`

标记无效（`quality='invalid'`，非硬删除）。需 `Idempotency-Key`。Resp 200：`{id}`。
权限：admin / 记录人本人 / 项目 owner，否则 403。审计 `test_data.delete`。

### `POST /api/v1/projects/{id}/test-data/batch`

批量录入（**首个数组体写端点，N ≤100，事务原子**：任一失败整批回滚并逐行报错）。

**请求**：JSON 数组（1–100 个元素；每个元素与单条 `TestDataPayload` 同构，
逐元素开启 `DisallowUnknownFields`）。`value` 支持 0 且缺失时报错（`*float64` 语义）。

```json
[
  { "data_type": "cryo", "measurement": "target_temp", "value": 4.2, "unit": "K",
    "quality": "normal", "measured_at": "2026-08-09T10:30:00+08:00",
    "run_id": "70000000-0000-4000-8000-000000000001", "notes": "稳定后读数" },
  { "data_type": "pressure", "measurement": "cell_pressure", "value": 0.013 }
]
```

**Resp 201**：`{ "count": N, "items": [TestData×N] }`（`count == len(items)`，顺序 = 请求行序）。

**错误结构**（`error.details.errors[]`，0-based `index` 指向请求数组下标即表格行序）：

| 字段 | 语义 |
|------|------|
| `error.code` | `validation_failed`（行级）/ `batch_too_large`（超限）/ `bad_request`（结构）/ 既有 403/404/500 码 |
| `error.details.errors[]` | 行级错误：`index`（数组下标）、`field`（data_type/measurement/value/unit/quality/source/measured_at/run_id/notes/body）、`code` ∈ `required`/`not_a_number`/`invalid_enum`/`too_long`/`invalid_uuid`/`run_not_found`/`unknown_field`/`invalid_row`、`message`（中文可展示文案） |
| 排序 | errors 按 index 升序、同 index 内字段序稳定排序；「收集全部错误一次返回」，不遇错即停 |
| 未知字段 | 行级 `unknown_field`（field=未知键名）；元素非对象 → 行级 `invalid_row`（field=body），不中断其余行 |

`not_a_number` 仅服务内构造可触发（JSON 无法表达 NaN/Inf，属 Go 内部防御校验）；
解码失败的行跳过语义校验，只返回 `unknown_field`/`invalid_row`（不叠加假 `required`）。

```json
{ "error": { "code": "validation_failed", "message": "3 行校验失败，请修正后重试",
    "details": { "errors": [
      { "index": 0, "field": "data_type", "code": "invalid_enum", "message": "数据类型不在允许枚举内" },
      { "index": 1, "field": "value",     "code": "required",     "message": "数值必填" },
      { "index": 2, "field": "run_id",    "code": "run_not_found","message": "实验批次不存在" }
    ] } }, "request_id": "req_…" }
```

**状态码总表**：401（未登录）/ 403（无权限）/ 404（项目不存在）/ 400（非数组、空数组、
请求体损坏、缺 `Idempotency-Key`）/ 413（请求体超 512KB，`request_too_large`
`details {max:524288}`）/ 409（幂等键重复）/ 422（超限 `batch_too_large`
`details {max:100, received:N}`，或行级校验失败 `validation_failed`）/ 500（DB 或
run 校验基础设施错误）。校验规则与单条逐行一致（含 `quality`/`source` 默认值、
trim 规范化）；run_id 存在性校验去重并发执行，插入期 FK 竞态违例（SQLSTATE 23503）
回退为行级 `run_not_found`（422），**任何路径不产生部分成功**。422 为全仓库唯一使用点。

**幂等与审计**：与单条同机制（`Idempotency-Key` 防双击/重放 → 409）；审计整批一条
（action `testdata.batch`）：成功 detail `{count, created_ids[]}`，422 失败 detail
`{count, error_rows}`，400 空数组/413 超限 detail `{count, received}`。批量端点仅替代
前端手工录入路径，单条端点（仪器/Agent）行为不变。

## 3.11 经验库模块（已实现）

路由：main.go:475-489。

### `GET /api/v1/experiences`

参数：`project_id`、`status`（缺省 `published`）、`tags`（逗号分隔）、`keyword`、`page`、`per_page`。
响应：`{items, total, page, per_page}`。非 admin/maintainer 查 candidate 只看到自己的候选。

### `POST /api/v1/experiences`

Body：`{project_id?, title, content, tags?, linked_projects?, ai_generated?, agent_task_id?, candidate_id?}`。
响应 201：`Experience`。权限：项目内 member+；`project_id` 为空（全局经验）仅 admin（403 `global_experience_admin_only`）。
非 agent 携带 `ai_generated`/`agent_task_id`/`candidate_id` → 400。

### `POST /api/v1/experiences/candidates`

Agent 生成候选，必须进入人工审核队列。**与 `POST /api/v1/experiences` 是同一 handler**
（experiences/handler.go），是否作为候选完全由请求体 `ai_generated`+`agent_task_id`+`candidate_id` 决定，
且仅 agent 角色可携带。

### `GET /api/v1/experiences/{id}`

详情。candidate 仅作者或 maintainer+ 可读；published 项目内 viewer 可读。

### `PATCH /api/v1/experiences/{id}`

Body：`{title?, content?, tags?, linked_projects?}`；`ai_generated`/`agent_task_id` 不可修改 → 400 `not_candidate` 语义。
权限：作者或项目 maintainer+。

### `POST /api/v1/experiences/{id}/publish`

审核通过并入库。权限：项目 maintainer+；全局仅 admin。错误：400 `not_candidate`、403 `publish_forbidden`。

### `POST /api/v1/experiences/{id}/archive`

归档（仅 published 可归档，否则 400 `not_published`）。权限：项目 **owner**；全局仅 admin。

> 契约历史：旧文档 `POST /api/v1/experiences/{candidate_id}/approve|reject` 路径不存在，
> 由 `/publish` 与 `/archive` 取代（approve/reject 是早期设计）。

## 3.12 附件与 OCR 模块（已实现；OCR 未实现）

路由：main.go:490-504。

### `POST /api/v1/attachments`

`multipart/form-data` 上传图片或文件（`file` 字段 + `entity_type`/`entity_id`/`description` 表单值），上限 100MiB。响应 201：

```json
{
  "data": {
    "attachment": {
      "id": "att_001",
      "original_name": "photo.jpg",
      "sha256": "hex",
      "description": "装配照片",
      "mime_type": "image/jpeg",
      "file_size": 1024
    }
  },
  "request_id": "req_001"
}
```

错误：400（非 multipart/缺 file）、413 `attachment_too_large`（>100MiB）。

### `GET /api/v1/attachments`

参数：`entity_type`、`entity_id`、`page`、`per_page`。响应：`{items, total, page, per_page}`。

### `GET /api/v1/attachments/{id}`

附件元数据。

### `GET /api/v1/attachments/{id}/content`

二进制流下载（`Content-Disposition: attachment`）。**全仓库唯一要求 Idempotency-Key 的 GET**
（handler 内校验，middleware 对 GET 豁免；附件下载属敏感读，审计 `attachments.download`）。

### `POST /api/v1/attachments/{id}/links`

Body：`{entity_type, entity_id, description?}`。响应 201：`AttachmentLink`。错误：409 `attachment_link_exists`。

### `DELETE /api/v1/attachments/{id}/links/{link_id}`

解除绑定。响应 200：`{attachment_id, link_id}`。审计 action 强制 `attachments.remove_link`。

### `DELETE /api/v1/attachments/{id}`

软删除。响应 200：`{id}`。

### `POST /api/v1/attachments/{attachment_id}/ocr`（未实现，预告）

触发 OCR，OCR 文本标记为不可信输入，只能进入 Agent 的数据通道。**代码中无此路由**（main.go 无注册）。

附件绑定对象权限**不走回环 HTTP 回调**：`main.go` 构造期向 attachments 注入窄接口
`attachmentPermissionBridge`（`main_bridges.go`，实现 `attachments.PermissionChecker`），
按实体类型复用各模块既有读路径（logs/issues/assembly/runs/testdata/rfmatch 的 GetByID
等）+ 项目 ACL 判定 read/write（main_bridges.go:321-411）。早期版本的回环
`GET /api/v1/{entity_type}s/{entity_id}/permission-check?user_id=...&action=...`
端点无任何模块实现且回环请求不带认证，属链路断裂 + fail-open（R3），已废弃删除；
服务层对权限检查器未注入/判定失败的路径 fail-closed（`attachments.ErrForbidden`）。

**ask/execute 豁免登记**：`POST /api/v1/ask/execute` 仅接受 SERVICE_TOKEN 鉴权
（handler 先 `IsServiceCall`，用户 JWT 一律 403），调用方身份由请求体 `user_id`
携带（py-agent 内部传参），服务层在只读事务内 `SET LOCAL ROLE ask_reader` 直读
业务表（迁移 033 GRANT SELECT 白名单，禁写/禁 join），与常规用户 JWT + 行级 ACL
认证模型互为隔离，属全库只读登记豁免（详见 AGENTS.md §5 例外小节）。

**R6 语义登记**：改角色（role 实际变化）时 `users.token_version` +1；JWT claims
携带旧 token_version 的 access token 经 `middleware.TokenVersionValidator`
（main.go 构造期接线，比对 `authRepo.GetByID`，停用账号同样立即失效）在
AuthRequired 阶段 401。同值 role、仅改 display_name / language **不递增**。

## 3.13 传感器与 EPICS 模块（已实现；设备推送未实现）

路由：main.go:552-558。

### `GET /api/v1/sensors/latest`

参数：`tags`（可选）。响应：InfluxDB 查询结果（最近读数）。错误：503 `sensor_error`（InfluxDB 不可达）。

### `GET /api/v1/sensors/history`

参数：`tag`（**必填**，缺失 → 400 `bad_request`）、`from`、`to`、`interval`。
错误：503 `sensor_error`。

> 契约历史：旧文档 `POST /api/v1/sensors/data`（设备推送）与 `POST /api/v1/epics/ioc-heartbeat`
> **未实现**（main.go 无注册）。当前时序数据由 IOC 侧直写 InfluxDB
> （py-agent/ioc/hiaf_storage.py，EPICS PV 采样入库），Go 侧只提供读取端点，无设备推送通道。

## 3.14 仪器控制模块（含安全租约与多步流程）

路由：main.go:505-551（instruments）、548-551（ws）。控制类端点限 maintainer/admin；急停/翻译全员可触发。

### `GET /api/v1/instruments`

列出仪器 worker 状态。响应：`[{id, name, state}]`。全员 JWT。

### `GET /api/v1/instruments/whitelist`

仪器命令白名单。响应：`[CommandDef{name, description, risk, scpi, build, timeout_ms, params, returns, result_parser}]`（以 docs/仪器白名单.yaml 为准）。

### `GET /api/v1/instruments/{id}/status`

单仪器状态。响应：`{instrument_id, state, rate_limited}`。404 `instrument_not_found`。

### GasCell 控制对象（已实现，main.go:520-531）

对象权限契约：`object_type=instrument`、`object_id=gascell`、写动作 `control_yellow`；
当前对象授权映射到 `maintainer/admin` 角色，在 middleware 与 service 双重校验（`RequireRole(maintainer, admin)` + service 内 userRole 二次校验）。
所有写接口要求 `Idempotency-Key` 并经过审计中间件。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/instruments/gascell/status` | 聚合 PV 快照 `{pv_name: {v, q}}`，全员 |
| GET | `/api/v1/ws/gascell` | SSE（main.go:548-551），帧为 `{type: snapshot\|update, seq, epoch, data}` + keepalive |
| POST | `/api/v1/instruments/gascell/params` | 写入 `setpoint/kp/ki` 子集 |
| POST | `/api/v1/instruments/gascell/start` / `stop` | 启停 IOC PI |
| POST | `/api/v1/instruments/gascell/valve` | PI 停止时写手动阀位 `{value}` |
| PUT | `/api/v1/instruments/gascell/safety/a5-max` | 修改 A5 上限 `{value}`（**方法是 PUT**） |
| POST | `/api/v1/instruments/gascell/safety/a5-clear` | 清除 A5 锁存 |

每次写入后立即 GET 回读，并按 `gascell-pv-ranges.yaml` 的 `readback_tolerance` 比对。
写入已发送但回读失败或不一致时响应仍成功，同时返回 `warning`，供页面醒目提示并由审计记录。
错误：403（权限不足）、502 `gateway_error`（EPICS 网关）、400 `validation_failed`。

### 仪器命令执行（main.go:536）

### `POST /api/v1/instruments/{instrument_id}/commands`

**限 maintainer/admin**（main.go:536 `RequireRole(maintainer, admin)`）。请求：

```json
{
  "command": "set_sweep_range",
  "params": {
    "start_freq": 3000000,
    "stop_freq": 5000000,
    "points": 401,
    "if_bandwidth": 10000
  }
}
```

响应：

```json
{
  "data": {
    "command": "set_sweep_range",
    "response": "...",
    "duration": 123
  },
  "request_id": "req_001"
}
```

red 风险命令拒绝（400 `command_not_allowed`）；白名单 + NormalizeParams 校验（400 `validation_failed`）；
503 `instrument_unavailable`、502 `command_failed`。yellow 命令还必须携带有效 `lease_id` 与
`approval_id`；green 命令保持无需租约。每次执行在 `command_log` 追加 requested/completed 记录。
> 已知限制（API-only 试点）：前端 UI 暂未接入 yellow 单命令的租约/审批链路，UI 发起的 yellow
> 单命令会得到 403 `instrument_authorization_required`；上述后端 API 链路完整可用，
> 详见 `docs/instrument-security.md` §8.1。

### 租约与审批

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/instruments/{id}/leases` | 申请独占租约；默认 15 分钟，最长 2 小时 |
| POST | `/api/v1/instruments/{id}/leases/{lease_id}/renew` | 携原因续期 |
| POST | `/api/v1/instruments/{id}/leases/{lease_id}/release` | 释放租约 |
| POST | `/api/v1/instruments/{id}/approvals` | 为精确 command + params + lease 请求审批 |
| POST | `/api/v1/instruments/{id}/approvals/{approval_id}/approve` | 非请求者/非 acting user 的 maintainer/admin 审批 |

### Hioki 多步扫频流程

> **灰度开关（M6）**：`INSTRUMENT_FLOW_ENABLED` 默认**关闭**。关闭时下表三个 POST 端点不注册
> （handler 层另有 404 `flow_disabled` 兜底）、FlowRecovery 不启动；GET 进度不受影响。
> **API-only 试点**：流程审批经 API/命令行完成，前端无审批 UI（已知限制见
> `docs/instrument-security.md` §8）。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/instruments/{id}/flows` | 创建 `impedance_frequency_sweep`（202，含 `envelope` 包络）；写入口仅内网 |
| POST | `/api/v1/instruments/{id}/flows/{flow_id}/approve` | 非创建者/非 acting user 的 maintainer/admin 审批包络并异步执行（202，含 `envelope`）；SQL CAS 保证并发只一次 `queued`；写入口仅内网 |
| GET | `/api/v1/instruments/{id}/flows/{flow_id}` | 会话、步骤、进度、`sweep_xy` 结果与**完整审批包络** `envelope`（命令集合含参数区间、频率网格、点数/重试/命令数上限、deadline、白名单版本、审批状态/有效期/审批人、包络 hash、恢复策略）；不含 SCPI 模板 |
| POST | `/api/v1/instruments/{id}/flows/{flow_id}/stop` | 当前原子命令后普通停止；写入口仅内网 |
| POST | `/api/v1/instruments/{id}/manual-check` | maintainer/admin 完成人工检查后解除急停锁；仅内网 |

流程仅支持 Hioki IM3536，Go 生成 linear/log 网格；allowed commands 固定为
`set_frequency`、`measure_single`。每轮 py-agent 只能返回一个 next_command/complete/abort 决策，
Go 对白名单、包络、租约、审批、7 yellow/10s、命令数、deadline 和重试额度逐步复核；
每步 `set_frequency.hz` 必须与 Go 网格点浮点精确相等。命令失败按结构化错误类别
（`timeout`/`rate_limited`/`validation_error`/`communication_error`）判定重试：
仅超时与瞬时硬件通信错误可重试（单点上限 1 次），越权/参数校验/解析失败绝不重试。

### `POST /api/v1/instruments/{instrument_id}/emergency-stop`

**任何已登录成员可触发**（急停设计为全员可用，无角色限制），必须写审计。
响应：`{status: "emergency_stop_queued"}`；异步上报告警中心（critical/instruments）。

### `POST /api/v1/instruments/{instrument_id}/nl-commands`

把自然语言翻译为白名单候选命令，不执行仪器操作。Body：`{input ≤1000 字, history ≤10 条, role∈user/assistant, content≤1000}`，body 上限 32KB。
**全员可调用**；每用户限流 10 次/分钟（429 `rate_limited`）；502 `agent_unavailable`（interpret 降级）。
响应包含 `command`、规范化 `params`、`risk`、`scpi_preview` 和确定性 `validation`。审计 `instrument.nl.translated`。

### `POST /api/v1/instruments/{instrument_id}/nl-execute`

NL 翻译并**直接执行**。**限 maintainer/admin**（handler 内角色检查）。响应：`{status, command, scpi, explanation, response, parsed_value, parsed_points, plot_type, duration_ms, error}`。
错误：403（角色不足）、504（命令执行超时 30s）、503 `instrument_unavailable`。
执行结果写 instrument_results 表。审计 `instrument.nl.executed`。

### `POST /api/v1/instruments/{instrument_id}/parse-result`

只读解析接口（响应解析，不执行）。Body：`{command, response}`（上限 1MB）。
**不需要 Idempotency-Key**（main.go:507-512 独立分组）。错误：400 `parse_failed`。

### Piezo（已废弃，main.go:537-545）

`GET /api/v1/instruments/piezo/status`（全员）、`POST /api/v1/instruments/piezo/start|stop|setpoint`（maintainer/admin）。
**全部响应携带 `Deprecation: true`、`Sunset: Sat, 31 Oct 2026`、`Link: </api/v1/instruments/gascell/status>; rel="successor-version"`**；
替代接口为 GasCell API。

## 3.15 Todolist 模块（已实现）

路由：main.go:559-577。所有写接口需 `Idempotency-Key`，
权限矩阵：添加者(owner) 全权；共享项项目 active 成员可完成（viewer 只读）；非成员不可见（列表不报 403、直查返回 404）。

### `POST /api/v1/todos`

手动添加待办。Body：`{title, priority?, project_id?}`。
`project_id` 非空 = 共享到该项目（校验添加者是项目 active 成员，非成员 403）。
`created_for` = 今天（Asia/Shanghai）。Resp 201：`{id, title, priority, status, source, created_by, created_for, project_id?, issue_id?, completed_at?, completed_by?, created_at, updated_at}`。

### `POST /api/v1/todos/llm-parse`

LLM 自然语言解析为草稿（不落库）。Body：`{raw_text}`（≤2000 字符）。
Resp：`{status: "ok"|"rejected", title, priority, reason?}`；上游失败降级为按清洗后标题保存。
限流 10 次/分钟/用户（与 provision/redeem 共用）。

### `POST /api/v1/todos/llm-add`

确认草稿后落库。Body：`{draft_id?, title, priority?}`（草稿内容前端原样回传，不二次解析）。
Resp 201：同 `POST /todos`，`source=llm`。

### `GET /api/v1/todos`

Params：`date=YYYY-MM-DD`（默认今天）、`scope=all|mine|shared`（默认 `all`）、
`status=open|done|cancelled|all`（默认 `open`，`open` 含 pending+deferred）、`limit=100`（不分页）。
scope 口径：`mine` = `created_by = me`；`shared` = `project_id IN 我的 active 项目`（含我添加的共享项，不含个人项）；
`all` = mine ∪ shared。非成员 `scope=shared` 返回 200 空列表（不断言 403）。
非法 `date`/`scope`/`status` → 400 `bad_request`。
**Resp 200：裸数组**（data 即待办列表，无 items/total 包装；含 `owner_display_name` 与 `updated_at`）。

### `PATCH /api/v1/todos/{id}`

编辑（仅 owner）。Body 带 `updated_at` 乐观锁版本 + `title?/priority?/project_id?`（`project_id` 传空串取消共享）。
乐观锁过期 → 409 `version_conflict`；`updated_at` 缺失 → 400。

### `PATCH /api/v1/todos/{id}/done`

勾选完成。owner 或共享项 active 成员（viewer 403）；状态守卫：仅 pending/deferred 可完成，
已 done 再 done → 409 `state_conflict`。Resp 200：完成后的待办（含 `completed_at`/`completed_by`）。

### `PATCH /api/v1/todos/{id}/defer`

推迟到明天（仅 owner；viewer/成员 403）。仅 pending 可推迟，deferred 再次 defer → 409 `state_conflict`。
`created_for` 直接改写为明日（顺延链不可追溯，已接受）。

### `DELETE /api/v1/todos/{id}`

删除（仅 owner）。Resp 200：`{id}`。不存在/无权限 → 404 `todo_not_found`。

### `GET /api/v1/todos/notification-topic`

只返回当前 JWT 用户的 ntfy topic 与订阅地址（幂等无副作用，不做参数化查询，防枚举）。
Resp：`{topic: "lab-todos-{sha256(user_id)[:16]}", subscribe_url}`。

### `POST /api/v1/todos/notification-topic/provision`

生成一次性 `provision_token`（24h TTL，再次 provision 作废旧 token；同时重置 ntfy 密码）。
Resp：`{provision_token, expires_at}`。限流 10 次/分钟/用户。

### `POST /api/v1/todos/notification-topic/redeem`

兑换 provision_token（一次性，兑换即作废）→ 返回 ntfy 账号与一次性密码。
Body：`{provision_token}`。Resp：`{username: "todo-{username}", password, topic}`。
token 归属绑定（仅签发对象可兑换，冒用尝试即焚毁）；无效/过期/已兑换/跨用户 token → 401 `invalid_provision_token`。

## 3.16 Agent 模块（已实现）

路由：main.go:292-311。tasks 子路由限 agent 角色（main.go:294）；candidates 子路由限 admin/maintainer（main.go:305）。

### `POST /api/v1/agent/tasks/claim`

领取待处理任务（agent 角色；幂等为 handler 级校验，只查 header 存在不查重复）。
Body：`{lease_seconds?}`。响应含 `claim_token`（028）：每次领取轮换，后续 complete/fail 必须携带同一 token，否则 409 `invalid_agent_lease`。

### `POST /api/v1/agent/tasks/{task_id}/complete` / `POST /api/v1/agent/tasks/{task_id}/fail`

agent 角色 + `{id}` 路由挂 QueueTaskContext（claim_token 所有权校验）+ Audit。
请求体含 `claim_token`（028 所有权校验）；complete 另可携带 `raw_text_snapshot`、`report_date`（030 审计链快照，
Go 侧计算 `raw_text_sha256` 落库；旧 worker 缺省时保持 NULL）。
fail 请求体：`{error, claim_token}`。candidates 非空时异步发 ntfy「Agent 待审核」。

### `POST /api/v1/agent/tasks/{task_id}/renew`（R8）

租约续约（agent 角色，幂等 header 照旧）。请求体：`{lease_seconds?, claim_token}`，
`lease_seconds` 缺省 300、范围 30-3600（与 claim 同界）。持有正确 `claim_token` 且任务
`processing`、租约未过期时，把 `lease_expires_at` 推到 `now() + lease_seconds` 并返回任务
（不改状态与 token）。worker 在前置 HTTP 链（get_report + list_issues）与 LLM 调用期间
周期调用（默认间隔 = 租约 1/3，夹 [30s, 90s]），防止全链路最坏耗时超租约导致任务被
重领双跑。错误：404 `agent_task_not_found`、409 `invalid_agent_lease`（token 错 / 租约已
过期被重领）、400 `bad_request`（越界或缺 token）。

### `GET /api/v1/agent/candidates`

admin/maintainer。参数：`status`（pending_review/approved/rejected/executed/execution_failed）、`page`、`per_page`（默认 1/20）。
响应：`{items: [AgentCandidateAction], total, page, per_page}`。

### `GET /api/v1/agent/candidates/{id}/trace`

admin/maintainer。候选全链路溯源（030）：

```json
{
  "candidate": {},
  "task": {
    "model": "deepseek-v4-pro", "prompt_version": "1.0", "agent_confidence": 0.9,
    "raw_text_snapshot": "AI 当时看到的日报原文", "raw_text_sha256": "...", "report_date": "2026-08-08"
  },
  "report": {"id": "...", "report_date": "2026-08-08", "raw_text": "当前值"},
  "result": {"issue_id": "...", "title": "...", "url": "/projects/.../issues/..."},
  "audit": []
}
```

`report` 经 logs 模块注入读取（无权限时降级为 null）；`result` 经 issues/experiences
按 `candidate_id` 反链注入反查；`audit` 经 audit 模块注入读取。030 迁移前完成的
存量任务无快照，`raw_text_snapshot`/`raw_text_sha256`/`report_date` 为 null。

### `POST /api/v1/agent/candidates/{id}/approve` / `POST /api/v1/agent/candidates/{id}/reject`

admin/maintainer。approve 无 body；reject Body：`{reason}`。
错误：404 `candidate_not_found`、409 `candidate_not_pending`。审计 detail 含 candidate_id/action_type/title（reject 另含 review_reason）。

> 契约历史：旧文档 `POST /api/v1/agent/tasks`（创建任务）、`GET /api/v1/agent/tasks/{task_id}`、
> `POST /api/v1/agent/tasks/{task_id}/approve-action` **未实现**——任务由 automation 规则/worker 自行 claim 驱动，
> 不存在人工创建任务与任务级动作批准端点。

## 3.17 审计模块（已实现）

路由：main.go:285-291。仅 admin/maintainer 可查（handler 内 `requireAuditor` 校验 → 403；路由只挂 AuthRequired）。
`audit_log` 自 029 迁移起带 SHA-256 hash 链（`prev_hash`/`hash`）：每条记录的 `hash = sha256(prev_hash|规范化内容)`，
创世块 `prev_hash` 为 64 个 `0`。写入被应用层 advisory lock 串行化，篡改/删行可被 verify 端点检出。
审计路由不挂 Audit 中间件（verify/events 查询不自审计）。
**注册顺序约束**：`/verify`、`/events` 必须先于 `/{request_id}` 注册（main.go:287，chi 静态段优先）。

### `GET /api/v1/audit/{request_id}`

按 request_id 查审计行（029 起响应含 `actor_type`/`acting_user_id`/`agent_task_id`/
`idempotency_key`/`prev_hash`/`hash`）。

响应：

```json
{
  "data": {
    "items": [
      {
        "id": 101, "request_id": "req_20260808_000001", "user_id": "...", "username": "admin",
        "method": "POST", "path": "/api/v1/issues", "action": "issues.create", "status_code": 201,
        "client_ip": "10.0.0.1", "detail": {}, "created_at": "2026-08-08T10:00:00+08:00",
        "actor_type": "user", "acting_user_id": null, "agent_task_id": null,
        "idempotency_key": "...", "prev_hash": "...", "hash": "..."
      }
    ],
    "total": 1
  },
  "request_id": "req_20260808_000002"
}
```

### `GET /api/v1/audit/verify`

全链重算校验，O(n) 单趟。支持 `?from_id=&to_id=` 增量区间（定期抽查用；`from_id`
以该行之前最近一行的 hash 为锚点，缺省 0=不设界）。参数非整数或 from>to → 400。

响应：

```json
{
  "data": {"valid": true, "total": 1234, "checked": 1234, "first_broken_id": null, "message": "链校验通过"},
  "request_id": "req_20260808_000003"
}
```

`valid=false` 时 `first_broken_id` 指向首个断链/篡改行，`message` 说明原因。

### `GET /api/v1/audit/events`

审计列表端点。查询参数：`action`（精确匹配）、`user_id`（UUID，**必须 36 位否则 400**）、`actor_type`
（user/agent/system）、`from`/`to`（RFC3339，`created_at` 区间）、`page`（默认 1）、
`per_page`（默认 20，**上限 100**）。按写入顺序倒序（最新在前）。

响应：

```json
{
  "data": {"items": [/* 同 GET /api/v1/audit/{request_id} 的 Record 结构 */], "total": 1234, "page": 1, "per_page": 20},
  "request_id": "req_20260808_000004"
}
```

## 3.18 自动化规则模块（已实现）

路由：main.go:260-266（admin only，挂 Audit + RequireIdempotencyKey）。
规则表驱动 `daily_report.submitted` 事件的 agent 任务入队（迁移 032 起触发器规则派发，
替代 014 硬编码）。一期边界由 DB CHECK + service 双重白名单锁死：事件仅
`daily_report.submitted`，动作仅 `enqueue_agent_task`；PATCH 仅允许切换 `enabled`。

### `GET /api/v1/admin/automation/rules`

规则列表（含 disabled）。响应：

```json
{
  "data": {
    "items": [
      {
        "id": "...", "name": "日报提交→issue 候选",
        "trigger_event": "daily_report.submitted",
        "action": {"type": "enqueue_agent_task", "mode": "parse_issues"},
        "enabled": true, "created_by": null,
        "created_at": "2026-08-09T10:00:00+08:00", "updated_at": "2026-08-09T10:00:00+08:00"
      }
    ],
    "total": 1
  },
  "request_id": "req_..."
}
```

### `POST /api/v1/admin/automation/rules`

新建规则（Idempotency-Key + 审计）。请求体 `{name, trigger_event, action}`；
`trigger_event`/`action.type` 非白名单值返回 400 `invalid_rule`（DB CHECK 为兜底）。响应 201：`Rule`。

### `PATCH /api/v1/admin/automation/rules/{id}`

仅允许切换 `enabled`（Idempotency-Key + 审计）。请求体 `{"enabled": false}`；
一期不改 `trigger_event`/`action`（避免无 schema 校验的写穿）。规则不存在返回
404 `rule_not_found`。

### `DELETE /api/v1/admin/automation/rules/{id}`

**硬删除**（Idempotency-Key + 审计留痕）。规则不存在返回 404 `rule_not_found`。响应 200：`{id}`。

## 3.19 系统管理模块（系统更新，已实现）

路由：main.go:268-284。仅 admin 角色可访问（路由挂 `RequireRole(admin)`）。更新由 Go 进程触发，执行引擎由
`UPDATE_ENGINE` 环境变量决定：

- `go`（推荐，容器部署默认）：Step 表驱动流水线（`go-server/system/`），派发独立 runner
  容器 `lab-updater-<session>`（复用 server 镜像，挂载 docker.sock 与仓库），server 重启不影响执行；
  runner 入口为镜像内 `lab-update` 二进制（`go-server/cmd/update-runner/`）。
- `shell`（兜底）：同样派发独立 runner 容器，entrypoint 改为 `bash .hermes/update.sh`
  （runner 容器内仓库为可写挂载）。update.sh 也可在宿主机上手工执行。

两种引擎都把日志写入 `UPDATE_LOG_FILE`（默认 `/tmp/lab-update-<session>.log`），结束时写
done marker（`exit_code`/`old_sha`/`new_sha`/`ended_at`），Go 侧 tail 日志文件回传 SSE；
服务重启后可从磁盘 `.log`/`.done`/`.runner` 文件恢复会话。

### `GET /api/v1/admin/system/version`

只读，无审计、无 Idempotency-Key。返回当前与远程版本信息。

响应：

```json
{
  "data": {
    "current": "9af36b7...",
    "current_short": "9af36b7",
    "latest": "abc1234...",
    "latest_short": "abc1234",
    "behind": 3,
    "can_update": true
  },
  "request_id": "req_..."
}
```

git 不可用或网络不可达时，`current`/`latest` 为空字符串、`can_update=false`（降级而非报错）。

### `POST /api/v1/admin/system/update`

写操作：必须带 `Idempotency-Key` 请求头并写审计日志。单例互斥：已有运行中更新时返回
`409 update_in_progress`；更新脚本缺失返回 `500 script_missing`。

响应：

```json
{
  "data": {
    "session_id": "upd_a1b2c3d4e5",
    "current": "9af36b7..."
  },
  "request_id": "req_..."
}
```

### `GET /api/v1/admin/system/update/stream/{sessionId}`

SSE 流式返回指定 session 的更新日志（无审计/幂等）。该端点与其余系统管理接口一样挂
`AuthRequired` + `RequireRole(admin)`：非 admin 用户返回 `403 permission_denied`；
access token 过期返回 `401`，前端应刷新 token 后重新建立 SSE 连接。

`sessionId` 必须是白名单格式
`upd_[a-z0-9]{10}`（如 `upd_a1b2c3d4e5`），否则一律 `404 session_not_found`
（防止把任意 URL 参数拼进文件路径）。session 不在内存时从磁盘 `.log`/`.done` 文件恢复。
单 session 最多 4 个并发订阅，超出返回 `409 too_many_subscribers`。

每帧 `data:` 为 JSON，事件结构：

```json
{
  "seq": 42,
  "ts": "2026-07-31T18:00:00.123456789+08:00",
  "type": "line"
}
```

`type` 取值：

- `line`：普通日志行，附加 `text`。
- `step`：步骤行，附加 `step`、`step_total`、`title`。
- `done`：更新结束，附加 `exit_code`、`success`、`old_sha`、`new_sha`。
- `error`：更新失败/中断/超时，附加 `message`。

`seq` 单调递增，是重连去重的唯一依据：前端按 `seq` 丢弃重复帧，历史回放保证
`done` 事件在长回放后仍可达。

## 3.20 告警中心模块（已实现）

路由：main.go:601-609。数据表 `alerts`（迁移 035）：`level`（info/warning/error/critical）、`source` 枚举
（security/instruments/todos/updater/agent/ioc/watchdog）、`status`（active/resolved）、
`occurrence_count`、`first_seen/last_seen`、`resolved_at/resolved_by`。

**鉴权矩阵**：

| 端点 | 通道 | 鉴权 | CSRF | 幂等 | 审计 |
|------|------|------|------|------|------|
| `POST /alerts/report` | 内部 | SERVICE_TOKEN 白名单 → AuthRequired 放行 | 豁免（IsServiceCall） | 不挂（聚合窗口+唯一索引天然幂等） | Audit，actor_type=system |
| `POST /alerts/resolve` | 用户 | JWT + RequireRoleOrService(admin, maintainer) | 校验 X-CSRF-Token | RequireIdempotencyKey | Audit，actor=user |
| `POST /alerts/resolve` | 内部 | SERVICE_TOKEN 白名单 → AuthRequired 放行 | 豁免（IsServiceCall） | 豁免（IsServiceCall） | Audit，actor_type=system |
| `GET /alerts`、`GET /alerts/{id}` | 用户 | JWT（全员可读） | 无（GET） | 无 | 无 |

安全约束：report/resolve 不挂 AgentContext（拒收 X-Acting-User-ID / X-Agent-Task-ID，
杜绝 agent 冒充）；不提供 DELETE/PATCH 端点（告警只增改，active↔resolved）。

### `POST /api/v1/alerts/report`

仅内部服务可调用（SERVICE_TOKEN；用户 JWT 一律 403）。请求体字段级校验：
`title` ≤256 字符、`detail` ≤2000 字符（防洪）、`level`/`source` 枚举校验、`source+title` 非空。

```json
{ "level": "warning", "source": "watchdog", "title": "lab-server 健康检查失败", "detail": "已连续 3 次探测失败" }
```

**Resp 200**：

```json
{ "data": { "alert_id": "…", "deduplicated": false, "occurrence_count": 1 }, "request_id": "req_…" }
```

- `deduplicated=true` 表示 last_seen 距今 ≤10min 窗口内合并（计数+1，未发 ntfy）；
- 窗口外复发复用 active 行（计数重置 1、清 resolved_at）并重发；
- 同 source+title 任意时刻至多 1 条 active 行（部分唯一索引 `uq_alerts_active_source_title` 为并发防双发最终防线）；
- 401（token 错）、400（枚举/长度校验失败）、403（用户通道）。

### `POST /api/v1/alerts/resolve`

双通道。请求体二选一：`{ "id": "uuid" }`（用户通道，拒绝 source+title 防批量解除）或 `{ "source": "…", "title": "…" }`（内部恢复上报）。

```json
// 用户（admin/maintainer）：{"id": "…"}
// 内部（SERVICE_TOKEN）：{"source": "ioc", "title": "OPC UA 断连 >30s"}
```

**Resp 200**：`{ "data": { "resolved": true }, "request_id": "req_…" }`

- 匹配不到 active 行 → 幂等 success（detail 由审计记录 `matched:false`）；
- `resolved_by`：用户通道记 username，内部通道记 `system`，TTL 兜底记 `ttl`；
- 用户通道：403（非 admin/maintainer）、401（未登录）、400（缺 Idempotency-Key）、
  409（Idempotency-Key 复用）；内部通道：401（service token 无效）；
- resolve 不自动发 ntfy（恢复通知在线化，历史可查）。

### `GET /api/v1/alerts`

全员只读（JWT）。Params：`status=active|resolved`（可选，active 默认按 `last_seen` DESC；
resolved 按 `resolved_at` DESC）、`limit`（默认 50，上限 200）、`offset`（默认 0）。
非法 status / limit<1 / offset<0 → 400。

**Resp 200**：

```json
{ "data": { "items": [ { "id": "…", "level": "warning", "source": "watchdog",
    "title": "lab-server 健康检查失败", "detail": "…", "status": "active",
    "occurrence_count": 3, "first_seen": "…", "last_seen": "…",
    "resolved_at": null, "resolved_by": "" } ],
  "total": 1, "limit": 50, "offset": 0 }, "request_id": "req_…" }
```

### `GET /api/v1/alerts/{id}`

单条详情（不存在或非法 UUID → 404）。

### 维护任务（非 HTTP）

- TTL 兜底：每小时 + 启动立即执行一次，`active 且 last_seen < now()-24h` → 置 resolved（`resolved_by='ttl'`）；
- 90 天滚动清理：每日 04:00（Asia/Shanghai），`resolved 且 resolved_at < now()-90d` → DELETE（active 永不删）；
- 两任务单语句天然幂等，仅影响行数 >0 时写系统审计（action `alerts.ttl`/`alerts.cleanup`，detail 带 count）。

## 3.21 AI 智能查询模块（已实现）

路由：main.go:579-593。chat/history 组：AuthRequired + Audit + RequireIdempotencyKey；
execute 组：仅 AuthRequired + Audit（SERVICE_TOKEN 通道，CSRF/幂等豁免，见 csrf.go:33）。

### `POST /api/v1/ask/chat`

LLM 自然语言 → 结构化 SQL 查询（不落库的直接问答）。Body：`{question}`（1–1000 字）。
响应：`{id, question, answer, sql, table_name, columns, rows, row_count, truncated, duration_ms, created_at}`。
错误：400 `bad_request`、`sql_rejected`；422 `sql_execution_failed`；
429 `rate_limited`（进程内 10 次/分钟/用户）；502 `upstream_error`（interpret 未就绪，AGENTS.md P0-3 降级）。

### `GET /api/v1/ask/history`

参数：`page`（默认 1）、`per_page`（默认 20，**service 层上限 50**，ask/service.go:766-775）。
响应：`{items, total, page, per_page}`（items 不含 rows 大字段）。只查本人（EffectiveUserID）。

### `GET /api/v1/ask/history/{id}`

单条历史（含 rows 快照）。非法 UUID 直接 404（不送 PG 防 500）。仅本人可读。

### `POST /api/v1/ask/execute`

**仅 SERVICE_TOKEN 通道**（service_token.go:37 白名单 + handler 级 IsServiceCall 双保险；用户 JWT → 403）。
只读事务内 `SET LOCAL ROLE ask_reader` 直读业务表（迁移 033 GRANT SELECT 白名单 18 表，
禁写/禁跨表 join/禁多语句——AGENTS.md §5 全库只读例外）。Body：`{sql}`。
响应：`{sql, table_name, columns, rows, row_count, truncated}`。
错误：400（SQL 非法）、422 `sql_execution_failed`、403（非服务调用）、401（SERVICE_TOKEN 无效）。
审计 `actor_type='system'`。

## 3.21.1 周报模块（AI-1，已实现）

路由：main.go（`/api/v1/weekly`）。写接口：AuthRequired + Audit + RequireIdempotencyKey +
RequireRole(admin, maintainer)。定时调度独立于 HTTP：weekly Scheduler 每周日 20:00
（Asia/Shanghai）触发，作者取 `WEEKLY_SUMMARY_AUTHOR_ID`（未配置则跳过并告警；
`WEEKLY_SCHEDULER_ENABLED=false` 可关闭）。生成结果**复用 experiences 表落库**
（global / published / tags=`["weekly_summary"]`，无新表新迁移）；ntfy 推送复用 notify 模块
（主题 `lab-weekly`）。

### `POST /api/v1/weekly/summary`

AI 两步（digest 提炼 → write 成稿）生成周报。Body（均可缺省）：
`{week_start?: "YYYY-MM-DD", notify?: bool}`——`week_start` 必须为周一，缺省取本周一；
`notify` 缺省 true。
流程：幂等查重（同周已存在 → 直接返回复用，不重复调 LLM/落库）→ 取本周
daily_reports（经 logs 模块注入窄接口）与 issue 统计（经 issues 模块注入窄接口，
created/resolved/open_high_critical）→ 调 py-agent `/v1/weekly-summary`（360s 超时）→
落库 experiences（author=当前用户）→ 推 ntfy。
载荷保护：reports 超 100 条或单条超 8000/3000/128 字符时 Go 侧截断
（丢最旧、日志告警），整批请求 ≤480KB，与 py-agent 校验预算 512KB 对齐。
响应：`{id, title, summary, markdown, highlights, problems, data_points, week_start, week_end, reused}`。
错误：400 `invalid_input`（week_start 非周一 / 本周无日报）；502 `upstream_error`
（py-agent 未就绪或 LLM 输出非法）；500 `internal_error`。
历史查询复用 `GET /api/v1/experiences?tags=weekly_summary&status=published`（CLI `weekly recent`）。

## 3.22 通知模块（未实现，预告）

### `POST /api/v1/notifications/events`

> ⚠️ 预告，尚未实现（文档超前于实现）。当前告警由 Go 侧 `notify.Send` 直发 ntfy，
> 无统一事件入口。

内部服务提交告警事件，由通知模块按规则路由到 ntfy 或 MeoW。

请求：

```json
{
  "event_type": "sensor_threshold_exceeded",
  "severity": "critical",
  "subject": "T1 温度超限",
  "body": "T1=42.5C，连续 3 次超过 40C",
  "object_ref": {
    "type": "sensor",
    "id": "T1"
  }
}
```

## 3.23 计划管理模块（未实现，已移除）

> ⚠️ 预告，尚未实现（main.go 无 plans 路由）。早期设计中的计划/任务/里程碑模块
> 未落地；装配、实验运行、步骤模板能力已分别由 §3.6（runs）、§3.7（assembly）、
> §3.8（steptemplates）承担，本模块不再规划。

旧设计端点：`POST/GET /api/v1/plans`、`PATCH /api/v1/plans/{plan_id}`、
`POST /api/v1/plans/{plan_id}/tasks`、`PATCH /api/v1/plans/{plan_id}/tasks/{task_id}`。

## 4. 模块间通信

| 调用方 | 被调用方 | 协议 | 用途 |
|--------|----------|------|------|
| Vue PWA | Go API 网关 | HTTPS REST | 用户操作 |
| LightAgent | Go API 网关 | HTTP 内网 REST | 解析、候选、代用户写入 |
| OCR 服务 | Go API 网关 | HTTP 内网 REST | 附件 OCR 结果写回（**未实现**） |
| Go todos scheduler | Go API 网关 | 内部 HTTP（SERVICE_TOKEN） | by-date 拉取全量用户日报（白名单收敛，service_token.go:36-39） |
| Go todos | py-agent | HTTP 内网 REST | `/v1/todo-add`（15s）/`/v1/todo-daily`（60s）LLM 生成 |
| Go 网关 | py-agent-interpret | HTTP 内网 REST | ask/chat、日志 ai-parse、步骤模板生成、仪器 NL 翻译（interpret 未就绪 → 502 降级，AGENTS.md P0-3） |
| py-agent IOC | InfluxDB | InfluxDB 2.x | EPICS PV 采样批量入库（hiaf_storage.py） |
| Go sensors | InfluxDB | InfluxDB 2.x | 时序数据只读查询 |
| Go API 网关 | ntfy | HTTP | 告警/待办/项目通知推送 |
| watchdog/backup 脚本 | Go API 网关 | 内部 HTTP（SERVICE_TOKEN） | `POST /api/v1/alerts/report` 告警上报（白名单） |

禁止项：

- Agent 直接写 PostgreSQL。
- 仪器服务直接写业务表。
- 传感器推送绕过 API 写库。
- 前端访问数据库、仪器或 Agent 进程。

## 5. 数据依赖关系

| 模块 | 自有数据 | 依赖数据 | 依赖方式 |
|------|----------|----------|----------|
| 认证与用户 | 用户、角色、会话、服务账号 | 无 | 本模块 DB/API |
| 权限 | 对象 ACL、角色授权 | 用户、项目、仪器、报告 | API 读取摘要或本模块授权表 |
| 日报/日志 | 日报、日志、日志附件关联 | 用户、项目、附件、仪器数据 | HTTP API |
| 附件 | 文件元数据 | 用户、日志/Issue 归属 | HTTP API |
| 问题管理 | Issue、评论、状态流转 | 用户、项目、日志、附件 | HTTP API |
| 经验库 | 经验、候选、审核记录 | Issue、日志、用户 | HTTP API |
| 实验运行 | 实验批次、运行步骤、日报关联 | 用户、项目、模板 | HTTP API + 模板注入 |
| 装配 | 装配步骤 | 用户、项目、模板 | HTTP API + 模板注入 |
| 步骤模板 | 模板、模板项 | 用户 | HTTP API |
| RF 匹配 | RF 匹配记录 | 用户、项目 | HTTP API |
| 传感器/EPICS | 时序读数（InfluxDB） | 设备身份、项目映射 | InfluxDB 直读 |
| 仪器控制 | 白名单、命令、结果摘要 | 用户、仪器 ACL | HTTP API + YAML |
| Agent | 任务、候选动作 | 日志、Issue、经验、权限 | HTTP API |
| 通知 | 通知事件、投递记录（**未实现**） | 用户通知偏好、告警规则 | HTTP API |
| Todolist | 待办、共享可见性 | 用户、项目、Issue（只读聚合） | HTTP API + 只读快照（跨模块只读例外已批准，见 `todos/snapshot.go`） |
| AI 智能查询 | ask_history 快照 | 全库只读（ask_reader 角色白名单） | 只读事务直读（AGENTS.md §5 全库只读例外） |
| 审计 | 审计事件 | 所有模块上下文 | 网关注入 + HTTP 写入 |

## 6. API 兼容与演进

- v1 API 字段只增不删；删除字段需先标记 deprecated 至少 2 个小版本。
- 枚举值新增必须由前端按未知值兜底展示。
- 写接口新增必填字段必须提供默认迁移策略。
- Agent 使用的 API 需要额外契约测试，防止提示词变更绕过权限边界。
