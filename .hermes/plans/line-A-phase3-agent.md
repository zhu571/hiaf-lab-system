# 并行线 A：Phase 3 AI Agent（按仓库现状修订）

> 审查日期：2026-07-19（包含当前工作区未提交改动）。
>
> 结论：**不能直接交给 Kimi Code 全量实现。** A-1 已完成一部分，但权限交集、Agent 审计、队列路由、候选审核链路和可复现的 LightAgent 工具调用 spike 仍是 Phase A 的阻塞项。
>
> 日报发送外部模型的保密判断是部署前提，不是代码保证；项目出现保密数据后必须重新评估并加后端门控。

## 1. 审查结果摘要

### 1.1 当前仓库真实状态

| 项目 | 实际状态 | 证据 |
|------|----------|------|
| A-1 migration | 已有 011–013，不是 015–017 | `migrations/011_issues_ai_generated.*.sql`、`012_experiences_ai_generated.*.sql`、`013_pending_agent_tasks.*.sql` |
| Issue AI 字段 | model / create DTO / repository / service 校验已接入 | `go-server/issues/{model,repository,service}.go` |
| Experience AI 字段 | model / create DTO / repository / service 校验已接入 | `go-server/experiences/{model,repository,service}.go` |
| 字段不可变 | Update DTO 不含字段，实际是“忽略未知 JSON”，不是“明确拒绝” | `go-server/issues/model.go`、`go-server/experiences/model.go` |
| `agent` 角色 | 常量和用户表约束已有；专用账号未 provision | `go-server/auth/model.go`、`migrations/001_initial_schema.up.sql` |
| queue API | 表存在，handler/service/repository/路由均不存在 | `migrations/013_pending_agent_tasks.up.sql`、`go-server/main.go` |
| 日报触发队列 | 未实现；提交只更新 `daily_reports` | `go-server/logs/repository.go` |
| acting user | 未处理 `X-Acting-User-ID`，也未做权限交集 | `go-server/middleware/` |
| Agent 审计 | 现有 `audit_log` / middleware 无 `actor_type`、`acting_user_id`、`agent_task_id` | `migrations/001_initial_schema.up.sql`、`go-server/middleware/audit.go` |
| 幂等 | 多数 handler 只检查 header 存在，没有 24h 去重存储 | 各模块 `handler.go` |
| py-agent | 只有 `.gitkeep`、`spike.py` 和一次运行日志，无 worker/API wrapper/requirements | `py-agent/` |
| 部署 | Compose 无 py-agent 服务、Agent 凭据或 ntfy 服务 | `deploy/docker-compose.yml` |
| Phase 4 API | 尚未挂载；Line C 的 011–014 编号已与 A-1 冲突 | `go-server/main.go`、`.hermes/plans/line-C-phase4-data.md` |

当前 `go test ./...` 与 `go vet ./...` 均通过，但只证明现有 Go 代码可编译、现有测试通过，不代表 Agent 链路可用。

### 1.2 方案所称“已有 API”逐条核对

| 方案 API | `main.go` 实际 | 结论 |
|----------|----------------|------|
| `POST /api/v1/daily-reports/today` | 存在 | 可用；写请求要求 `Idempotency-Key` |
| `POST /api/v1/daily-reports/{id}/submit` | 存在 | 可用；尚不入队 |
| `POST /api/v1/projects/{id}/issues` | 存在 | 可用；Agent 目前过不了 acting-user 权限链 |
| `GET /api/v1/projects/{id}/issues` | 存在 | 可用；一次只接收一个 `status`，去重需分别查 `open` / `in_progress` |
| `POST /api/v1/issues/{id}/comments` | 存在 | 可用；仍需 acting-user、审计和幂等落地 |
| `POST /api/v1/experiences` | 存在 | 创建结果本身是 `candidate`；路径与 `api-contract.md` 的 `/experiences/candidates` 不一致 |
| `POST /api/v1/auth/login` | 存在 | 可用 |
| `POST /api/v1/auth/refresh` | 存在 | 可用；当前 access token 固定 15 分钟，不是 Agent 1 小时 |
| `GET /api/v1/daily-reports/{id}` | 不存在 | 必须新增，claim 只返回引用，Agent 再按 acting user 读取日报 |
| `POST /api/v1/agent/tasks/claim` | 不存在 | 必须新增 Go 路由 |
| `POST /api/v1/agent/tasks/{id}/complete` | 不存在 | 必须新增 Go 路由 |
| `POST /api/v1/agent/tasks/{id}/fail` | 不存在 | 必须新增 Go 路由 |

