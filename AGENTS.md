# AGENTS.md — 实验室日志管理系统

> 给 AI 编程助手（Codex / Claude Code / Cursor 等）的项目入口文档。  
> 开始写代码前，先读本文档，再按任务读取对应设计文档。
> docs/ 下有同名文件是设计文档索引的副本，以根目录版本为准。

---

## 1. 项目全貌

本项目正在把现有的单人 SQLite + ELOG 实验室日志系统，逐步扩展为多人协作的模块化实验室日志平台。

目标部署环境是 IMP/HIAF 内网 Rocky Linux 物理机，采用 Docker Compose 单机全栈；外网访问通过 frp + VPS + Nginx HTTPS。

核心目标：

- 多人日志录入、Issue、经验库、计划、项目维度管理。
- AI Agent 辅助解析、分类、生成候选内容，但不得绕过权限、审计和人工审批边界。
- 传感器/EPICS/PLC 数据接入和仪器控制能力逐步加入。
- 从当前 SQLite/ELOG 过渡到 PostgreSQL，不破坏现有可用数据链路。

## 2. 当前仓库状态

当前仓库仍处在“设计完成、Phase 1 待启动/过渡期”状态，不是目标目录结构已经全部落地的成品项目。

当前 GitHub 仓库已有：

- `docs/`：API、权限审计、仪器安全、项目设计、Agent 策略、维护策略等设计文档。
- `AGENTS.md`：AI 编程助手入口。
- `.github/workflows/ci.yml`：Go、前端、Python Agent 三个 CI job。
- 目标目录的占位文件：`go-server/`、`web-ui/`、`py-agent/`、`migrations/`、`deploy/`。

历史单机 SQLite/ELOG 链路可能存在于迁移前的本地工作目录或备份中，但不应直接提交数据库、附件、备份或含凭据配置到 GitHub。

目标但未必已存在：

- `go-server/`：Go 后端。
- `web-ui/`：Vue 3 前端。
- `py-agent/`：LightAgent 服务。
- `migrations/`：PostgreSQL 迁移。
- `deploy/`：Docker Compose、frp、Nginx 配置。

如果这些目录不存在，不要假设它们已经实现；按设计文档创建最小可运行版本。

## 3. 技术栈

| 层 | 技术 |
|----|------|
| 当前存量 | Python 3 + SQLite + ELOG |
| 后端目标 | Go 1.22+，chi 路由，标准库 `net/http` |
| 数据库目标 | PostgreSQL 16 |
| 前端目标 | Vue 3 + Element Plus，PWA |
| AI Agent | Python 3.11+，LightAgent (`wanxingai/lightagent`) |
| 部署 | Docker Compose，Rocky Linux 单机，frp + VPS |
| 消息/告警 | ntfy（紧急），MeoW（日常） |

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

## 5. 目标目录结构

新功能按下列目标结构推进；不存在的目录由对应任务创建。

```text
hiaf-lab-system/
├── go-server/              # Go 后端
│   ├── main.go             # 入口，注册所有模块路由
│   ├── auth/               # 认证鉴权模块
│   ├── logs/               # 日志管理模块
│   ├── issues/             # 问题管理模块
│   ├── experiences/        # 经验库模块
│   ├── plans/              # 计划管理模块
│   ├── projects/           # 项目管理模块
│   ├── sensors/            # 传感器数据模块
│   ├── instruments/        # 仪器控制模块
│   ├── qa/                 # AI 问答模块
│   ├── middleware/         # JWT、权限、审计、日志中间件
│   └── common/             # DB、响应、错误、request_id 等共享工具
├── py-agent/               # Python Agent，LightAgent 工具只调 Go REST API
├── web-ui/                 # Vue 3 前端 PWA
├── migrations/             # PostgreSQL 迁移脚本
├── deploy/                 # Docker Compose、frp、Nginx 配置
├── images/                 # 运行时图片附件目录，占位可提交，实际附件不提交
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
- 不把 access token 放入 `localStorage`。

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

### 当前 SQLite/Python 链路

这条链路只适用于迁移前的历史本地副本。如果当前仓库没有 `lab_db.py` 和对应 schema，不要为验证而临时重建旧链路。

```bash
python3 - <<'PY'
from lab_db import LabDB
db = LabDB()
print("lab_env.db and gas_cell.db ready")
PY
```

可选：验证 ELOG 配置是否存在。

```bash
python3 - <<'PY'
import json
from pathlib import Path
p = Path("elog_config.json")
print(json.loads(p.read_text()) if p.exists() else "elog_config.json not found")
PY
```

### 目标 PostgreSQL 链路

只有在已经创建 `migrations/` 和 Go 后端后才运行这组命令。

```bash
# 1. 启动 PostgreSQL
docker rm -f lab-pg 2>/dev/null || true
docker run -d --name lab-pg \
  -e POSTGRES_DB=lab \
  -e POSTGRES_USER=lab \
  -e POSTGRES_PASSWORD=lab \
  -p 5432:5432 \
  postgres:16

# 2. 运行迁移
for f in migrations/*.sql; do
  [ -e "$f" ] || { echo "no migrations/*.sql"; break; }
  PGPASSWORD=lab psql -h 127.0.0.1 -U lab -d lab -v ON_ERROR_STOP=1 -f "$f"
done

# 3. 启动 Go 后端
cd go-server
go test ./...
go run .
```

### 目标前端链路

```bash
cd web-ui
npm ci
npm run dev
```

### 目标 Agent 链路

```bash
cd py-agent
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
python worker.py
```

## 8. PR 前检查清单

- [ ] 已阅读本次任务对应的设计文档。
- [ ] `go test ./...` 通过（如有 `go-server/`）。
- [ ] `go vet ./...` 无警告（如有 `go-server/`）。
- [ ] 前端构建/检查通过（如有 `web-ui/`）。
- [ ] 新 API 与 `api-contract.md` 一致。
- [ ] 写接口要求 `Idempotency-Key`。
- [ ] 权限中间件或 service 权限检查已应用。
- [ ] 所有写操作有审计日志。
- [ ] 没有跨模块直接访问对方数据库表。
- [ ] 没有硬编码密码、token、key、内网凭据。
- [ ] 数据库迁移只追加，不改历史迁移。
- [ ] 测试覆盖正常路径和至少一个异常路径。

## 9. 对 AI 编程助手的工作要求

- 先确认当前仓库实际状态，再决定是修改存量 Python/SQLite，还是创建目标 Go/Vue/PostgreSQL 模块。
- 不要因为目标架构存在，就重写当前可用链路；过渡期改动要保持 SQLite/ELOG 可回滚。
- 代码改动尽量小，优先复用已有设计和本仓库已有代码。
- 涉及仪器控制、权限、审计、Agent 自动操作时，宁可多读文档，不要靠猜。
- 发现文档与代码不一致时，在改动中同步修正文档或在提交说明里明确指出。
