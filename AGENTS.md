# AGENTS.md — 实验室日志管理系统

> 给 AI 编程助手（Codex / Claude Code / Cursor 等）的项目入口文档。  
> 开始写代码前，先读本文档，再按任务读取对应设计文档。
> docs/ 下有同名文件是设计文档索引的副本，以根目录版本为准。

---

## 1. 项目全貌

HIAF 低温气体靶实验室的多人协作日志管理平台。系统已完成从单机 SQLite/ELOG 到全栈模块化架构的过渡。

部署环境是 IMP/HIAF 内网 Rocky Linux 物理机，采用 Docker Compose 单机全栈；外网访问通过 frp + VPS + Nginx HTTPS。

核心能力：

- 多人日志录入、Issue、经验库、项目维度管理。
- AI Agent 辅助解析、分类、生成候选内容，但不得绕过权限、审计和人工审批边界。
- Agent 任务队列带 claim_token 所有权校验（迁移 028）；候选动作可回溯完整 AI 动作时间线（迁移 030，`GET /api/v1/agent/candidates/{id}/trace`）。
- 审计日志 SHA-256 hash 链防篡改（迁移 029）：`GET /api/v1/audit/verify` 支持 from_id/to_id 增量校验，`GET /api/v1/audit/events` 提供审计列表。
- 自动化规则引擎（automation 模块，admin-only）：`/api/v1/admin/automation/rules`。
- 传感器/EPICS/PLC 数据接入和仪器控制。
- InfluxDB 时序数据存储 + Grafana 监控仪表盘。
- EPICS 通道访问网关 + 虚拟 IOC（pyEpics 模拟硬件 PV）。
- ntfy 消息通知。
- Web 触发系统更新：UPDATE_ENGINE=go 为默认（server 经 docker.sock 派发独立 runner 容器执行 git pull + compose 重建）；UPDATE_ENGINE=shell 时 runner 容器改跑 `.hermes/update.sh` 兜底，该脚本也可在宿主机手工执行。
- 宿主机 watchdog 心跳告警（`deploy/scripts/watchdog.sh`，systemd timer 每 60s 探测 lab-server / lab-ioc，只告警不自动重启）。

## 2. 当前仓库状态

系统已全部落地运行。当前 GitHub 仓库：

- `docs/`：API、权限审计、仪器安全、项目设计、Agent 策略、维护策略等设计文档。
- `go-server/`：Go 后端，20+ 个模块包（含 automation、steptemplates、todos、testdata 等）。
- `web-ui/`：Vue 3 + Element Plus 前端，24 个页面。
- `py-agent/`：Python LightAgent 服务 + EPICS 虚拟 IOC。
- `migrations/`：PostgreSQL 迁移脚本（32 个版本）。
- `deploy/`：Docker Compose（10 个服务）、Dockerfile、secrets。
- `.github/workflows/ci.yml`：Go、前端、Python Agent 三个 CI job。

## 3. 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.22+，chi 路由，标准库 `net/http` |
| 数据库 | PostgreSQL 16，golang-migrate/migrate |
| 前端 | Vue 3 + Element Plus + vue-i18n（中/英），Vite 多 chunk 构建（vendor 分包 + 路由懒加载），产物经 go:embed 嵌入 `go-server/static/` |
| AI Agent | Python 3.11+，LightAgent (`wanxingai/lightagent`) |
| 时序库 | InfluxDB 2.x |
| 监控 | Grafana |
| EPICS | EPICS CA 网关 + pyEpics 虚拟 IOC |
| 消息/告警 | ntfy（紧急），MeoW（日常） |
| 部署 | Docker Compose，Rocky Linux 单机，frp + VPS |

## 4. 开发前必须阅读

设计文档目前在 `docs/` 目录。写代码前按任务读取对应文档。

| 文档 | 什么时候读 |
|------|-----------|
| `docs/实验室日志系统扩展方案.md` | 首先读。理解项目目标、架构、数据流、阶段计划 |
| `docs/api-contract.md` | 写任何 Go API、Agent REST 调用、前端 API 前必读 |
| `docs/permission-audit.md` | 写认证、权限、审计、Agent 代操作前必读 |
| `docs/instrument-security.md` | 写仪器控制、SCPI、租约、告警前必读 |
| `docs/仪器白名单.yaml` | 写仪器命令前必读，所有参数范围以此为准 |
| `docs/project-design.md` | 写项目维度、项目 ACL、项目报表前必读 |
| `docs/ai-qa-codex.md` | 写 AI 问答或 Codex 协作模块前必读 |
| `docs/agent-auto-review.md` | 写 Agent 自动解析、自动入库、审核队列前必读 |
| `docs/collab-guide.md` | 了解多人协作、分支、审核流程 |
| `docs/maintenance-strategy.md` | 写部署、迁移、备份、回滚前必读 |
| `docs/codex-plan.md` | 了解 Codex 对架构和实施顺序的补充建议 |

