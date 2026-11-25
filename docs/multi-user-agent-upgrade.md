# 从记录系统到多人 Agent：GitHub 调研与开发步骤

> 版本：v0.1（2026-11-24）
> 状态：待评审
> 关联：`AGENTS.md`、`docs/ai-qa-assistant.md`、`docs/agent-auto-review.md`、`docs/permission-audit.md`、`docs/api-contract.md`
> 调研数据：GitHub REST API，2026-11-24 查询；star 数只作成熟度参考，会持续变化。

---

## 0. 结论摘要

1. **可以升级，且应做增量改造。** 现有系统的身份、项目 ACL、审计 hash 链、任务队列、候选审批已经是"多人"底座；缺的是**会话状态、工具循环、并发配额**三层，不是权限体系。
2. **不建议替换成 Open WebUI / Dify / FastGPT 这类现成平台。** 它们是"通用 AI 工作台"，没有本项目的项目级 ACL、审计链、候选审批和实验记录语义；替换会引入双套权限、数据出域和许可证问题。
3. **最值得借鉴的四类设计：**
   - 会话即资源（标题、搜索、归档、分享）——Open WebUI / LibreChat；
   - 每用户数据隔离 + scope 授权——Agno AgentOS；
   - session + interrupt 的人工审批中断/恢复——OpenAI Agents SDK / LangGraph；
   - 多用户 ELN 的团队权限与审计——eLabFTW。
4. **推荐形态顺序：** 先做"每人私有助手"（形态 A），再做"项目共享会话"（形态 B），外部接入复用现有 `labctl`/MCP（形态 C）。
5. **工期：** 单人 10–12 周；双人 6–7 周（详见 §11）。最小可用版本（会话 + 只读工具 + 非流式）单人 3–4 周。

---

## 1. 现状基线（代码级）

| 能力 | 现状 | 位置 | 对升级的含义 |
|---|---|---|---|
| 身份与角色 | JWT + `admin/maintainer/member/viewer/agent` | `go-server/auth/`、`middleware/` | 直接复用，无需改 |
| 项目 ACL | 项目成员 + 可见性 + 读写权限 | `middleware/permission.go`、`project_members` | 会话和工具都必须按项目过滤 |
| 审计 | SHA-256 hash 链、增量校验、事件列表 | 迁移 029、`go-server/audit/` | 会话/工具调用要挂进同一条链 |
| Agent 委托 | `X-Acting-User-ID` + `X-Agent-Task-ID`，校验任务行 | `middleware/agent.go` | 只适合队列任务，不适合连续会话 |
| 任务队列 | claim_token + 租约续约 + 死信告警 | 迁移 013/014/028、`py-agent/worker.py` | 可复用所有权模型，但要加公平调度 |
| 候选审批 | `pending_review → approve/reject → executed` | 迁移 014、`go-server/agent/service.go` | 写动作复用这条链路 |
| AI 解析 | 固定流程：claim → 取日报 → 检索 issue → LLM → complete | `py-agent/worker.py:135` | 是 ETL，不是 agent loop |
| AI 问答 | 单轮：plan SQL → execute → integrate | `go-server/ask/service.go:317`、`py-agent/tools/ask.py` | 无会话、无 history、无工具循环 |
| 问答历史 | 每用户一问一答快照，无 session_id | 迁移 033、`ask_history` | 不能当会话存储用 |
| CLI/MCP | 每人自带 token，全部走 REST | `cli/mcp_server.py` | 外部接入路径现成 |

**三个结构性缺口**（上一轮分析的结论，这里作为设计前提）：

1. **没有会话状态**：`Chat` 只发 `question`（`ask/service.go:308-313`）；前端 `AskDialog.vue` 的 `turns` 是组件本地数组，刷新即丢；`ask_history` 没有 thread。
2. **没有工具循环**：`parse.py:52` 的 `LightAgent(tools=[])` + 清空 `ToolRegistry` + `NoToolHook`，模型不能决定下一步调什么。真正的 tool loop 只在 `spike.py:114` 里验证过，没进生产。
3. **没有多用户并发模型**：`worker.run_forever` 一次一个任务；`serve.py` 单进程 uvicorn；`ask/service.go:943` 的限流是进程内 map；`AskEngine._client_lock` 串行化 execute。

---

## 2. GitHub 调研

### 2.1 调研口径

- 只选**可自托管、有明确多用户/权限模型、2026 年仍活跃**的项目；
- 分四类：多用户 AI 工作台、应用/工作流平台、Agent 运行时、实验室记录与自动化；
- 关注点固定为五项：**多用户与权限、会话与记忆、工具与审批、部署形态、许可证**；
- 结论只用于"借鉴设计"，不复制代码。

### 2.2 多用户 AI 工作台

#### Open WebUI（`open-webui/open-webui`，159k★，自定义许可）

