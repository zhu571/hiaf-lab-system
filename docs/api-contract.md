# 模块边界与 API 契约

> 版本：v1  
> 适用范围：实验室日志系统扩展方案 v5

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

### 2.2 通用请求头

| Header | 必填 | 说明 |
|--------|------|------|
| `Authorization: Bearer <access_token>` | 是 | 用户或服务 JWT（内部 service token 仅白名单路径有效，见 §3.9） |
| `X-Request-ID` | 否 | 客户端请求 ID；缺省由网关生成 |
| `Idempotency-Key` | 写接口必填 | 同一用户同一操作 24 小时内去重 |
| `X-Acting-User-ID` | Agent 必填 | Agent 代表的真实用户 |
| `X-Device-ID` | 设备推送必填 | 传感器或 IOC 设备 ID |
| `X-Signature` | 设备推送必填 | HMAC-SHA256 签名 |
| `X-Timestamp` | 设备推送必填 | 毫秒时间戳，允许 5 分钟偏差 |

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

### 2.4 分页、过滤与时间

- 时间字段使用 RFC3339：`2026-07-14T10:30:00+08:00`。
- 列表接口使用 `page_size`、`cursor` 游标分页。
- 删除接口默认软删除，响应仍返回被删除对象 ID。

## 3. 模块 API

## 3.1 认证与用户模块

### `POST /api/v1/auth/login`

请求：

```json
{
  "username": "zhangsan",
  "password": "********"
}
```

响应：

```json
{
  "data": {
    "access_token": "jwt",
    "expires_in": 900,
    "refresh_token": "opaque_refresh_token",
    "refresh_expires_in": 2592000,
    "must_change_password": false,
    "user": {
      "id": "usr_001",
      "name": "张三",
      "roles": ["member"]
    }
  },
  "request_id": "req_001"
}
```

### `POST /api/v1/auth/refresh`

请求：

```json
{
  "refresh_token": "opaque_refresh_token"
}
```

响应：同登录接口，但 refresh token 轮换。

### `POST /api/v1/auth/logout`

撤销当前 refresh token。

### `PATCH /api/v1/auth/profile`

更新当前用户自己的资料。请求：

```json
{
  "language": "zh"
}
```

`language` 仅支持 `zh` / `en`，用于前端界面语言；登录、刷新、`/auth/me` 返回的 user 对象均携带 `language` 字段。非法值返回 `invalid_language`。

### `GET /api/v1/users/me`

返回当前用户资料、角色、对象级权限摘要。

### `POST /api/v1/users`

管理员创建用户。初始密码只返回一次，首次登录强制修改。

## 3.2 日志模块

### `POST /api/v1/logs`

请求：

```json
{
  "project_id": "prj_rf_001",
  "occurred_at": "2026-07-14T10:20:00+08:00",
  "category": "rf_matching",
  "content": "RF 匹配网络 3-5 MHz 扫频通过",
  "attachments": ["att_001"],
  "source": "web"
}
```

响应：

```json
{
  "data": {
    "id": "log_001",
    "status": "created"
  },
  "request_id": "req_001"
}
```

### `GET /api/v1/logs`

查询参数：`project_id`、`category`、`author_id`、`from`、`to`、`cursor`、`page_size`。

### `GET /api/v1/logs/{log_id}`

返回日志正文、附件、关联 issue、关联仪器数据。

### `PATCH /api/v1/logs/{log_id}`

只允许作者、项目管理员、admin 修改。Agent 只能修改自己创建且未人工确认的草稿。

### `DELETE /api/v1/logs/{log_id}`

仅 admin 或项目管理员。Agent 禁止调用。

### `POST /api/v1/logs:parse`

Agent 解析入口，返回候选字段，不直接入库。

请求：

```json
{
  "raw_text": "今天真空 5e-6，RF 匹配通过",
  "attachments": ["att_001"],
  "candidate_project_ids": ["prj_rf_001"]
}
```

响应：

```json
{
  "data": {
    "candidates": [
      {
        "project_id": "prj_rf_001",
        "category": "rf_matching",
        "occurred_at": "2026-07-14T00:00:00+08:00",
        "content": "真空 5e-6，RF 匹配通过",
        "confidence": 0.82,
        "requires_review": false
      }
    ]
  },
  "request_id": "req_001"
}
```

### `POST /api/v1/daily-reports/{id}/ai-parse`