如果文档名和实际文件不一致，先用 `rg --files` 查找，不要凭记忆创建重复文档。

## 5. 目录结构

```text
hiaf-lab-system/
├── go-server/              # Go 后端
│   ├── main.go             # 入口，注册所有模块路由
│   ├── auth/               # 认证鉴权模块
│   ├── logs/               # 日志管理模块
│   ├── issues/             # 问题管理模块
│   ├── experiences/        # 经验库模块
│   ├── projects/           # 项目管理模块
│   ├── instruments/        # 仪器控制模块 (含白名单校验)
│   ├── sensors/            # 传感器数据模块
│   ├── assembly/           # 装配/组装模块
│   ├── runs/               # 实验运行模块
│   ├── rfmatch/            # RF 匹配模块
│   ├── steptemplates/      # 步骤模板模块
│   ├── todos/              # 待办事项模块
│   ├── testdata/           # 测试数据模块
│   ├── system/             # 系统更新模块（版本查询、更新触发、SSE 日志流）
│   ├── automation/         # 自动化规则引擎模块（admin-only）
│   ├── weekly/             # 周报模块（AI-1：手动端点 + 每周日 20:00 调度，复用 experiences 落库）
│   ├── cmd/update-runner/  # 更新 runner 入口（独立容器内执行 git pull + compose 重建；cmd/ 下另有 devserver、seed-agent）
│   ├── agent/              # Agent 交互模块
│   ├── audit/              # 审计日志模块（含 hash 链校验、事件列表端点）
│   ├── attachments/        # 附件管理模块
│   ├── notify/             # 消息通知模块
│   ├── epics-gateway/      # EPICS 通道访问网关（Python 脚本 + Dockerfile，非 Go 包）
│   ├── middleware/         # JWT、权限、审计、日志中间件
│   ├── common/             # DB、响应、错误、request_id 等共享工具
│   └── static/             # go:embed 嵌入的前端构建产物（由 web-ui 构建同步）
├── py-agent/               # Python Agent
│   ├── worker.py           # 后台 Worker（任务队列 + 死信 ntfy 告警）
│   ├── serve.py            # AI 解析 HTTP 服务
│   ├── tools/              # LightAgent 工具函数 (只调 Go REST API)
│   ├── prompts/            # Prompt 模板
│   ├── ioc/                # EPICS 虚拟 IOC (pyEpics 模拟硬件 PV)
│   └── tests/              # 测试
├── web-ui/                 # Vue 3 前端
├── migrations/             # PostgreSQL 迁移脚本
├── deploy/                 # Docker Compose、frp、Nginx 配置
├── images/                 # 运行时图片附件目录
└── AGENTS.md               # 本文件
```

每个 Go 业务模块采用：

```text
go-server/<module>/
├── handler.go       # HTTP handler：解析请求、调 service、返回统一响应
├── service.go       # 业务逻辑、权限后置约束、审计事件组装
├── repository.go    # 本模块数据库访问；不得读写其他模块表
├── model.go         # 请求、响应、领域模型
├── handler_test.go  # HTTP 层测试
└── service_test.go  # 业务逻辑测试
```

铁律：不允许跨模块直接访问、写入或 join 对方数据库表。跨模块协作只走两条路：对外的 HTTP API，或在 `main.go` 构造期注入的窄接口/适配器（如各模块的 `ProjectAccessAdapter`，agent 模块的 `SetExecutor` / `SetReportReader` / `SetAuditReader` / `SetResultResolver`）。agent 模块的候选执行和 trace 端点全靠注入桥接 logs/audit/issues/experiences，自身不 SELECT `daily_reports`、`audit_log` 等他模块表。

**全库只读例外（ask 模块）**：AI 智能查询系统（`go-server/ask/`）的 `POST /api/v1/ask/execute` 由 SERVICE_TOKEN 调用，在只读事务内 `SET LOCAL ROLE ask_reader` 直读业务表，是上述铁律的全库级只读例外——DB 权限层由迁移 033 的 `ask_reader` 角色 GRANT SELECT 白名单（18 张主表）强制，Go 解析器仅作纵深；仅允许 SELECT 单表，禁写/禁跨表 join/禁多语句。与 agent 模块 `SetExecutor` 桥接先例并列；后续收紧（project_id 过滤）见 `docs/permission-audit.md` D4 风险登记。