- **定位**：自托管 ChatGPT 式界面，支持 Ollama/OpenAI 兼容后端。
- **多用户**：用户/分组、按模型和知识库的访问控制、管理面板；官方文档有"与团队共享"专章。
- **会话**：会话是持久资源（标题、搜索、归档、导入导出）。
- **可借鉴**：**"用户/组 → 资源"的权限矩阵**，可直接映射成"用户/项目 → 会话/工具"；会话列表的交互细节。
- **不适用**：Svelte 前端、通用 RAG，没有项目级 ACL 和实验记录语义。
- **许可**：非标准 OSI 许可（BSD-3 衍生 + 品牌条款，GitHub 标 NOASSERTION，以仓库 LICENSE 为准），**不适合把代码搬进本项目**。

#### LibreChat（`LibreChat-AI/LibreChat`，47k★，MIT）

- **定位**：增强版 ChatGPT 克隆，README 明确列出 Agents、MCP、多用户认证、消息搜索、管理面板。
- **多用户**：邮箱/OAuth/LDAP 多种登录，会话按用户隔离，管理员可管用户和配置。
- **可借鉴**：**"会话即资源"的完整生命周期**（重命名、搜索、分享、fork、删除），以及 Agents 的配置化思路。
- **不适用**：Node/TS 单体，权限模型是"应用级角色"，不是项目级 ACL。
- **许可**：MIT，是四家里最宽松的；可作为交互层参考实现阅读。

#### AnythingLLM（`Mintplex-Labs/anything-llm`，68k★，MIT）

- **定位**：本地优先的 agent 工作台，workspace + RAG + agents + MCP。
- **多用户**：Docker 版提供多用户模式，用户与 workspace 绑定。
- **可借鉴**：**workspace 作为"数据边界"** 的思路，对应本项目的"项目"；多用户模式下"谁能进哪个 workspace"的配置方式。
- **不适用**：以文档 RAG 为中心，不接业务表和审批流。

### 2.3 应用/工作流平台

| 项目 | star | 许可 | 多用户/权限 | 会话 | 工具/工作流 | 结论 |
|---|---|---|---|---|---|---|
| Dify（`langgenius/dify`） | 161k★ | Apache-2.0 + 附加条款（以 LICENSE 为准） | workspace/多租户 | 应用内会话 | 可视化 workflow + agent + RAG + API | 工作流编排参考；不嵌入 |
| FastGPT（`labring/FastGPT`） | 30k★ | 自定义许可 | 多租户 | 应用内会话 | 知识库 + 可视化 workflow | 知识库检索参考；不嵌入 |
| MaxKB（`1Panel-dev/MaxKB`） | 23k★ | GPL-3.0 | 企业级用户/角色 | 应用内会话 | 知识库 + workflow + MCP | 企业权限模型参考；GPL 不复制 |
| RAGFlow（`infiniflow/ragflow`） | 94k★ | Apache-2.0 | 多租户 | 检索会话 | 深度文档解析 + 检索 | 只在"文档解析/检索质量"上参考 |

**共同判断**：这四家都解决"知识库问答 + 工作流编排"，但**没有一家内置"项目级 ACL + 候选审批 + 审计链"**。它们适合当"检索层/编排层"的参考，不适合当"本项目 agent 的底座"。

### 2.4 Agent 运行时（会话/记忆/审批）

#### Agno AgentOS（`agno-agi/agno`，43k★，Apache-2.0）

- **定位**：自托管的 agent 运行时，内置 sessions、memory、knowledge、evals。
- **多用户**：JWT + scopes + `user_isolation=True`；官方文档明确：用户拥有的 session/memory/run 按 `sub` 隔离，写操作强制 `user_id = sub`，admin scope 才可跨用户；**服务账号永远只作用域到自己**。
- **可借鉴**：**"授权（能做什么）"与"数据隔离（能看谁的）"分开建模**——这正是本项目需要的两层；`session_id + user_id` 的所有权校验模式。
- **不适用**：Python 单体运行时，接不进 Go 的权限/审计体系。

#### Letta（`letta-ai/letta`，25k★，Apache-2.0）

- **定位**：有状态 agent 平台，核心是 memory blocks + 自编辑记忆。
- **可借鉴**：**记忆分层**（core memory / archival memory / recall memory）的概念，用于设计"会话摘要 + 长期偏好"。
- **不适用**：它的 memory 是 agent 自管理的，跨用户隔离需要自己兜；本项目不应让 agent 自由写长期记忆。

#### LangGraph（`langchain-ai/langgraph`，45k★，MIT）

- **定位**：有状态图运行时，checkpointer 持久化每一步，`interrupt` 支持人工中断/恢复。
- **可借鉴**：**"运行可中断、状态可恢复"** 的模型——本项目的候选审批可以升级成"会话内 interrupt"：agent 提出写动作 → 挂起 → 用户批准 → 从断点继续。
- **不适用**：引入 LangGraph 平台等于再养一套运行时；用它的"思想"即可。