Line C 的 test-data / rf-matching / assembly / runs 路由当前均不存在，不能在 Phase A 代码中假定可调用。

## 2. 必须遵守的设计约束

### 2.1 `docs/permission-audit.md`

- Agent 每次业务写入必须有 `actor_type=agent`、Agent 账号 `actor_id`、`acting_user_id`、`agent_task_id` 和授权来源。
- 有效权限是 `Agent 服务账号 ∩ acting_user 权限 ∩ Agent 硬限制`，不是简单跳过 `project_members`。
- 非 Agent 请求携带 `X-Acting-User-ID` / `X-Agent-Task-ID` 必须拒绝。
- Agent 请求缺少 acting user、task 不属于该 acting user、task 非 `processing` 时必须拒绝。
- 日志正文、OCR、评论等均是不可信数据；只能作为结构化数据输入，不能改变工具白名单。
- 经验发布永远需要人工确认，Agent 不得调用 publish/archive/delete/权限/配置接口。

因此，原方案的“中间件对 agent 显式豁免 project_members”应删除。正确实现是中间件验证 Agent 身份和任务上下文，再以 acting user 做现有项目权限检查，同时审计仍记录真实 actor 为 Agent。

### 2.2 `docs/agent-auto-review.md`

该文档只允许“普通日报”在后端规则、用户偏好等共同满足时自动入库，并明确要求 **Issue、经验候选、仪器命令、导出、删除和批量更新必须人工确认**。

所以本方案修订为：

- Phase A 的 Issue 和经验都先形成 `agent_candidate_actions`，进入审核队列。
- Experience 获批后只能创建 `candidate`，仍由现有 publish 流程另行人工发布。
- assembly / experiment run 状态更新也先进入候选审核，不允许仅凭 LLM `confidence` 直接 PATCH。
- `confidence` 只保存为 `agent_confidence`；Go 后端规则输出 `rule_decision`，最终状态由审核动作决定。

### 2.3 `docs/api-contract.md`

- 所有写接口必须要求并真正执行 `Idempotency-Key` 的 24 小时去重；当前代码大多只检查非空，需要补持久化幂等实现。
- Agent 写入必须带 `X-Acting-User-ID`；本方案再增加 `X-Agent-Task-ID` 以绑定审计和队列任务。
- 经验候选契约路径是 `POST /api/v1/experiences/candidates`；新增该路由并复用现有 create handler，保留 `/experiences` 兼容当前前端。
- API 成功/失败响应继续使用统一 `data/error + request_id` 包装。
- Agent 候选动作需要查询和人工批准接口；不能用 Worker 本地日志替代审核记录。

## 3. 修订后的范围与架构

```text
人工提交日报
  └─ PostgreSQL 同事务生成 pending_agent_tasks（唯一 report_id）
       └─ Agent 账号 claim（原子领取 + lease）
            └─ 按 report_id、acting_user 通过 Go API 读取日报
                 └─ LightAgent 仅输出结构化候选，不直接暴露业务写工具
                      └─ complete(result + prompt/model version)
                           └─ agent_candidate_actions
                                ├─ shadow：仅保存和评估
                                └─ pending_review：前端人工批准/拒绝
                                     └─ 批准后由受限执行器调用既有业务 API
```

Agent 不直连 PostgreSQL / SQLite。Python 只调用 Go REST API；Go 后端独占权限、规则、幂等和审计边界。

### Phase A 输出

| 类型 | Phase A 行为 | 自动执行 |
|------|--------------|----------|
| Issue | 生成候选；查重命中则生成“追加评论”候选，否则生成“创建 Issue”候选 | 否，人工批准 |
| Experience | 生成经验候选动作；批准后调用 `/experiences/candidates` 创建 candidate | 否，且发布仍需再次人工操作 |

### Phase B 输出（等待 Line C）

