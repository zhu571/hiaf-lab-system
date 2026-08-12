# HIAF 实验室日志管理系统

HIAF 低温气体靶实验室的多人协作日志管理平台。

`go-server/`、`web-ui/`、`py-agent/`、`migrations/`、`deploy/` 已全部落地运行。系统包含 Go API 后端、Vue 3 前端、Python Agent、PostgreSQL 数据库、InfluxDB 时序库、Grafana 监控、EPICS 网关、虚拟 IOC、ntfy 消息通知等模块，通过 Docker Compose 一键部署。

核心能力：

- 多人日志录入、Issue、经验库、项目维度管理，写操作带幂等键和审计日志。
- AI Agent 辅助解析、分类、生成候选内容（不绕过权限、审计和人工审批边界）；Agent 任务队列带 claim_token 所有权校验，候选动作可回溯完整 AI 动作时间线。
- 审计日志 SHA-256 hash 链防篡改，`GET /api/v1/audit/verify` 支持增量校验。
- 自动化规则引擎（admin-only）：按事件触发自动动作，见 `go-server/automation/`。
- 仪器控制遵循 SCPI 白名单 + 租约 + 人工确认；宿主机 watchdog 心跳告警只告警、不自动重启。
- InfluxDB 时序数据存储 + Grafana 监控仪表盘；EPICS 通道访问网关 + 虚拟 IOC。

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.22+，chi 路由，标准库 `net/http` |
| 前端 | Vue 3 + Element Plus + TypeScript，Vite 多 chunk 构建（vendor 分包 + 路由懒加载），产物经 go:embed 嵌入后端 |
| 数据库 | PostgreSQL 16，golang-migrate/migrate |
| 时序库 | InfluxDB 2.x |
| 监控 | Grafana |
| AI Agent | Python 3.11+，LightAgent (`wanxingai/lightagent`) |
| EPICS | EPICS CA 网关 + pyEpics 虚拟 IOC |
| 消息通知 | ntfy (紧急)，MeoW (日常) |
| 部署 | Docker Compose，Rocky Linux 单机，frp + VPS |

## 项目结构

```text
hiaf-lab-system/
├── go-server/          # Go 后端
│   ├── main.go         # 入口，注册所有模块路由
│   ├── auth/           # 认证鉴权
│   ├── logs/           # 日志管理
│   ├── issues/         # 问题管理
│   ├── experiences/    # 经验库
│   ├── projects/       # 项目管理
│   ├── instruments/    # 仪器控制 (含白名单校验)
│   ├── sensors/        # 传感器数据
│   ├── assembly/       # 装配/组装
│   ├── runs/           # 实验运行
│   ├── rfmatch/        # RF 匹配
│   ├── steptemplates/  # 步骤模板
│   ├── todos/          # 待办事项
│   ├── testdata/       # 测试数据
│   ├── system/         # 系统更新 (版本查询、更新触发、SSE 日志流)
│   ├── automation/     # 自动化规则引擎 (admin-only)
│   ├── agent/          # Agent 交互
│   ├── audit/          # 审计日志 (含 hash 链校验)
│   ├── attachments/    # 附件管理
│   ├── notify/         # 消息通知
│   ├── epics-gateway/  # EPICS 通道访问网关 (Python 脚本 + Dockerfile)
│   ├── middleware/     # JWT、权限、审计中间件
│   ├── common/         # 共享工具 (DB、响应、错误、request_id)
│   ├── cmd/            # 辅助入口 (update-runner / devserver / seed-agent)
│   └── static/         # go:embed 嵌入的前端构建产物
├── py-agent/           # Python Agent
│   ├── worker.py       # 后台 Worker (任务队列 + 死信 ntfy 告警)
│   ├── serve.py        # AI 解析 HTTP 服务
│   ├── tools/          # LightAgent 工具函数 (调 Go REST API)
│   ├── prompts/        # Prompt 模板
│   ├── ioc/            # EPICS 虚拟 IOC (pyEpics 模拟硬件 PV)
│   └── tests/          # 测试
├── web-ui/             # Vue 3 前端
│   ├── src/api/        # API 客户端
│   ├── src/views/      # 24 个业务页面
│   └── src/components/ # 通用组件
├── migrations/         # PostgreSQL 迁移 (32 个版本，64 个文件)
├── deploy/             # Docker Compose、Dockerfile、frp、Nginx 配置
└── images/             # 运行时图片附件目录
```