#### OpenAI Agents SDK（`openai/openai-agents-python`，30k★，MIT）

- **定位**：轻量多 agent 框架。
- **能力**：内置 Sessions（SQLite/Redis/SQLAlchemy 等）、human-in-the-loop interruptions、guardrails、tracing。
- **可借鉴**：**Session 抽象**（历史自动存取、可限制条数、可换后端）与**审批中断**的接口设计；guardrails 分"输入/输出/工具"三层的做法。
- **不适用**：Python SDK，模型绑定 OpenAI 生态；本项目已有 LightAgent。

### 2.5 实验室记录与自动化

#### eLabFTW（`elabftw/elabftw`，1.5k★，AGPL-3.0）

- **定位**：最流行的开源电子实验记录本（ELN），PHP/MySQL。
- **能力**：多用户、团队/权限、审计日志、API、时间戳、实验模板。
- **可借鉴**：**"团队 → 条目"的权限继承**、审计字段设计、条目版本化。
- **不适用**：无 AI agent；AGPL 且技术栈不同，不能替换。

#### Bluesky（`bluesky/bluesky`，245★，BSD-3）

- **定位**：实验编排与数据采集框架（RunEngine、计划、队列、设备抽象）。
- **可借鉴**：**队列/设备租约**模型——多人排队使用仪器时，"谁持有设备、租约多久、超时怎么办"的成熟做法；与本项目 `instruments` 的租约可以对齐。
- **不适用**：面向同步辐射光束线，和 LLM agent 无关。

### 2.6 协议与前端组件

| 组件 | 用途 | 在本项目中的位置 |
|---|---|---|
| MCP（`modelcontextprotocol`） | 工具协议 | 已有 `cli/mcp_server.py`，可作为形态 C 的接入层 |
| assistant-ui（`assistant-ui/assistant-ui`） | React 聊天 UI（流式、工具调用、审批卡） | 仅参考交互；本项目是 Vue 3 |
| Vercel AI SDK（`vercel/ai`） | 流式 + tool calling 的 UI 协议 | 参考 SSE 事件设计，不引入依赖 |
| OpenFGA / Casbin | 关系型/策略型授权 | 参考"关系元组"建模；本项目现有 ACL 够用，暂不引入 |

### 2.7 七条结论

1. **没有"拿来即用"的替代品。** 通用平台缺项目 ACL、审计链、候选审批；ELN 缺 agent；agent 运行时缺业务权限。
2. **最该抄的是"会话即资源"**：Open WebUI/LibreChat 把会话当一等公民（列表、标题、搜索、归档、分享），这是"多人使用"的第一体验差异。
3. **授权与隔离要分开建模**：Agno 的 `authorization`（scope）与 `user_isolation`（数据行）是两层，不能混成一个 `if role == admin`。
4. **审批用"中断/恢复"而不是"事后拒绝"**：LangGraph `interrupt` 与 OpenAI Agents SDK 的 `interruptions` 都证明，写动作应在会话内挂起、等人批准、再继续。
5. **会话存储必须外置**：所有成熟项目都把历史放数据库（SQLite/Postgres/Redis），没有把历史放前端或进程内存的。
6. **平台类项目（Dify/FastGPT/MaxKB/RAGFlow）只做检索与编排参考**：它们的多租户是"应用/知识库"级，和本项目的"项目"级不是一回事。
7. **许可证要提前筛**：AGPL/GPL 项目（eLabFTW、MaxKB）只读参考；Open WebUI/Dify/FastGPT 是自定义许可，不能复制代码。

---

## 3. 目标形态与边界

### 3.1 三种"多人"形态

| 形态 | 定义 | 会话归属 | 权限模型 | 适用阶段 |
|---|---|---|---|---|
| **A. 每人私有助手** | 每个用户有自己的会话列表，agent 以该用户身份查数据 | `user_id` | 现有 ACL + 项目过滤 | MVP，先做 |
| **B. 项目共享会话** | 会话可共享给项目成员，支持 @成员、交接 | `project_id` + 成员 | 项目 ACL + 会话可见性 | 第二阶段 |
| **C. 外部接入** | 其他系统/客户端通过 API/MCP 使用 | 服务账号或用户 token | scope + 服务账号 | 复用现有 labctl |

### 3.2 推荐路线

**A → B → C**。理由：A 的风险最小（不改变现有权限模型），能最快验证"会话 + 工具"是否真的提升效率；B 需要新增共享语义和通知；C 已经有 MCP 底座，等 A/B 稳定后开放。

### 3.3 不做什么

- ❌ 不把 Open WebUI/Dify 嵌进来做前端或后端；
- ❌ 不让 agent 直连 PostgreSQL，不把 DB 凭据交给 py-agent；
- ❌ 不让 agent 自动执行写动作（创建 issue、评论、发经验都必须走候选审批）；
- ❌ 不做"通用聊天"：只回答实验室数据与流程问题；
- ❌ 不在 MVP 引入向量库/长期记忆；先把"最近 N 条 + 滚动摘要"做好。

