# 开发工作流

> 给人类和 AI 编程助手的协作指南。clone 后先看本文件，再按任务读取 `AGENTS.md` 和 `docs/` 下的专题文档。

## 1. 仓库状态

`go-server/`、`web-ui/`、`py-agent/`、`migrations/`、`deploy/` 已全部落地运行。系统采用 Docker Compose 单机全栈部署，包含 Go API 后端、Vue 3 前端、Python Agent、PostgreSQL、InfluxDB、Grafana、EPICS 网关、虚拟 IOC、ntfy 通知等模块。写代码前先确认实际文件状态。

## 2. Clone 后第一步

```bash
git clone git@github.com:zhu571/hiaf-lab-system.git
cd hiaf-lab-system

# 确认远端和当前分支
git remote -v
git branch --show-current
git fetch origin

# 所有日常开发都从 develop 开始
git checkout develop
git pull --ff-only origin develop
```

如果 `git checkout develop` 失败，先查看远端分支：

```bash
git branch -r
git checkout -b develop origin/develop
```

## 3. Git 工作流

```text
main ─────────────────────────  受保护，只通过 PR 从 develop 合入
  │
  └── develop ───────────────  日常集成，所有功能分支先 PR 到这里
        ├── feat/auth-login
        ├── feat/logs-api
        ├── fix/issue-status
        ├── docs/contributing
        └── chore/ci
```

固定规则：

- 不直接 push 到 `main`。
- 不直接 push 到 `develop`。
- 所有改动都从 `develop` 新建主题分支。
- PR 目标分支是 `develop`，不是 `main`。
- 一个分支只做一个主题；不要混合功能、重构、格式化和文档大改。
- 不 force push 已经给别人审核的分支，除非明确说明原因。
- 不提交 `.gitignore` 已忽略的文件，例如 `.env`、数据库、附件、备份、证书、token、构建产物。

分支命名：

```text
feat/<module-or-topic>
fix/<module-or-topic>
docs/<topic>
chore/<topic>
hotfix/<topic>
```

示例：

```text
feat/auth-login
feat/logs-create-api
fix/issues-status-transition
docs/api-contract
chore/github-actions
```

## 4. 人类开发者流程

```bash
# 1. 从最新 develop 创建分支
git checkout develop
git pull --ff-only origin develop
git checkout -b feat/my-module

# 2. 开发并检查改动
git status --short
git diff

# 3. 提交
git add -A
git commit -m "feat(logs): add create log endpoint"

# 4. 推送自己的主题分支
git push -u origin feat/my-module
```

然后在 GitHub 创建 PR：

```text
feat/my-module → develop
```

PR 地址：

```text
https://github.com/zhu571/hiaf-lab-system/pulls
```

## 5. AI 编程助手流程

AI 编程助手必须使用 Git CLI 操作本地仓库，不要用 GitHub API 绕过分支和 PR 流程。

开始前：

```bash
git status --short --branch
git fetch origin
git checkout develop
git pull --ff-only origin develop
git checkout -b docs/<topic>   # 或 feat/fix/chore
```

完成后：

```bash
git status --short
git diff
git add -A
git commit -m "docs(contributing): clarify git workflow"
git push -u origin docs/<topic>
```

最后告诉用户：

```text
已推送 docs/<topic>，请在 GitHub 创建 PR：docs/<topic> → develop。
```

如果当前工作区已有用户未提交改动：

- 不要覆盖、回滚或格式化无关文件。
- 只修改本任务相关文件。
- 如果同一文件里已有无关改动，先读懂上下文，再做最小补丁。
- 如果无法区分哪些改动属于用户，先停止并说明风险。

如果没有远端 push 权限：

- 不要改推 `main` 或 `develop`。
- 保留本地 commit。
- 把当前分支名、commit SHA 和 `git status --short --branch` 结果告诉用户。

## 6. 写代码前读什么

| 任务 | 必读文档 |
|------|----------|
| 了解项目全貌 | `README.md`、`docs/设计总纲.md`、`docs/实验室日志系统扩展方案.md` |
| AI 编程助手接手任务 | `AGENTS.md`、本文件 |
| Git 分支、PR、审核 | 本文件、`docs/collab-guide.md` |
| 负责模块拆分和协作 | `docs/collab-guide.md`、`docs/多人协作开发规范.md` |
| 写 Go API 或前端 API 调用 | `docs/api-contract.md` |
| 写认证、权限、审计、Agent 代操作 | `docs/permission-audit.md` |
| 写项目维度或项目 ACL | `docs/project-design.md` |
| 控制仪器 | `docs/instrument-security.md` + `docs/仪器白名单.yaml` |
| 写 Agent 自动解析或自动入库 | `docs/agent-auto-review.md` |
| 审核代码（常规） | 找同事 Review |
| 审核代码（单人/紧急） | `docs/code-review.md` — Codex AI 审查流程 |
| 搭建开发环境 | `docs/collab-guide.md` 第 7 节 |
| 写 AI 问答 | `docs/ai-qa-codex.md`、`docs/ai-qa-assistant.md` |
| 写部署、迁移、备份、回滚 | `docs/maintenance-strategy.md` |

如果文档名和实际文件不一致，先运行：

```bash
rg --files
```

不要凭记忆创建重复文档。

## 7. 当前目标目录

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
│   ├── middleware/     # JWT、权限、审计中间件
│   └── common/          # 共享工具
├── py-agent/           # Python Agent
│   ├── tools/          # LightAgent 工具函数 (调 Go REST API)
│   ├── prompts/        # Prompt 模板
│   └── ioc/            # EPICS 虚拟 IOC
├── web-ui/             # Vue 3 前端 PWA
├── migrations/         # PostgreSQL 迁移脚本
├── deploy/             # Docker Compose、frp、Nginx 配置
├── images/             # 运行时图片附件目录
├── docs/               # 设计和协作文档
├── AGENTS.md           # AI 编程助手入口
├── CONTRIBUTING.md     # 本文件
├── README.md           # 项目入口
└── .github/workflows/  # CI 自动检查
```

## 8. PR 前检查

根据本次改动范围运行对应检查。

Go 后端：

```bash
cd go-server
go test ./...
go vet ./...
```

前端：

```bash
cd web-ui
npm ci
npm test --if-present
npm run build --if-present
```

Python Agent：

```bash
cd py-agent
python -m compileall -q .
```

文档改动至少检查：

```bash
git diff --check
```

PR 描述必须包含：

- 改了什么。
- 影响哪些模块或文档。
- 实际运行了哪些检查。
- 未运行的检查及原因。
- 是否影响 API、数据库迁移、权限审计、仪器白名单、Agent 自动入库或部署。

## 9. 禁止事项

- 不提交数据库、SQLite 文件、附件、备份、真实 `.env`、证书、token、内网凭据。
- 不修改已发布迁移文件；只能追加新迁移。
- 不让 Agent 直连 PostgreSQL 或 SQLite 写业务数据。
- 不绕过 Go 后端权限、审计和人工审批边界。
- 不在 CI 或本地脚本中硬编码密码、token、设备密钥。
- 可以用 `gh pr create --fill --web` 打开 PR 创建页面；不得使用 `gh pr merge` 或其他自动合并命令，合并必须等人类审核。