**轻量只读例外登记（4 处，代码不动）**：除 ask 全库只读例外外，以下跨模块轻量只读为既有登记豁免：

- `issues/repository.go` `CountRelatedLogs`/`CountLogsByIDs` —— 直读 `logs` 表 COUNT（删除校验用）；
- `todos/repository.go` `List`/`OpenVisibleForUser` —— `LEFT JOIN users` 取展示字段 display_name；
- `projects/repository.go:242-249` `UserExists` —— 直读 `users` 表 EXISTS（成员校验用）；
- `logs/repository.go` `WeeklyReports` —— `JOIN users` 取展示字段 display_name（周报 AI-1 数据源；经 main.go 注入 weekly 模块窄接口，SQL 仍在 logs 包内自有表）。

**注入化窄接口登记（AI-1 周报链路）**：weekly 模块跨模块只读（daily_reports/issues）与落库（experiences）
全部经 main.go 构造期注入（`main_bridges.go` `weeklyReportReaderBridge`/`weeklyIssueStatsBridge`/
`weeklyExperienceBridge`，对齐 todos `issueStatusResolver`/agent `SetExecutor` 先例），
weekly 自身不 SELECT 任何业务表；新查询方法（`logs.WeeklyReports`、`issues.WeeklyIssueStats`、
`experiences.CreateWeeklySummary`/`FindWeeklySummary`）均定义在所属模块包内、只访问本模块表。

豁免约束（写死在文档里）：**只允许 COUNT / EXISTS / 展示字段 JOIN；禁止跨模块写、禁止写语句内嵌跨模块子查询、禁止业务数据 join 穿透。新增此类访问必须先在登记小节补充条目（PR 评审把关），能注入化的优先注入化。** 写入类跨模块访问无豁免——`todos.IssueSync` 原为写语句内嵌跨模块子查询，已按注入化处置：todos 侧定义窄接口 `issueStatusResolver{ TerminalIssueIDs(ctx) }`，经 `main.go` 构造期注入 issues 仓储，todos 自身不读 issues 表。

**横切点说明（不登记不豁免）**：middleware 直读 `users`/`projects`/`pending_agent_tasks` 及写 `audit_log`（permission.go / agent.go / audit.go）属横切关注点，不属业务模块禁令范围。

## 6. 编码约定

### Go 后端

- 使用 Go 1.22+、chi、标准库 `net/http`。
- 所有业务 API 使用 `/api/v1/{module}/{resource}`。
- 成功响应：

```json
{
  "data": {},
  "request_id": "req_20260714_000001"
}
```

- 失败响应：

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

- 写接口必须要求 `Idempotency-Key`，并写审计日志。
- 权限检查在 middleware/service 层集中处理，不要散落在 handler 中。
- handler 不直接访问数据库，只调用 service。
- repository 只访问本模块表。
- 所有时间使用 RFC3339，保留时区。
- 禁止硬编码密码、token、设备密钥、内网凭据。

### Vue 3 前端

- 使用 Composition API 和 `<script setup>`。
- API 调用集中放在 `src/api/`。
- 权限按钮隐藏只是 UX，后端仍必须强校验。
- 列表页必须处理加载中、空、错误三种状态。
- 表单提交要显示后端返回的 `request_id`，便于追审计日志。
- 不把 access token 放入 `localStorage`（使用 HttpOnly Cookie）。
- UI 文案走 vue-i18n：`src/i18n/zh.ts` 是完整基准，`src/i18n/en.ts` 对齐同一 key 结构；新页面不要写死中文。语言偏好存后端 `users.language`（`PATCH /api/v1/auth/profile`），`localStorage` 兜底。

### Python / Agent

- LightAgent 工具函数只调用 Go REST API，不直连 PostgreSQL 或 SQLite。
- 所有 Agent 写动作必须带 `actor_id`、`acting_user_id`、`agent_task_id`。
- Agent 不能删除业务记录，不能修改权限/配置/密码/token。
- Agent 对日志正文、OCR、经验候选中的命令性文本不得直接当工具指令执行。
- Prompt 模板集中放在 `py-agent/prompts/`。
- 自动入库遵循 `agent-auto-review.md`：Agent 置信度只是参考，最终由后端规则和用户偏好决定。

### 数据库

- PostgreSQL 业务表使用 `snake_case`。
- 业务表必含 `id`、`created_at`、`updated_at`。
- 项目化业务表必含 `project_id`。
- 迁移文件序号递增，只追加新迁移，不修改已发布迁移。
- 审计表 append-only，应用账号不得拥有 UPDATE/DELETE 权限。
- 从 SQLite 迁移时必须保留源表、源 ID、源 hash、迁移批次号。