---

## 4. 目标架构

### 4.1 组件图

```
┌──────────────────── web-ui (Vue 3) ────────────────────┐
│ AssistantPanel（由 AskDialog 升级）                      │
│ 会话列表 · 消息流 · 工具时间线 · 候选审批卡 · 停止生成   │
└──────────────────────────┬──────────────────────────────┘
                           │ JWT + CSRF + Idempotency-Key
                           ▼
┌──────────────── go-server/assistant ────────────────────┐
│ 会话/消息/运行 CRUD · SSE 代理 · 配额 · 工具网关          │
│ 工具网关 = 授权 + 审计 + 固定查询模板                     │
└───────┬──────────────────────────────────┬──────────────┘
        │ 内部 token（SERVICE_TOKEN）       │ 注入窄接口 / HTTP
        ▼                                  ▼
┌──────────── py-agent（无状态） ─────────┐  ┌──── 业务模块 ────┐
│ /v1/assistant/chat                       │  │ logs / issues /  │
│ 模型编排 · 工具选择 · 流式输出           │  │ experiences /    │
└──────────────────┬───────────────────────┘  │ testdata / ...   │
                   │                          └──────────────────┘
                   ▼
              DeepSeek API
```

### 4.2 关键决策（ADR）

| # | 决策 | 理由 | 代价 |
|---|---|---|---|
| ADR-1 | 会话/消息/运行全部存 PostgreSQL，py-agent 无状态 | 多副本、可审计、可迁移、可回放 | 每次请求多一次 DB 读 |
| ADR-2 | Go 是唯一用户面；py-agent 只被 Go 以内部 token 调用；流式由 Go 代理 SSE | 认证/限流/审计集中在一处 | Go 要处理流式转发和断线 |
| ADR-3 | 工具调用必须回 Go 授权 + 审计；py-agent 不直连 DB | 与 `ask_reader` 同一安全模型 | 每次工具多一跳 |
| ADR-4 | 写动作只产出 candidate，人工审批后由 Go 执行 | 复用 `agent_candidate_actions`，保持 `agent-auto-review.md` 边界 | 写操作不是"即时"的 |
| ADR-5 | 数据隔离在 SQL 层强制（项目 ID 注入），不靠 prompt | 提示注入不可信 | 工具实现要逐个带 `project_id` |
| ADR-6 | 交互式与批处理分队列、分 worker | 避免日报解析饿死对话 | 多一组 worker 配置 |
| ADR-7 | MVP 工具用固定查询模板；复杂统计复用 `ask` 的只读执行通道 | 降低 text-to-SQL 风险 | 工具覆盖面先窄后宽 |

### 4.3 一次对话的时序

```
用户输入
  → Go: 校验会话归属 + 配额 + 写 user message
  → Go: 组装上下文（最近 N 条 + 会话摘要 + 用户可见项目）
  → Go → py-agent /v1/assistant/chat（流式）
      → py-agent 选择工具 → 回调 Go /api/v1/assistant/tools/{tool}
          → Go 重新授权（项目 ACL + 工具白名单）→ 执行固定查询
          → 写 assistant_tool_calls + 审计
      → 工具结果回灌模型 → 继续或收尾
  → Go: 落 assistant message + token 用量
  → SSE: delta / tool.call / tool.result / candidate.proposed / message.end
```

---

## 5. 数据模型（迁移编号待定，草案）

> 编号说明：045（幂等）、046（审计月分区）已被修复批占用。本节的 `0XX` 在开工前
> 按当时最大迁移号顺延（预计 051+，见 `docs/architecture-remediation-plan.md` §0.3）。