| 类型 | 行为 | 自动执行 |
|------|------|----------|
| test_data | 生成创建候选 | 否，先人工确认；若未来要自动化须先修改 `agent-auto-review.md` |
| rf_matching | 生成创建候选 | 否 |
| assembly step | 生成状态更新候选 | 否 |
| experiment run | 生成状态更新候选 | 否 |

## 4. A-1 完成情况与缺口

### 已完成

- migration 011：Issue 增加 `ai_generated`、`agent_task_id`。
- migration 012：Experience 增加同名字段。
- migration 013：建立最小 `pending_agent_tasks` 表。
- Issue / Experience 模型、创建请求、SQL scan/insert 已接字段。
- service 已拒绝“非 Agent 角色传 `ai_generated=true`”。
- 更新 DTO 不包含两个 AI 字段，因此 repository 不会修改它们。

### A-1 尚未闭环

- 非 Agent 仍可传 `agent_task_id`；必须一并拒绝。
- Agent 可传 `ai_generated=true` 但缺少 `agent_task_id`；必须拒绝并验证任务存在、状态和 acting user。
- PATCH 携带 `ai_generated` / `agent_task_id` 会被 JSON decoder 静默忽略，不符合“返回 400”；相关 handler 要显式拒绝未知/不可变字段。
- migration 013 没有 status CHECK、`UNIQUE(report_id)`、acting user、lease、重试时间、完成时间和结果/prompt/model 元数据。
- `ON DELETE CASCADE` 会切断审计回溯；后续 migration 应改为 `ON DELETE RESTRICT`（生产数据迁移前确认没有依赖删除逻辑）。
- A-1 迁移是当前未提交工作区内容；合并前仍需在真实 PostgreSQL 上执行 up/down 验证。

这些补充使用下一个 migration `014_agent_runtime_hardening`，不要修改已完成的 011–013。Line C 必须把原计划的 011–014 重编号到 015 以后。

## 5. Queue API 契约（新增 Go 路由）

### `POST /api/v1/agent/tasks/claim`

- 仅 `role=agent`。
- 使用 `FOR UPDATE SKIP LOCKED` 原子领取 `pending` 或到期可重试的 `failed` 任务。
- 设置 `processing`、`claimed_at`、`lease_expires_at`，并增加 `attempts`。
- 返回 `task_id`、`report_id`、`acting_user_id`；不在 queue repository join 日报表。
- Worker 崩溃后，lease 到期的任务可重新领取。

### `POST /api/v1/agent/tasks/{id}/complete`

- 仅允许当前 Agent 完成仍处于 `processing` 且 lease 有效的任务。
- 请求保存结构化候选、`model`、`prompt_version`、token/延迟等最小评估元数据。
- shadow mode 也必须持久化候选，不能只写进程日志。
- complete 与候选动作落库同事务；重复 complete 由 `Idempotency-Key` 返回首次结果。

### `POST /api/v1/agent/tasks/{id}/fail`

- 记录截断/脱敏后的错误。
- `attempts < max_attempts` 时为可重试 `failed` 并设置 `next_attempt_at`；达到上限转 `dead` 并告警。
- 禁止把日报原文、JWT、API key 写入 `last_error`。

### 还需新增

- `GET /api/v1/daily-reports/{id}`：使用 acting user 权限返回日报和关联日志。
- `GET /api/v1/agent/candidates`：审核列表。
- `POST /api/v1/agent/candidates/{id}/approve`：人工批准，写审批审计。
- `POST /api/v1/agent/candidates/{id}/reject`：人工拒绝，保存理由。
- `POST /api/v1/experiences/candidates`：复用现有创建 candidate 的逻辑。

## 6. Agent 身份、权限与审计

### 账号与 token

- 创建专用禁用交互式前端登录的 `agent@system` 账号，角色 `agent`。
- 当前代码统一签发 15 分钟 access token；要对齐 `permission-audit.md` 的 Agent 1 小时 token，需新增 Agent 专用签发策略和测试，或先修订权限文档后继续用 15 分钟。不得在方案里声称现状已经是 1 小时。
- Worker 使用账号凭据登录并轮换 refresh token。Compose secret 应是 `/run/secrets/agent_password`，**不能把 JWT 签名密钥或预生成 JWT 暴露给 py-agent**。