把日报 raw_text 交给 py-agent 整理为结构化日志草稿。**结果不落库**，仅返回给前端由用户逐条编辑确认后走 `POST /api/v1/projects/{id}/logs` 入库。

要求：`Idempotency-Key` 头（组级中间件强制）；仅日报作者本人（admin 除外）且日报为 `draft` 状态；agent 角色被 middleware 白名单拦截（403 `agent_action_forbidden`）。每用户限流 10 次/分钟。

请求体：无（`projects` 由服务端按当前用户 `create_log` 权限注入，不透传前端）。

响应（三态，对齐 py-agent `/v1/daily-parse`）：

```json
{
  "data": {
    "status": "ok",
    "logs": [
      {
        "category": "assembly",
        "project_id": "prj_rf_001",
        "content": "装配匹配电路",
        "occurred_at": "2026-08-06T09:00:00+08:00"
      }
    ],
    "question": null,
    "reason": null,
    "model": "deepseek-v4-pro",
    "prompt_version": "1.0"
  },
  "request_id": "req_001"
}
```

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

## 3.3 附件与 OCR 模块

### `POST /api/v1/attachments`

`multipart/form-data` 上传图片或文件。响应：

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
    },
    "links": []
  },
  "request_id": "req_001"
}
```

其余接口：`GET /api/v1/attachments`、`GET /api/v1/attachments/{id}`、`GET /api/v1/attachments/{id}/content`、`POST /api/v1/attachments/{id}/links`、`DELETE /api/v1/attachments/{id}/links/{link_id}`、`DELETE /api/v1/attachments/{id}`。下载接口也必须携带 `Idempotency-Key` 并写审计。

### `POST /api/v1/attachments/{attachment_id}/ocr`

触发 OCR，OCR 文本标记为不可信输入，只能进入 Agent 的数据通道。

附件绑定对象权限通过 `GET /api/v1/{entity_type}s/{entity_id}/permission-check?user_id=...&action=read|write` 回调目标模块。TODO：各目标模块实现该接口前，`404`/`501` 暂按允许处理；实现完成后删除兼容回退。

## 3.4 问题管理模块

### `POST /api/v1/issues`

请求：

```json
{
  "project_id": "prj_rf_001",
  "title": "RF 匹配在 4.2 MHz 附近反射异常",
  "description": "S11 曲线出现尖峰",
  "severity": "medium",
  "related_log_ids": ["log_001"],
  "assignee_id": "usr_002"
}
```

### `GET /api/v1/issues`

查询参数：`project_id`、`status`、`severity`、`assignee_id`、`cursor`、`page_size`。

### `PATCH /api/v1/issues/{issue_id}`

修改标题、描述、严重度、负责人。

### `POST /api/v1/issues/{issue_id}/comments`

添加评论。

### `POST /api/v1/issues/{issue_id}/transition`

请求：

```json
{
  "target_status": "resolved",
  "reason": "已更换匹配电容并复测通过"
}
```

## 3.5 经验库模块

### `GET /api/v1/experiences`

查询已发布经验。支持 `project_id`、`tag`、`keyword`。

### `POST /api/v1/experiences/candidates`

Agent 生成候选，必须进入人工审核队列。

请求：

```json
{
  "source_issue_ids": ["iss_001"],
  "title": "RF 匹配尖峰排查流程",
  "content": "候选经验正文",
  "tags": ["rf", "matching"]
}
```

### `POST /api/v1/experiences/{candidate_id}/approve`

审核通过并入库。

### `POST /api/v1/experiences/{candidate_id}/reject`

审核拒绝并记录原因。

## 3.6 计划管理模块

### `POST /api/v1/plans`

创建装配、测试或实验计划。

### `GET /api/v1/plans`

按项目、状态、负责人查询。

### `PATCH /api/v1/plans/{plan_id}`

更新计划内容。

### `POST /api/v1/plans/{plan_id}/tasks`

新增任务。

### `PATCH /api/v1/plans/{plan_id}/tasks/{task_id}`

更新任务状态、进度、时间。

## 3.7 传感器与 EPICS 模块

### `POST /api/v1/sensors/data`

设备推送接口。只接受设备 JWT 或 HMAC 签名，不接受普通用户 JWT。

请求：

```json
{
  "device_id": "plc_env_001",
  "sampled_at": "2026-07-14T10:20:00+08:00",
  "readings": {
    "T1": 25.1,
    "T2": 25.3,
    "H1": 42.0
  }
}
```

响应：

```json
{
  "data": {
    "accepted": 27,
    "rejected": 0
  },
  "request_id": "req_001"
}
```

### `GET /api/v1/sensors/latest`

返回最新传感器值。

### `GET /api/v1/sensors/history`

查询参数：`tag`、`from`、`to`、`interval`。

### `POST /api/v1/epics/ioc-heartbeat`

IOC 心跳上报。

## 3.8 仪器控制模块

### `GET /api/v1/instruments`

列出仪器状态、占用租约、互斥锁状态。

### `POST /api/v1/instruments/{instrument_id}/leases`

申请仪器占用租约。

请求：

```json
{
  "purpose": "RF 匹配扫频",
  "duration_seconds": 900
}
```

### `DELETE /api/v1/instruments/{instrument_id}/leases/{lease_id}`

释放租约。

### `POST /api/v1/instruments/{instrument_id}/commands`

## 3.9 Todolist 模块

个人/共享待办 + issue 自动聚合 + LLM 生成 + ntfy 订阅。所有写接口需 `Idempotency-Key`，
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
Resp 200：待办数组（含 `owner_display_name` 与 `updated_at`）。

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

### 内部 service token 调用（白名单收敛）

Scheduler 经 service token 调用 `GET /api/v1/daily-reports/by-date` 拉取全量用户日报：
`Authorization: Bearer <SERVICE_TOKEN>` + `user_id=<uuid>`（可加 `date=` 与 `latest=true` 回溯最近一份非空日报）。
白名单仅此路径；其他路径携带 service token 不生效（交给 JWT 鉴权，→401）。
普通 JWT 调用 by-date 时 `user_id` 参数被忽略、强制取自己（防越权）。
service token 调用产生的审计行 `actor_type='system'`。


执行白名单命令。

请求：

```json
{
  "lease_id": "lease_001",
  "command": "set_sweep_range",
  "params": {
    "start_freq": 3000000,
    "stop_freq": 5000000,
    "points": 401,
    "if_bandwidth": 10000
  },
  "confirm_token": "manual_confirm_token_when_required"
}
```

响应：

```json
{
  "data": {
    "command_id": "cmd_001",
    "status": "completed",
    "result": {}
  },
  "request_id": "req_001"
}
```

### `POST /api/v1/instruments/{instrument_id}/emergency-stop`

紧急停止。任何已登录成员可触发，必须写审计。

### `POST /api/v1/instruments/{instrument_id}/nl-commands`

把自然语言翻译为白名单候选命令，不执行仪器操作。登录用户可调用，仍需
`Idempotency-Key`；每用户最多 10 次/分钟。响应包含 `command`、规范化
`params`、`risk`、`scpi_preview` 和确定性 `validation`。只有后续显式调用
`/commands` 才会执行，yellow 命令仍要求人工确认和 maintainer/admin 权限。

### GasCell 控制对象（v2 MVP）

对象权限契约：`object_type=instrument`、`object_id=gascell`、写动作
`control_yellow`。P3 MVP 先将对象授权映射到 `maintainer/admin` 角色并在
middleware 与 instruments service 双重校验；租约与独立 ACL 后续接入时保持 API
路径和动作名不变。所有写接口要求 `Idempotency-Key` 并经过审计中间件。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/instruments/gascell/status` | 聚合 PV 快照 |
| GET | `/api/v1/ws/gascell` | SSE，帧为 `snapshot/update + seq + epoch + data` |
| POST | `/api/v1/instruments/gascell/params` | 写入 `setpoint/kp/ki` 子集 |
| POST | `/api/v1/instruments/gascell/start` / `stop` | 启停 IOC PI |
| POST | `/api/v1/instruments/gascell/valve` | PI 停止时写手动阀位 |
| PUT | `/api/v1/instruments/gascell/safety/a5-max` | 修改 A5 上限 |
| POST | `/api/v1/instruments/gascell/safety/a5-clear` | 清除 A5 锁存 |