```sql
-- 0XX: 多人助手会话层（assistant 模块自有表；不读其他模块业务表）
CREATE TABLE assistant_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    project_id      UUID REFERENCES projects(id),      -- 形态 B：项目共享会话
    title           VARCHAR(200) NOT NULL DEFAULT '',
    status          VARCHAR(16) NOT NULL DEFAULT 'active', -- active/archived
    summary         TEXT NOT NULL DEFAULT '',          -- 滚动摘要
    summary_upto    UUID,                              -- 摘要覆盖到的最后一条消息
    last_message_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assistant_sessions_user ON assistant_sessions(user_id, last_message_at DESC);
CREATE INDEX idx_assistant_sessions_project ON assistant_sessions(project_id, last_message_at DESC);

CREATE TABLE assistant_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES assistant_sessions(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    status          VARCHAR(16) NOT NULL DEFAULT 'running', -- running/waiting_approval/done/failed/cancelled
    model           VARCHAR(64) NOT NULL DEFAULT '',
    prompt_version  VARCHAR(32) NOT NULL DEFAULT '',
    input_tokens    INT NOT NULL DEFAULT 0,
    output_tokens   INT NOT NULL DEFAULT 0,
    error           TEXT,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ,
    request_id      VARCHAR(64) NOT NULL DEFAULT ''
);
CREATE INDEX idx_assistant_runs_session ON assistant_runs(session_id, started_at DESC);

CREATE TABLE assistant_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES assistant_sessions(id) ON DELETE CASCADE,
    run_id          UUID REFERENCES assistant_runs(id),
    role            VARCHAR(16) NOT NULL,               -- user/assistant/tool
    content         TEXT NOT NULL DEFAULT '',
    content_json    JSONB,                              -- 结构化片段（工具卡/候选卡）
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_assistant_messages_session ON assistant_messages(session_id, created_at);

CREATE TABLE assistant_tool_calls (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES assistant_runs(id) ON DELETE CASCADE,
    seq             INT NOT NULL,
    tool_name       VARCHAR(64) NOT NULL,
    params          JSONB NOT NULL DEFAULT '{}',
    result_digest   CHAR(64),                            -- 结果快照 hash，不存大结果
    status          VARCHAR(16) NOT NULL,                -- ok/denied/error
    duration_ms     INT NOT NULL DEFAULT 0,
    audit_request_id VARCHAR(64) NOT NULL DEFAULT '',
    candidate_id    UUID REFERENCES agent_candidate_actions(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (run_id, seq)
);

CREATE TABLE assistant_usage (
    user_id     UUID NOT NULL REFERENCES users(id),
    usage_date  DATE NOT NULL,
    runs        INT NOT NULL DEFAULT 0,
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    tool_calls  INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, usage_date)
);
```

**约定：**

- `assistant_*` 归 `go-server/assistant/`，repository 只访问这五张表；`project_id`/`user_id`/`candidate_id` 只做 FK 约束，不 join 业务表。
- 会话与消息删除用 `status=archived` + 定时清理；审计链不删。
- `summary` 由 Go 在 run 结束后异步压缩（超过 token 预算时），py-agent 不写库。
- 保留策略：`archived` 180 天后删除消息正文，保留会话壳与审计；与 `ask_history` 的保留策略对齐。

---

## 6. API 契约草案

### 6.1 用户面（JWT + CSRF + Idempotency-Key）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/v1/assistant/sessions` | 创建会话 `{title?, project_id?}` |
| GET | `/api/v1/assistant/sessions` | 我的会话列表（`status`, `page`, `per_page`） |
| GET | `/api/v1/assistant/sessions/{id}` | 会话详情（含最近 N 条消息） |
| PATCH | `/api/v1/assistant/sessions/{id}` | 改标题/归档 |
| DELETE | `/api/v1/assistant/sessions/{id}` | 软归档 |
| GET | `/api/v1/assistant/sessions/{id}/messages` | 分页历史（`before`, `limit`） |
| POST | `/api/v1/assistant/sessions/{id}/messages` | 发消息；`Accept: text/event-stream` 走 SSE，否则返回 JSON |
| POST | `/api/v1/assistant/runs/{id}/cancel` | 停止生成 |
| GET | `/api/v1/assistant/usage` | 我的当日/当月用量与配额 |

### 6.2 内部工具面（SERVICE_TOKEN）

```
POST /api/v1/assistant/tools/{tool_name}
Authorization: Bearer <SERVICE_TOKEN>
X-Assistant-User-ID: <发起会话的用户>
X-Assistant-Session-ID: <会话 ID>
X-Assistant-Run-ID: <run ID>

{"params": {...}}
```

Go 侧必须重新解析用户、重新查项目权限，**不信任 py-agent 传来的任何权限声明**。

### 6.3 SSE 事件

```
event: message.start   data: {"run_id":"...","message_id":"..."}
event: delta           data: {"text":"..."}
event: tool.call       data: {"seq":1,"tool":"search_issues","params":{...}}
event: tool.result     data: {"seq":1,"status":"ok","digest":"..."}
event: candidate.proposed data: {"candidate_id":"...","action_type":"create_issue","title":"..."}
event: message.end     data: {"run_id":"...","usage":{"input":123,"output":456}}
event: error           data: {"code":"quota_exceeded","message":"..."}
```

断线重连：`Last-Event-ID` 对应 `assistant_messages.id`，服务端从该消息之后补发（MVP 可只保证"不重复、不丢最终答案"）。

### 6.4 错误码

| code | HTTP | 含义 |
|---|---|---|
| `assistant_session_not_found` | 404 | 会话不存在或不属于当前用户 |
| `assistant_run_in_progress` | 409 | 同一会话已有运行中的 run |
| `quota_exceeded` | 429 | 超出用户日配额 |
| `tool_denied` | 403 | 工具不在该用户角色允许列表 |
| `upstream_error` | 502 | py-agent/模型不可用（对齐现有 P0-3 降级语义） |