## 快速开始

```bash
# 克隆仓库
git clone git@github.com:zhu571/hiaf-lab-system.git
cd hiaf-lab-system

# 配置环境变量
cp deploy/.env.example deploy/.env
# 编辑 .env，填入 DEEPSEEK_API_KEY 和其他必要配置

# Docker Compose 一键启动
docker compose -f deploy/docker-compose.yml up -d
```

启动后访问：

| 服务 | 地址 | 说明 |
|------|------|------|
| Go 后端 API | http://localhost:8000 | REST API + 前端 SPA |
| Grafana 监控 | http://localhost:3000 | 仪表盘（凭据见 deploy/secrets/grafana_admin_password.txt） |
| ntfy 消息 | http://localhost:8085 | 通知服务 |

部署涉及的全部服务：

| 容器 | 说明 |
|------|------|
| `lab-postgres` | PostgreSQL 16 数据库 |
| `lab-migrate` | 数据库迁移 (启动时自动执行) |
| `lab-server` | Go 后端 + 嵌入前端 |
| `lab-py-agent` | Python AI Agent (后台 Worker) |
| `lab-py-agent-interpret` | Python AI 解析服务 |
| `lab-epics-gateway` | EPICS 通道访问网关 |
| `lab-ioc` | 虚拟 IOC (模拟硬件 PV) |
| `lab-influxdb` | 时序数据库 |
| `lab-grafana` | 监控仪表盘 |
| `lab-ntfy` | 消息通知 |

## CLI / MCP（labctl）

`cli/` 是给 AI Agent（Hermes/Codex/Goose 等）或运维人员的命令行操作系统：通过 REST API 直接操作日报、项目、Issue、测试数据、实验批次、告警与日志，不手拼 HTTP 请求，服务端权限/审计/限流不变。

```bash
# 运行（复用 py-agent 虚拟环境；依赖见 cli/requirements.txt：click/httpx/mcp）
py-agent/.venv/bin/python -m cli.cli --help
```

8 个子命令：`login`（登录/注销/whoami）、`daily-report`（today/history/entry）、`projects`（list/get/create）、`issues`（list/create/transition）、`test-data`（list/entry）、`runs`（list/get/status）、`alerts`（list/resolve）、`logs`（list/get）。输出默认 JSON（Agent 易解析），`--human` 人类可读，错误透传服务端 `request_id`。

认证双通道：交互登录 `login <用户名>`（或 CI 场景 `login --token-stdin`，凭证存 `~/.labctl/token` 0600、密码不落盘）；无人值守 `LABCTL_SERVICE_TOKEN` 服务账号（纯透传，只读为主——写操作缺 CSRF token 会 403 `csrf_failed` 原样透传）。环境变量 `LABCTL_BASE_URL` 默认 `http://localhost:8000`。

MCP Server（stdio，21 个 `labctl_*` 工具）：

```bash
py-agent/.venv/bin/python -m cli.mcp_server
```

MCP 自身无独立鉴权——安全边界 = 启动进程所持 token 的权限，仅限内网/受信主机启动。详见 `cli/README.md`。

## 入口文档

| 文档 | 说明 |
|------|------|
| [docs/设计总纲.md](docs/设计总纲.md) | 项目全貌、架构、数据流、阶段计划 |
| [AGENTS.md](AGENTS.md) | AI 编程助手入口 |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Git 工作流与 PR 流程 |
| [docs/collab-guide.md](docs/collab-guide.md) | 多人协作流程 |
| [docs/api-contract.md](docs/api-contract.md) | API 契约 |
| [docs/permission-audit.md](docs/permission-audit.md) | 权限审计 |
| [docs/instrument-security.md](docs/instrument-security.md) | 仪器安全 |
| [docs/project-design.md](docs/project-design.md) | 项目维度设计 |

## CI

GitHub Actions 包含三个 job：Go 后端 (`go test ./...`)、Vue 前端 (`npm ci && npm run build`)、Python Agent (`python -m compileall -q .`)。

## 许可证

MIT
