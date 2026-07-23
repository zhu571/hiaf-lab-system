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
- 传感器/EPICS/PLC 数据接入和仪器控制。
- InfluxDB 时序数据存储 + Grafana 监控仪表盘。
- EPICS 通道访问网关 + 虚拟 IOC（pyEpics 模拟硬件 PV）。
- ntfy 消息通知。

## 2. 当前仓库状态

系统已全部落地运行。当前 GitHub 仓库：

- `docs/`：API、权限审计、仪器安全、项目设计、Agent 策略、维护策略等设计文档。
- `go-server/`：Go 后端，14+ 业务模块。
- `web-ui/`：Vue 3 + Element Plus 前端，9 个页面。
- `py-agent/`：Python LightAgent 服务 + EPICS 虚拟 IOC。
- `migrations/`：PostgreSQL 迁移脚本（21 个版本）。
- `deploy/`：Docker Compose（10 个服务）、Dockerfile、secrets。
- `.github/workflows/ci.yml`：Go、前端、Python Agent 三个 CI job。

## 3. 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.22+，chi 路由，标准库 `net/http` |
| 数据库 | PostgreSQL 16，golang-migrate/migrate |
| 前端 | Vue 3 + Element Plus，Vite 单文件构建（JS/CSS 全部内联进 index.html，go:embed 嵌入） |
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
│   ├── agent/              # Agent 交互模块
│   ├── audit/              # 审计日志模块
│   ├── attachments/        # 附件管理模块
│   ├── notify/             # 消息通知模块
│   ├── epics-gateway/      # EPICS 通道访问网关
│   ├── middleware/         # JWT、权限、审计、日志中间件
│   └── common/             # DB、响应、错误、request_id 等共享工具
├── py-agent/               # Python Agent
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

铁律：模块间只走 HTTP API，不允许跨模块直接访问、写入或 join 对方数据库表。

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
- 发现文档与代码不一致时，在改动中同步修正文档或在提交说明里明确指出。