每次写入后立即 GET 回读，并按 `gascell-pv-ranges.yaml` 的
`readback_tolerance` 比对。写入已发送但回读失败或不一致时响应仍成功，同时返回
`warning`，供页面醒目提示并由审计记录。

旧 `/api/v1/instruments/piezo/*` 已废弃，响应携带 `Deprecation: true`、
`Sunset` 与 successor `Link`；替代接口为上述 GasCell API。

## 3.9 Agent 模块

### `POST /api/v1/agent/tasks/claim`

领取待处理任务（agent 角色 + Idempotency-Key）。响应含 `claim_token`（028）：
每次领取轮换，后续 complete/fail 必须携带同一 token，否则 409 `invalid_agent_lease`。

### `POST /api/v1/agent/tasks/{task_id}/complete` / `POST /api/v1/agent/tasks/{task_id}/fail`

agent 角色 + Idempotency-Key。请求体含 `claim_token`（028 所有权校验）；
complete 另可携带 `raw_text_snapshot`、`report_date`（030 审计链快照，
Go 侧计算 `raw_text_sha256` 落库；旧 worker 缺省时保持 NULL）。

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

### `POST /api/v1/agent/tasks`

创建 Agent 任务。

请求：

```json
{
  "task_type": "daily_log_parse",
  "acting_user_id": "usr_001",
  "input_refs": ["log_draft_001"],
  "dry_run": true
}
```