### 6.5 与 `api-contract.md` 的关系

新增端点必须先补 `docs/api-contract.md` 对应章节，再写实现；本文档只定草案，最终以 `api-contract.md` 为准。

---

## 7. 开发步骤

> 每一步都可独立合并、独立回滚。默认用 feature flag `ASSISTANT_ENABLED` 控制入口。

### S0 决策与脚手架（0.5 周）

**目标**：把"形态、配额、模型、灰度"四件事定下来，避免开发中途返工。

| 任务 | 产物 | 验收 |
|---|---|---|
| 确认形态 A 的范围（哪些工具、哪些角色） | 一页 ADR | 评审通过 |
| 定配额：每用户日 token/请求上限 | `.env` 配置项 + 文档 | 有默认值，可热改 |
| 定模型：主模型/降级模型/超时 | `py-agent/.env.example` | 与现有 `DEEPSEEK_*` 命名一致 |
| 建 feature flag | `ASSISTANT_ENABLED`、`ASSISTANT_PILOT_USERS` | 关闭时零行为变化 |
| 迁移 0XX dry-run | 本地 up/down 通过 | `scripts/test-all.sh` 不受影响 |

### S1 会话层（2 周）

**目标**：能创建会话、发消息、持久化、按用户隔离；先不做工具，模型只基于上下文回答。

**改动清单：**

- `migrations/0XX_assistant_sessions.up.sql` / `.down.sql`
- `go-server/assistant/`：`model.go`、`repository.go`、`service.go`、`handler.go` + 测试
- `go-server/main.go`：注册 `/api/v1/assistant/*`，注入 `ProjectAccessAdapter`
- `py-agent/serve.py`：新增 `/v1/assistant/chat`（非流式版）
- `web-ui/src/api/assistant.ts`、`views/AssistantView.vue`（或升级 `AskDialog`）
- `web-ui/src/config/navigation.ts`：新增入口（`minRole` 缺省）
- `web-ui/src/i18n/zh.ts` / `en.ts`：新增 key

**验收：**

- 用户 A 创建会话，用户 B 的列表里不可见；B 直接 GET A 的 session 返回 404。
- 每条 user/assistant 消息都有审计行（`action=assistant.message.send`）。
- 关闭 flag 后，原有 `ask` 链路零变化。
- Go 单测覆盖：归属校验、分页、归档、非法输入。

### S2 工具层（3 周）

**目标**：从"只会聊天"变成"能查数据、能提候选"。

**工具清单（MVP）：**

| 工具 | 类型 | 背后接口 | 权限 |
|---|---|---|---|
| `search_issues` | read | `GET /api/v1/projects/{id}/issues` | 项目 viewer+ |
| `get_issue_detail` | read | `GET /api/v1/issues/{id}` | 项目 viewer+ |
| `search_logs` | read | `GET /api/v1/projects/{id}/logs` | 项目 viewer+ |
| `get_project_progress` | read | `projects/{id}` + `assembly` + `experiment-runs` 聚合 | 项目 viewer+ |
| `search_experiences` | read | `GET /api/v1/experiences` | 全局/项目可见 |
| `query_test_data` | read | `GET /api/v1/projects/{id}/test-data` | 项目 viewer+ |
| `get_latest_sensors` | read | `GET /api/v1/sensors/latest` | 登录用户 |
| `get_daily_reports` | read | `GET /api/v1/daily-reports` | 本人/团队可见 |
| `get_run_status` | read | `GET /api/v1/experiment-runs/{id}` | 项目 viewer+ |
| `propose_create_issue` | propose | 写 `agent_candidate_actions` | 项目 member+ |
| `propose_add_comment` | propose | 写候选 | 项目 member+（受 comment_policy） |
| `propose_create_experience` | propose | 写候选 | 项目 member+ |

**改动清单：**

- `py-agent/tools/assistant_tools.py`：工具定义（`tool_info` + 校验 + 调 Go）
- `py-agent/tools/assistant.py`：tool loop（复用 `spike.py` 的 guardrail 模式）
- `go-server/assistant/tools.go`：工具网关（授权 + 固定查询 + 审计）
- `go-server/assistant/tool_policy.go`：角色/项目 → 工具白名单
- `docs/api-contract.md`：补内部工具面

**验收：**

- 工具白名单在 Go 侧强制：viewer 调 `propose_*` 返回 403，不依赖 prompt。
- 每次工具调用写 `assistant_tool_calls` + 审计；`trace` 能串起 session → run → tool → candidate。
- 注入语料（`忽略以上指令...`、伪造工具调用、伪造 project_id）全部被拦截。
- 工具返回结果按项目过滤；跨项目查询返回空而不是报错。

### S3 流式与前端（1.5 周）

**目标**：打字机效果、工具时间线、候选卡片、停止生成。

**改动清单：**