### 请求上下文

业务请求由 Worker 带：

```text
Authorization: Bearer <agent access token>
X-Acting-User-ID: <日报作者>
X-Agent-Task-ID: <claim 返回的任务 ID>
Idempotency-Key: <稳定、可重试的动作键>
```

中间件验证 task 与 acting user 的绑定，再把 acting user 用于既有项目 ACL；审计必须保留 Agent 为真实 actor。

### 审计最小改动

`audit_log` 及 middleware 至少补：`actor_type`、`actor_id`、`acting_user_id`、`agent_task_id`、`idempotency_key`、结果和脱敏 detail。Agent 业务写入若不能形成审计，不得返回成功。

## 7. Shadow mode 不是“零成本”

`shadow_mode=true` 只能保证“不改业务表”，不能保证零成本或零配套：

- 仍产生 DeepSeek token 费用、网络延迟和数据出域。
- 仍需 queue DB 写入、候选结果持久化、任务状态和 Agent 审计。
- 仍需记录模型、prompt 版本和人工评估标签，否则 2–4 周后无法计算准确率。
- 需要最小前端审核页展示候选、来源、规则判定并允许标注正确/错误；仅在开发期可用受限的管理员 JSON 列表临时代替。
- shadow 期间绝不调用 Issue/Experience/Line C 的业务写 API。

因此前端改动不是开闸后的可选装饰，而是人工确认和效果评估的依赖。建议新增 `web-ui/src/views/AgentCandidatesView.vue`、`web-ui/src/api/agent.ts` 和对应路由；Issue/Experience 列表还需显示 `ai_generated` 标记，但只在真正执行获批候选后出现。

## 8. LightAgent + DeepSeek V4 Pro 可行性

### 官方能力

- DeepSeek 官方已提供 `deepseek-v4-pro`，支持 OpenAI ChatCompletions、思考/非思考模式；正确参数是 `thinking` / `reasoning_effort` 以官方文档为准，不能沿用旧 `deepseek-reasoner` 假设。
- LightAgent 官方支持自定义 `base_url` 和 Python tools，组合在接口层面可行。

### 当前仓库的实际 spike

- `py-agent/spike.py` 使用 `LightAgent 0.9.4` 和 `deepseek-v4-pro`，现有日志证明模型请求成功。
- 同一日志明确显示自定义 `create_issue` 未进入可用工具列表，反而加载了 `execute_python_*`、`upload_file_to_oss` 等内置工具。
- 因此 S-1 **未通过**。在问题定位并禁止所有未审计内置工具前，不得进入主 Worker 实现。

### S-1 通过标准

1. 固定 `lightagent`、`openai` 版本，生成可复现的 `requirements.txt`。
2. 工具列表只包含本次任务白名单；Python 执行、文件上传、动态 tool generation、memory/self-learning 默认关闭。
3. mock 工具确实被调用一次，参数 schema 正确；未提供的工具绝不出现。
4. prompt injection 样例不能扩大工具集合或触发额外调用。
5. 分别验证 non-thinking 与 thinking；先用 non-thinking 做结构化解析基线，只有质量数据证明需要时再启用 reasoning，避免无谓成本和超时。
6. 超时、429、5xx、非法 JSON、工具参数错误均能变成可重试 `fail`，且日志不泄露原文和密钥。

参考：

- <https://api-docs.deepseek.com/zh-cn/news/news260424/>
- <https://github.com/wanxingai/LightAgent>

## 9. 去重与候选执行

- 使用现有 Issue 列表 API 的 `search` 做 `ILIKE` 预筛；分别请求 `status=open` 和 `status=in_progress`，合并后最多取 10 条。
- LLM 只返回“同一问题 / 非同一问题 + 证据”，不能自行执行写操作。
- 命中时生成“追加评论”候选，未命中时生成“创建 Issue”候选。
- 唯一稳定动作键由 `task_id + action_type + candidate_index` 生成，作为幂等键并加数据库唯一约束。
- embedding、可换 dedup interface、few-shot 学习本期删除；当真实规模或误判数据证明 `ILIKE + LLM` 不够时再加。