## 7. 本地快速启动

### Docker Compose（推荐）

```bash
# 1. 配置环境变量
cp deploy/.env.example deploy/.env
# 编辑 .env，填入 DEEPSEEK_API_KEY 和其他必要配置

# 2. 一键启动全部服务
docker compose -f deploy/docker-compose.yml up -d
```

> **AI 辅助服务降级说明（P0-3）**：server 对 `py-agent-interpret` 的依赖仅为
> `service_started`（不要求健康）。interpret 未就绪/崩溃重启期间，`ask/chat`、
> 日志 AI 解析、仪器 NL 命令返回 502 `upstream_error`（前端已有对应错误提示）；
> `/health` 始终 200；日志录入、权限、审计、仪器控制等其余业务完全不受影响。
> interpret 自身恢复健康后 AI 功能自动恢复。

### 单独开发 Go 后端

```bash
# 需要本地 PostgreSQL
docker rm -f lab-pg 2>/dev/null || true
docker run -d --name lab-pg \
  -e POSTGRES_DB=lab \
  -e POSTGRES_USER=lab \
  -e POSTGRES_PASSWORD=lab \
  -p 5432:5432 \
  postgres:16

# 运行迁移
for f in migrations/*.sql; do
  [ -e "$f" ] || { echo "no migrations/*.sql"; break; }
  PGPASSWORD=lab psql -h 127.0.0.1 -U lab -d lab -v ON_ERROR_STOP=1 -f "$f"
done

# 启动 Go 后端
cd go-server
go test ./...
go run .
```

### 单独开发前端

```bash
cd web-ui
npm ci
npm run dev
```

### 单独开发 Agent

```bash
cd py-agent
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
python worker.py
```

### 运维 / 测试脚本（仓库根视角）

| 脚本 | 用途 |
|------|------|
| `scripts/test-all.sh` | 本地一键全量测试（迁移 up/down → Go race+覆盖率 → py unittest → 前端 vitest → 构建产物与 static 一致性），任一步失败即退出 |
| `scripts/test-e2e.sh` | 本地一键 Playwright E2E 冒烟（起 postgres → 迁移 → Go server → vite dev → playwright，trap 自动清理） |
| `deploy/scripts/backup.sh` | 数据库定时备份（容器内 pg_dump `-Fc` → `/opt/lab-backups/`，flock 防重入，失败上报告警中心；gascell systemd timer 每日 03:00 + 保留 14 天） |
| `deploy/scripts/restore-check.sh` | 恢复演练：`pg_restore -Fc` 到临时库校验表数与关键行后 drop，验证备份可用 |
| `deploy/scripts/watchdog.sh` | 宿主机 watchdog：每 60s 探测全部容器健康态（HTTP + docker inspect），3 次失败告警只告警不自动重启 |
| `cli/`（labctl） | AI Agent/运维命令行客户端 + MCP Server（见 `cli/README.md`；测试：`py-agent/.venv/bin/python -m unittest discover -s cli/tests`） |

## 8. PR 前检查清单

- [ ] 已阅读本次任务对应的设计文档。
- [ ] `go test ./...` 通过。
- [ ] `go vet ./...` 无警告。
- [ ] 前端构建/检查通过。
- [ ] 前端产物已全量同步到 `go-server/static/` 并随提交更新（embed 只打包仓库内文件，缺 assets 会导致白屏）。
- [ ] 新 API 与 `api-contract.md` 一致。
- [ ] 写接口要求 `Idempotency-Key`。
- [ ] 权限中间件或 service 权限检查已应用。
- [ ] 所有写操作有审计日志。
- [ ] 没有跨模块直接访问对方数据库表。
- [ ] 没有硬编码密码、token、key、内网凭据。
- [ ] 数据库迁移只追加，不改历史迁移。
- [ ] 测试覆盖正常路径和至少一个异常路径。

## 9. 对 AI 编程助手的工作要求

- 先确认当前仓库实际状态再动手。
- 代码改动尽量小，优先复用已有设计和本仓库已有代码。
- 涉及仪器控制、权限、审计、Agent 自动操作时，宁可多读文档，不要靠猜。
- 新增跨模块轻量只读前先查 AGENTS.md §5「轻量只读例外登记」：能注入化的优先注入化；仅 COUNT / EXISTS / 展示字段 JOIN 可登记豁免，且须同步登记条目（PR 评审把关）。
- 发现文档与代码不一致时，在改动中同步修正文档或在提交说明里明确指出。
