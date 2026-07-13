# 开发工作流

> 给人类和 AI 编程助手的协作指南。clone 后先看这个。

## 快速开始

```bash
git clone git@github.com:zhu571/hiaf-lab-system.git
cd hiaf-lab-system

# ⚠️ 重要: 切到 develop 分支工作，不要在 main 上直接改
git checkout develop

# 创建你的 feature 分支
git checkout -b feat/你的模块名
```

## Git 工作流（规则固定，不商量）

```
main ──────────────────────────  受保护，只通过 PR 从 develop 合入
  │
  └── develop ────────────────  日常集成，所有 feature 分支先合到这里
        ├── feat/auth
        ├── feat/log-module
        ├── feat/web-ui
        └── feat/你的分支
```

**铁律：**
- **不允许直接 push 到 main**
- **不允许直接 push 到 develop**（走 PR）
- 每天至少 push 一次你的 feature 分支
- PR 前 `git pull origin develop` 解决冲突
- PR 目标：你的 feature 分支 → **develop**

## 提交和推送

### 如果你是人类

```bash
# 1. 确认在 develop 分支上创建了 feature 分支
git checkout develop
git pull origin develop
git checkout -b feat/my-module

# 2. 写完代码后
git add -A
git commit -m "feat: 添加日志模块 /api/logs 端点"
git push -u origin feat/my-module

# 3. 去 GitHub 开 PR: feat/my-module → develop
#    https://github.com/zhu571/hiaf-lab-system/pulls
```

### 如果你是 AI 编程助手

你必须用 Git CLI 操作仓库，不要用 GitHub API 绕过 PR 流程。

```
你的工作流程:
1. git checkout develop && git pull origin develop
2. git checkout -b feat/<模块名>
3. 写代码、commit
4. git push -u origin feat/<模块名>
5. 告诉用户去开 PR: feat/<模块名> → develop
```

**AI 不要做的事:**
- ❌ 不要直接 commit 到 main 或 develop
- ❌ 不要 force push
- ❌ 不要修改 `.gitignore` 里的文件
- ❌ 不要用 `gh pr create --merge` 自动合 PR——等人类 approve

## 从哪里开始

| 你想做什么 | 读哪个文档 |
|-----------|-----------|
| 了解项目全貌 | `docs/设计总纲.md` |
| 知道自己负责哪个模块 | `docs/多人协作开发规范.md` 或 `docs/collab-guide.md` |
| 写 API 端点 | `docs/api-contract.md` |
| 写前端组件 | `docs/collab-guide.md` 第 9 节 |
| 写 Agent 逻辑 | `docs/ai-qa-assistant.md` + `docs/agent-auto-review.md` |

## 仓库结构

```
hiaf-lab-system/
├── go-server/          # Go 后端 (各子目录对应模块)
├── py-agent/           # Python Agent (LightAgent)
├── web-ui/             # Vue 3 前端
├── migrations/         # PostgreSQL 迁移脚本
├── deploy/             # Docker Compose + frp + Nginx
├── docs/               # 15 份设计文档
├── AGENTS.md           # AI 编程助手入口
├── CONTRIBUTING.md     # 本文件
├── .gitignore
├── .github/workflows/  # CI 自动检查
└── README.md
```
