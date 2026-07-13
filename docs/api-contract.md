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
| `Authorization: Bearer <access_token>` | 是 | 用户或服务 JWT |
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

## 3.3 附件与 OCR 模块

### `POST /api/v1/attachments`

`multipart/form-data` 上传图片或文件。响应：

```json
{
  "data": {
    "id": "att_001",
    "path": "photos/2026-07-14/uuid.jpg",
    "sha256": "hex",
    "mime": "image/jpeg"
  },
  "request_id": "req_001"
}
```

### `POST /api/v1/attachments/{attachment_id}/ocr`

触发 OCR，OCR 文本标记为不可信输入，只能进入 Agent 的数据通道。

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

## 3.9 Agent 模块

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

### `GET /api/v1/audit/events`

仅 admin 或安全审计员可查。支持 `actor_id`、`acting_user_id`、`object_type`、`object_id`、`from`、`to`。

## 4. 模块间通信

| 调用方 | 被调用方 | 协议 | 用途 |
|--------|----------|------|------|
| Vue PWA | Go API 网关 | HTTPS REST | 用户操作 |
| LightAgent | Go API 网关 | HTTP 内网 REST | 解析、候选、代用户写入 |
| OCR 服务 | Go API 网关 | HTTP 内网 REST | 附件 OCR 结果写回 |
| EPICS/PLC 接入 | Go API 网关 | HTTP 内网 REST | 传感器数据和 IOC 心跳 |
| 仪器控制服务 | Go API 网关 | HTTP 内网 REST | 命令结果和审计回写 |
| Go API 网关 | 通知服务 | HTTP 内网 REST | 告警事件路由 |

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
| 审计 | 审计事件 | 所有模块上下文 | 网关注入 + HTTP 写入 |

## 6. API 兼容与演进

- v1 API 字段只增不删；删除字段需先标记 deprecated 至少 2 个小版本。
- 枚举值新增必须由前端按未知值兜底展示。
- 写接口新增必填字段必须提供默认迁移策略。
- Agent 使用的 API 需要额外契约测试，防止提示词变更绕过权限边界。