### `GET /api/v1/agent/tasks/{task_id}`

查询任务状态、候选结果、需要人工确认的动作。

### `POST /api/v1/agent/tasks/{task_id}/approve-action`

人工批准 Agent 候选动作。

## 3.10 通知模块

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

## 3.11 审计模块

仅 admin/maintainer 可查（handler 内角色校验）。`audit_log` 自 029 迁移起带
SHA-256 hash 链（`prev_hash`/`hash`）：每条记录的 `hash = sha256(prev_hash|规范化内容)`，
创世块 `prev_hash` 为 64 个 `0`。写入被应用层 advisory lock 串行化，篡改/删行可被
verify 端点检出。审计路由不挂 Audit 中间件（verify/events 查询不自审计）。

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
以该行之前最近一行的 hash 为锚点，缺省 0=不设界）。

响应：

```json
{
  "data": {"valid": true, "total": 1234, "checked": 1234, "first_broken_id": null, "message": "链校验通过"},
  "request_id": "req_20260808_000003"
}
```

`valid=false` 时 `first_broken_id` 指向首个断链/篡改行，`message` 说明原因。

### `GET /api/v1/audit/events`

审计列表端点。查询参数：`action`（精确匹配）、`user_id`（UUID）、`actor_type`
（user/agent/system）、`from`/`to`（RFC3339，`created_at` 区间）、`page`（默认 1）、
`per_page`（默认 20，上限 100）。按写入顺序倒序（最新在前）。

响应：

```json
{
  "data": {"items": [/* 同 GET /api/v1/audit/{request_id} 的 Record 结构 */], "total": 1234, "page": 1, "per_page": 20},
  "request_id": "req_20260808_000004"
}
```

## 3.12 自动化规则模块（C9 规则引擎一期）

仅 admin 角色可访问（路由挂 `RequireRole(admin)`，写操作挂 Audit + Idempotency-Key）。
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
`trigger_event`/`action.type` 非白名单值返回 400 `invalid_rule`（DB CHECK 为兜底）。

### `PATCH /api/v1/admin/automation/rules/{id}`

仅允许切换 `enabled`（Idempotency-Key + 审计）。请求体 `{"enabled": false}`；
一期不改 `trigger_event`/`action`（避免无 schema 校验的写穿）。规则不存在返回
404 `rule_not_found`。

### `DELETE /api/v1/admin/automation/rules/{id}`

硬删除（Idempotency-Key + 审计留痕）。规则不存在返回 404 `rule_not_found`。

## 3.13 系统管理模块（系统更新）

仅 admin 角色可访问（路由挂 `RequireRole(admin)`）。更新由 Go 进程触发，执行引擎由
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

SSE 流式返回指定 session 的更新日志。该端点与其余系统管理接口一样挂
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

## 3.14 测试数据模块

### `GET /api/v1/projects/{pid}/test-data`

按项目分页列出测试数据。Query：`run_id`/`data_type`/`quality`（过滤，均需合法值，
否则 400）、`page`/`per_page`（默认 1/20，per_page ≤100）。默认过滤 `quality='invalid'`。
Resp 200：`{items: TestData[], total, page, per_page}`。权限：项目 viewer 及以上。

### `POST /api/v1/projects/{pid}/test-data`

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

### `PATCH /api/v1/test-data/{id}`

更新单条（仅 `measurement`/`value`/`unit`/`quality`/`measured_at`/`notes` 可改；
`data_type`/`run_id` 不可改，传入 → 400）。需 `Idempotency-Key`。Resp 200：更新后的 `TestData`。

