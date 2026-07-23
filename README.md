# HIAF 实验室日志管理系统

HIAF 低温气体靶实验室的多人协作日志管理平台。

`go-server/`、`web-ui/`、`py-agent/`、`migrations/`、`deploy/` 已全部落地运行。系统包含 Go API 后端、Vue 3 前端、Python Agent、PostgreSQL 数据库、InfluxDB 时序库、Grafana 监控、EPICS 网关、虚拟 IOC、ntfy 消息通知等模块，通过 Docker Compose 一键部署。

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.22+，chi 路由，标准库 `net/http` |
| 前端 | Vue 3 + Element Plus + TypeScript，Vite 单文件构建 |
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
│   ├── agent/          # Agent 交互
│   ├── audit/          # 审计日志
│   ├── attachments/    # 附件管理
│   ├── notify/         # 消息通知
│   ├── epics-gateway/  # EPICS 通道访问网关
│   ├── middleware/      # JWT、权限、审计中间件
│   └── common/          # 共享工具 (DB、响应、错误、request_id)
├── py-agent/           # Python Agent
│   ├── tools/          # LightAgent 工具函数 (调 Go REST API)
│   ├── prompts/        # Prompt 模板
│   ├── ioc/            # EPICS 虚拟 IOC (pyEpics 模拟硬件 PV)
│   └── tests/          # 测试
├── web-ui/             # Vue 3 前端
│   ├── src/api/        # API 客户端
│   ├── src/views/      # 9 个业务页面
│   └── src/components/ # 通用组件
├── migrations/         # PostgreSQL 迁移 (21 个版本，43 个文件)
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
| Grafana 监控 | http://localhost:3000 | 仪表盘 (默认 admin/admin) |
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