“近期人工调整 severity 作为 few-shot”当前也不可实现：现有 audit 没有可靠的 before/after severity 历史。该功能移到后续阶段，前置是结构化变更审计和只读样本 API。

## 10. Go 后端改动清单（按原 12 项复核后修订）

| 原 # | 原说法 | 审查结论与具体动作 |
|------|--------|--------------------|
| 1 | migration 015 Issue 字段 | **已由 011 完成**；不要重复建列 |
| 2 | migration 016 Experience 字段 | **已由 012 完成**；不要重复建列 |
| 3 | migration 017 queue 表 | **已由 013 建最小表**；新增 `migrations/014_agent_runtime_hardening.*.sql` 补约束、acting user、lease、结果元数据和候选动作表 |
| 4 | Issue model/DTO | **已完成**；`go-server/issues/handler.go` 增加不可变字段显式 400，service 校验 task ID |
| 5 | Experience model/DTO | **已完成**；同上修改 `go-server/experiences/handler.go` / `service.go` |
| 6 | service 仅 Agent 可传 AI 标记 | **部分完成**；补 `agent_task_id`、acting user、task 状态绑定和测试 |
| 7 | 日报提交同事务 INSERT | **未实现且有模块表边界冲突**；用 migration 014 的 PostgreSQL trigger/outbox 在 `draft→submitted` 时同事务插入，避免 logs repository 直接写 agent 表；加集成测试 |
| 8 | queue API | **未实现**；新增 `go-server/agent/{model,repository,service,handler}.go` 并在 `go-server/main.go` 挂三条路由 |
| 9 | agent@system provisioning | **未实现**；新增显式部署初始化命令/一次性 seed，不在通用测试数据 migration 硬编码生产密码 |
| 10 | agent 豁免项目成员 | **删除原做法**；修改 `go-server/middleware/` 实现 agent ∩ acting user ∩ 硬限制 |
| 11 | acting user + 审计 | **未实现**；migration 014、`go-server/middleware/audit.go`、JWT/Agent context middleware 一起完成 |
| 12 | 日报质检 API | **Phase A 删除**；当前 `quality_status` 是提交校验，不是 AI 质检。确有产品需求时另立 migration/API/UI 任务 |

原清单还漏了以下阻塞改动：

| 新 # | 必需改动 | 文件 |
|------|----------|------|
| 13 | `GET /daily-reports/{id}`，按 acting user 返回源日报/日志 | `go-server/logs/{handler,service}.go`、`go-server/main.go` |
| 14 | candidate list/approve/reject API 与审批审计 | `go-server/agent/*`、`go-server/main.go` |
| 15 | `/experiences/candidates` 契约兼容路由 | `go-server/main.go`、`go-server/experiences/handler_test.go` |
| 16 | 真正的幂等去重，不只是 header 非空检查 | `migrations/014_agent_runtime_hardening.*.sql`、`go-server/middleware/` |
| 17 | shadow 审核/评估页面和 AI 标记展示 | `web-ui/src/api/agent.ts`、`web-ui/src/views/AgentCandidatesView.vue`、现有 Issue/Experience view |
| 18 | py-agent Compose 服务、healthcheck、Agent 密码 secret、告警接入 | `deploy/docker-compose.yml`、`deploy/secrets/` 文档；不得提交 secret 实值 |
| 19 | Line C migration 重编号，避免 011–014 冲突 | `.hermes/plans/line-C-phase4-data.md` |

## 11. 项目结构（最小可交付）

```text
py-agent/
├── main.py
├── prompts/
│   └── parse.txt
├── tools/
│   └── api.py
├── tests/
│   └── test_worker.py
├── requirements.txt
└── .env.example
```

- 删除 `review.txt` 和独立 `dedup.py` 的预设；Phase A 没有 AI 质检，去重先放在一个小函数里，变复杂后再拆。
- `.env.example` 只列变量名，不含 token、密码、内网地址实值。
- Worker 只负责 claim → fetch report → parse → complete/fail；获批候选执行可在同一进程用严格白名单轮询，不引入 Redis、Celery 或多 Agent。

## 12. 修订后的实施顺序

### Gate 0：先修设计和编号