- `go-server/assistant/stream.go`：SSE 代理（`http.Flusher`，心跳，超时）
- `web-ui`：消息流组件、工具卡、候选卡、停止按钮、断线重连
- `web-ui/src/components/business/AssistantPanel.vue`

**验收：**

- 首 token < 2s（内网，正常负载）。
- 点"停止"后 run 状态为 `cancelled`，不再产生 token 计费。
- 刷新页面后历史完整；断网 30s 内重连不重复输出。

### S4 运行层（1.5 周）

**目标**：20 人同时用不互相拖垮。

| 任务 | 做法 |
|---|---|
| 公平队列 | `pending_agent_tasks` 已有 `acting_user_id`；claim 用 `FOR UPDATE SKIP LOCKED`，按用户轮转 |
| 交互/批处理分池 | 新增 `queue_class`（`interactive`/`batch`），两个 worker 部署 |
| 分布式限流 | 用 `assistant_usage` 计数替代进程内 map；或引入 Redis（若已有） |
| py-agent 多副本 | uvicorn workers + 每线程 httpx client；去掉 `AskEngine._client_lock` 的全局串行 |
| 超时/取消 | run 级 deadline；取消时通知 py-agent 中断 |

**验收：**

- 20 并发用户压测，p95 < 8s，无 5xx。
- 一个用户刷 100 次不会影响另一个用户的对话。
- 关掉一个 py-agent 副本，请求自动落到另一个（或返回 502 并提示重试）。

### S5 协作与治理（1.5 周）

- 会话共享到项目（形态 B）：`assistant_sessions.project_id` + 可见性 + 成员校验。
- @成员、交接给人、审批 inbox（扩展 `AgentCandidatesView` 为"待我审批"）。
- 通知：复用 ntfy/MeoW；候选生成、审批完成各一条。
- trace 扩展：`GET /api/v1/assistant/sessions/{id}/trace`（admin/maintainer）。

### S6 评估与上线（1 周）

- 固定问题集（30–50 条）：SQL/工具选择正确率、答案引用准确率、注入拦截率。
- 灰度：pilot 用户 → 全员；观察 1 周 token 成本与错误率。
- 文档：`docs/api-contract.md` 定稿、`AGENTS.md` 更新、运维手册补"如何关停 assistant"。

---

## 8. 权限与审计

### 8.1 工具权限矩阵

| 工具类型 | viewer | member | maintainer | admin |
|---|---|---|---|---|
| read（项目内） | ✅ | ✅ | ✅ | ✅ |
| read（全局经验） | ✅ | ✅ | ✅ | ✅ |
| propose 写候选 | ❌ | ✅ | ✅ | ✅ |
| 审批候选（UI，非工具） | ❌ | ❌ | ✅ | ✅ |
| admin 工具（用户管理/配置） | ❌ | ❌ | ❌ | ❌（agent 永不可用） |

### 8.2 审计事件

| action | actor_type | 关键 detail |
|---|---|---|
| `assistant.session.create` | user | `session_id`, `project_id` |
| `assistant.message.send` | user | `session_id`, `run_id`, `chars` |
| `assistant.tool.call` | agent | `session_id`, `run_id`, `tool`, `params_hash`, `status` |
| `assistant.candidate.propose` | agent | `candidate_id`, `action_type`, `project_id` |
| `assistant.run.fail` | agent | `run_id`, `error_code`（脱敏） |

### 8.3 三道防线

1. **输入侧**：`ensure_safe` + 注入正则 + 把日志/issue 正文标为 untrusted（沿用 `parse.py` 的 trusted/untrusted 分区）。
2. **工具侧**：Go 重新授权 + 工具白名单 + 参数 schema + 固定查询模板；禁止 LLM 生成任意 SQL（复杂统计复用 `ask` 的受控执行通道）。
3. **输出侧**：写动作只产候选；候选必须人工审批；审批后由 Go 以审批人身份执行并写审计。

---

## 9. 测试计划

| 层级 | 内容 | 工具 |
|---|---|---|
| Go 单测 | 归属校验、分页、归档、工具策略、配额 | `go test ./...` |
| Go 集成 | 两会话隔离、跨项目工具拒绝、候选审批链路、审计落行 | 测试库 + `httptest` |
| Python 单测 | 工具参数校验、注入拦截、tool loop 终止条件 | `unittest` |
| 前端单测 | 消息流状态机、SSE 重连、候选卡 | vitest |
| E2E | 创建会话 → 提问 → 工具卡 → 提候选 → 审批 | Playwright |
| 安全 | 注入语料、越权 project_id、伪造 run_id、超大输入 | 脚本 + 人工 |
| 性能 | 20/50 并发，p95、错误率、token 成本 | k6 或脚本 |

---

## 10. 风险登记

