# 多人助手会话层（assistant）实现说明

> 状态：MVP 已落地（会话 + 消息 + 真实 LLM 回复 + 项目共享 + 配额 + 导出）。
> 设计背景见 `docs/multi-user-agent-upgrade.md`。

## 1. 范围

已实现：

- 私有会话与项目共享会话
- 消息持久化、消息作者
- run 记录（`assistant_runs`）
- py-agent 真实回复（无工具、无技能）
- 项目成员 ACL
- 每日 run 配额与用量查询
- 搜索、重命名、归档/恢复、Markdown 导出

未实现（后续）：

- SSE 流式输出
- tool call 循环与审批中断
- 会话摘要 / 长期记忆
- 附件、引用、分享链接
- 管理员跨用户审计视图（审计链已有请求级记录）

## 2. 数据表

迁移 052 建立 5 张表；迁移 053 给消息加 `user_id`。

| 表 | 用途 |
|---|---|
| `assistant_sessions` | 会话壳：owner、project、title、status、summary |
| `assistant_runs` | 每次提问的 run：status/model/tokens/error |
| `assistant_messages` | user/assistant/tool 消息；`user_id` 记录发送者 |
| `assistant_tool_calls` | 预留给工具调用审计（当前未写） |
| `assistant_usage` | 每用户每日 runs/token/tool 计数 |

约定：

- `assistant_*` 只由 `go-server/assistant/` 访问，不 join 其他模块业务表。
- `project_id`、`user_id`、`candidate_id` 只做 FK，不做跨模块查询。
- 删除采用 `status=archived`，审计链不删。

## 3. API

前缀 `/api/v1/assistant`，全部要求登录；写接口要求 `Idempotency-Key` 并写审计。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/usage` | 当日用量与上限 |
| GET | `/sessions?status=&q=&limit=` | 可见会话（私有 + 项目共享） |
| POST | `/sessions` | 新建；`project_id` 为空=私有 |
| GET | `/sessions/{id}` | 详情 |
| PATCH | `/sessions/{id}` | 改标题/状态（仅创建者） |
| DELETE | `/sessions/{id}` | 归档（仅创建者） |
| GET | `/sessions/{id}/messages` | 消息列表 |
| POST | `/sessions/{id}/messages` | 只追加 user 消息（兼容旧前端） |
| POST | `/sessions/{id}/runs` | 提问并生成回复；返回 run + 本次消息 |
| GET | `/sessions/{id}/export` | Markdown 附件 |

## 4. 权限

- 私有会话：仅 `user_id` 本人可见/可写。
- 项目会话：
  - 项目 active 成员可读会话与消息；
  - 项目成员可继续对话（写 user 消息/触发 run）；
  - 重命名/归档仍限创建者。
- 项目成员校验通过 `ProjectAccessChecker` 注入（`main_bridges` 风格），
  assistant 不直接读 `project_members`。
- 所有响应按 `user_id`/`project_id` 过滤，越权返回 404（不泄露存在性）。

## 5. 回复链路

```text
POST /sessions/{id}/runs
  -> 校验会话访问 + active + 配额
  -> 写 user 消息（记录 user_id）
  -> 创建 assistant_runs(status=running)
  -> 写 assistant_usage.runs += 1
  -> 调 py-agent POST /v1/assistant-reply
       {messages:[{role,content}...]}（最多 20 条、单条 ≤4000 字）
  -> 写 assistant 消息（content_json 可选）
  -> run done；失败 run failed + 502 upstream_error
```

py-agent 侧 `tools/assistant.py`：

- 无工具、无技能、非 thinking 的 LightAgent；
- 所有输入过 `ensure_safe`；
- 超时 120s；
- 未配置 `PY_AGENT_INTERPRET_URL` 时 Go 回退到明确的占位回复。

## 6. 配额

- 默认每用户每日 100 次 run（`defaultDailyRuns`）。
- run 创建时计数，失败尝试也计入，防止刷接口。
- 超限返回 `429 quota_exceeded`。
- `GET /usage` 返回 `runs/run_limit`，前端显示“今日用量”。

## 7. 前端

- `/assistant`，导航“AI 助手”。
- 会话列表支持搜索、显示已归档、共享/归档标记。
- 新建时可选“私有/项目”。
- 消息区显示“我/其他成员/助手”。
- 支持重命名、归档/恢复、导出、错误提示（带 request_id）。

## 8. 测试

- `go test ./assistant`：服务校验、DB 生命周期、Handler 错误映射。
- `py-agent/tests/test_assistant.py`：消息校验器。
- CI 迁移 up/down 覆盖 052/053。
- 未覆盖：真实 LightAgent 调用、SSE、工具循环（需部署环境/API key）。

## 9. 运维

- 迁移：`052_assistant_sessions`、`053_assistant_message_author`。
- 回滚：先停前端入口，再按迁移 down；`assistant_*` 表可整体删除。
- 用量表按日增长，后续可加 180 天清理任务。
- 若 py-agent 不可用：assistant 返回 502，其他业务不受影响。