| Step | 产出 | 阻塞关系 |
|------|------|----------|
| G-1 | 本文修订完成；Line C migration 改为 015+ | 所有新 migration |
| G-2 | 明确 candidate action 审批产品流程；确认 Phase A 不自动创建 Issue/经验 | 后端 API、前端 |

### Gate 1：可复现 spike

| Step | 产出 | 阻塞关系 |
|------|------|----------|
| S-1 | 修复自定义工具注册，禁用内置危险工具，满足第 8 节通过标准 | 全部 py-agent 主流程 |

### Phase A1：后端安全与队列基础

| Step | 产出 | 前置 |
|------|------|------|
| A-1 | 保留已完成 011–013；补 PostgreSQL up/down 实测 | G-1 |
| A-2 | migration 014：queue hardening、candidate actions、Agent audit/idempotency 字段 | A-1 |
| A-3 | Agent 专用账号/token 策略；acting-user/task middleware；权限交集 | A-2 |
| A-4 | queue claim/complete/fail + lease/retry/dead + 路由 | A-3 |
| A-5 | 日报提交触发 outbox + `GET /daily-reports/{id}` | A-2 |
| A-6 | Issue/Experience AI 字段严格校验 + `/experiences/candidates` | A-3 |
| A-7 | candidate list/approve/reject + 审批审计 + 幂等执行 | A-4, A-6 |

### Phase A2：Worker、shadow 与人工审核

| Step | 产出 | 前置 |
|------|------|------|
| A-8 | 最小 py-agent 结构、固定依赖、HTTP/JWT 客户端 | S-1, A-4 |
| A-9 | 主循环与结构化 parse prompt；所有业务工具默认关闭 | A-8 |
| A-10 | `ILIKE` 预筛 + LLM 去重判定，输出候选而非写业务 | A-9 |
| A-11 | shadow 持久化、候选审核页、正确/错误标注 | A-7, A-10 |
| A-12 | E2E：权限交集、审计、幂等、lease 重领、重复日报、prompt injection、批准/拒绝 | A-11 |
| A-13 | Compose 服务、secret、healthcheck、dead task ntfy 告警 | A-12 |

### Phase B：等待 Line C 后再做

| Step | 产出 | 前置 |
|------|------|------|
| B-1 | Line C 四类 API、权限、审计和契约测试完成 | Line C |
| B-2 | parse schema 扩展到 6 类，仍只生成 candidate actions | B-1, A-12 |
| B-3 | test_data / rf_matching / assembly / runs 的批准执行器 | B-2 |
| B-4 | 全流程联调和新的 shadow 评估 | B-3 |

严重程度 few-shot、embedding、自动执行非日志动作均不在本期范围；有真实评估数据和设计文档变更后再立项。

## 13. 验收标准

- `go test ./...`、`go vet ./...` 通过；新增正常路径和至少一个越权/重试异常路径。
- migration 011–014 在 PostgreSQL 16 上 up/down 实测；不运行破坏性测试 seed 验证生产 migration。
- `main.go` 中三条 queue 路由、日报详情路由、candidate 审核路由真实存在。
- Agent 不带 acting user/task、伪造 acting user、越权项目、非 processing task 均被拒绝并审计。
- 同一日报只产生一个 queue task；同一动作重试不产生重复 Issue/Experience/comment。
- shadow 只写任务/候选/审计表，不写业务表，且能按 prompt/model 版本统计人工标注结果。
- prompt injection 测试证明日志正文不能启用 Python、文件、网络或未列入白名单的工具。
- Experience 只能创建 candidate，Agent 永远不能 publish/archive。
- Worker 不持有数据库凭据或 JWT 签名密钥；仓库不提交 Agent 密码/API key。

## 14. 一句话结论

**方案已有可用的 A-1 数据字段基础，但在 queue 路由、acting-user 权限交集、Agent 审计/幂等、候选人工审核和失败的 LightAgent 工具 spike 修好前，不能直接交给 Kimi Code 全量实现；应按 Gate 0 → Gate 1 → Phase A1 顺序分批交付。**

## 设计文档参考

- `docs/agent-auto-review.md`
- `docs/permission-audit.md`
- `docs/api-contract.md`
- `AGENTS.md`
- `.hermes/plans/A-1-codex-task.md`
- `.hermes/plans/line-C-phase4-data.md`