### `DELETE /api/v1/test-data/{id}`

标记无效（`quality='invalid'`，非硬删除）。需 `Idempotency-Key`。Resp 200：`{id}`。
权限：admin / 记录人本人 / 项目 owner，否则 403。

### `POST /api/v1/projects/{pid}/test-data/batch`

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

```json
{ "error": { "code": "validation_failed", "message": "3 行校验失败，请修正后重试",
    "details": { "errors": [
      { "index": 0, "field": "data_type", "code": "invalid_enum", "message": "数据类型不在允许枚举内" },
      { "index": 1, "field": "value",     "code": "required",     "message": "数值必填" },
      { "index": 2, "field": "run_id",    "code": "run_not_found","message": "实验批次不存在" }
    ] } }, "request_id": "req_…" }
```

**状态码总表**：401（未登录）/ 403（无权限）/ 404（项目不存在）/ 400（非数组、空数组、
请求体损坏、缺 `Idempotency-Key`）/ 409（幂等键重复）/ 422（超限 `batch_too_large`
`details {max:100, received:N}`，或行级校验失败 `validation_failed`）/ 500（DB 或
run 校验基础设施错误）。校验规则与单条逐行一致（含 `quality`/`source` 默认值、
trim 规范化）；run_id 存在性校验去重并发执行，插入期 FK 竞态违例（SQLSTATE 23503）
回退为行级 `run_not_found`（422），**任何路径不产生部分成功**。

**幂等与审计**：与单条同机制（`Idempotency-Key` 防双击/重放 → 409）；审计整批一条
（action `testdata.batch`）：成功 detail `{count, created_ids[]}`，422 失败 detail
`{count, error_rows}`。批量端点仅替代前端手工录入路径，单条端点（仪器/Agent）行为不变。

## 4. 模块间通信

| 调用方 | 被调用方 | 协议 | 用途 |
|--------|----------|------|------|
| Vue PWA | Go API 网关 | HTTPS REST | 用户操作 |
| LightAgent | Go API 网关 | HTTP 内网 REST | 解析、候选、代用户写入 |
| OCR 服务 | Go API 网关 | HTTP 内网 REST | 附件 OCR 结果写回 |
| EPICS/PLC 接入 | Go API 网关 | HTTP 内网 REST | 传感器数据和 IOC 心跳 |
| 仪器控制服务 | Go API 网关 | HTTP 内网 REST | 命令结果和审计回写 |
| Go API 网关 | 通知服务 | HTTP 内网 REST | 告警事件路由 |
| Go todos scheduler | Go API 网关 | 内部 HTTP（SERVICE_TOKEN） | by-date 拉取全量用户日报（白名单收敛） |
| Go todos | py-agent | HTTP 内网 REST | `/v1/todo-add`（15s）/`/v1/todo-daily`（60s）LLM 生成 |

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
| 日志 | 日志、日志附件关联 | 用户、项目、附件、仪器数据 | HTTP API |
| 附件/OCR | 文件元数据、OCR 文本 | 用户、日志/Issue 归属 | HTTP API |
| 问题管理 | Issue、评论、状态流转 | 用户、项目、日志、附件 | HTTP API |
| 经验库 | 经验、候选、审核记录 | Issue、日志、用户 | HTTP API |
| 计划管理 | 计划、任务、里程碑 | 用户、项目 | HTTP API |
| 传感器/EPICS | 传感器读数、IOC 心跳 | 设备身份、项目映射 | HTTP API |
| 仪器控制 | 租约、命令、结果摘要 | 用户、仪器 ACL、白名单 | HTTP API + YAML |
| Agent | 任务、候选动作 | 日志、Issue、经验、权限 | HTTP API |
| 通知 | 通知事件、投递记录 | 用户通知偏好、告警规则 | HTTP API |
| Todolist | 待办、共享可见性 | 用户、项目、Issue（只读聚合） | HTTP API + 只读快照（跨模块只读例外已批准，见 `todos/snapshot.go`） |
| 审计 | 审计事件 | 所有模块上下文 | 网关注入 + HTTP 写入 |

## 6. API 兼容与演进

- v1 API 字段只增不删；删除字段需先标记 deprecated 至少 2 个小版本。
- 枚举值新增必须由前端按未知值兜底展示。
- 写接口新增必填字段必须提供默认迁移策略。
- Agent 使用的 API 需要额外契约测试，防止提示词变更绕过权限边界。