| 风险 | 影响 | 缓解 |
|---|---|---|
| 提示注入（日志正文含指令） | agent 调错工具/越权 | 输入分区 + 工具白名单 + Go 侧授权，不依赖 prompt |
| 跨项目泄露（摘要/检索） | 数据越权 | 摘要按用户可见范围生成；检索 SQL 强制 `project_id IN (...)`；不共享缓存 |
| 成本失控 | 账单 | 每用户日配额 + 缓存 + 小模型路由 + 停止按钮 |
| 延迟 | 体验差 | 流式首 token；工具并发；超时降级 |
| 单点（py-agent/模型） | 全站 AI 不可用 | 502 降级已有；多副本；队列重试 |
| 审计缺口 | 无法追责 | 工具调用强制写审计；`trace` 覆盖 session → candidate |
| 许可证污染 | 合规 | 只读参考 AGPL/GPL；不复制代码；依赖清单评审 |
| 会话数据隐私 | 合规 | 归档 + 保留期 + 用户可删；管理员不可读他人会话（除审计需要） |

---

## 11. 里程碑与工作量

| 阶段 | 内容 | 单人 | 双人 | 依赖 |
|---|---|---|---|---|
| S0 | 决策与脚手架 | 0.5 周 | 0.5 周 | — |
| S1 | 会话层 | 2 周 | 1 周 | S0 |
| S2 | 工具层 | 3 周 | 1.5 周 | S1 |
| S3 | 流式与前端 | 1.5 周 | 1 周 | S1、S2 |
| S4 | 运行层 | 1.5 周 | 1 周 | S2 |
| S5 | 协作与治理 | 1.5 周 | 1 周 | S3、S4 |
| S6 | 评估与上线 | 1 周 | 0.5 周 | 全部 |
| **合计** | | **11.5 周** | **6.5 周** | |

> 估算前提：1 名熟悉本仓库的 Go + Vue 全栈；不含模型效果调优的反复迭代。

---

## 12. 参考资料

**多用户工作台**

- Open WebUI：https://github.com/open-webui/open-webui ；共享文档：https://github.com/open-webui/docs/blob/main/docs/getting-started/sharing.md
- LibreChat：https://github.com/LibreChat-AI/LibreChat
- AnythingLLM：https://github.com/Mintplex-Labs/anything-llm

**应用/工作流平台**

- Dify：https://github.com/langgenius/dify
- FastGPT：https://github.com/labring/FastGPT
- MaxKB：https://github.com/1Panel-dev/MaxKB
- RAGFlow：https://github.com/infiniflow/ragflow

**Agent 运行时**

- Letta：https://github.com/letta-ai/letta
- Agno：https://github.com/agno-agi/agno ；用户隔离：https://docs.agno.com/agent-os/security/authorization/user-isolation
- LangGraph：https://github.com/langchain-ai/langgraph
- OpenAI Agents SDK：https://github.com/openai/openai-agents-python ；Sessions：https://openai.github.io/openai-agents-python/sessions/

**实验室记录与自动化**

- eLabFTW：https://github.com/elabftw/elabftw
- Bluesky：https://github.com/bluesky/bluesky

**协议与组件**

- MCP：https://modelcontextprotocol.io
- assistant-ui：https://github.com/assistant-ui/assistant-ui
- Vercel AI SDK：https://github.com/vercel/ai

---

## 附录 A 与现有代码的映射

| 现有文件 | 本次改动 | 说明 |
|---|---|---|
| `migrations/` | 新增 0XX | 五张 assistant 表 |
| `go-server/agent/` | 不改 | 候选审批继续由 agent 模块负责 |
| `go-server/ask/` | 保留 | 单轮问答继续可用；assistant 复用其只读执行通道 |
| `go-server/main.go` | 注册 assistant 路由 | 注入 `ProjectAccessAdapter` |
| `py-agent/serve.py` | 新增 `/v1/assistant/chat` | 与现有端点同构（Bearer 内部 token） |
| `py-agent/tools/ask.py` | 复用部分逻辑 | `ensure_safe`、`_json_object`、模型封装 |
| `py-agent/spike.py` | 参考 | tool loop + guardrail 的生产化 |
| `web-ui/src/components/business/AskDialog.vue` | 升级或替换 | 会话列表 + 消息流 |
| `web-ui/src/config/navigation.ts` | 新增入口 | 按角色过滤 |
| `docs/api-contract.md` | 补章节 | 用户面 + 内部工具面 |
| `deploy/docker-compose.yml` | 新增 worker 池 | 交互/批处理分离 |

## 附录 B 术语

| 术语 | 含义 |
|---|---|
| 会话（session） | 一段连续对话，属于一个用户（形态 B 可属于项目） |
| 运行（run） | 一次用户输入触发的完整 agent 执行 |
| 工具（tool） | agent 可调用的受控查询/提案动作 |
| 候选（candidate） | agent 提出的写动作，需人工审批 |
| 摘要（summary） | 对早期消息的压缩，控制上下文预算 |
| 形态 A/B/C | 私有助手 / 项目共享 / 外部接入 |
