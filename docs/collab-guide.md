# 实验室日志管理系统多人协作开发规范

> 适用团队：3-4 人实验室开发小组  
> 技术栈：Go + chi、Python + LightAgent、Vue 3、PostgreSQL  
> 相关设计文档：`api-contract.md`、`permission-audit.md`、`instrument-security.md`、`project-design.md`、`ai-qa-codex.md`、`agent-auto-review.md`、`仪器白名单.yaml`

## 1. 协作目标

本项目不是单人脚本项目，而是多人长期维护的实验室系统。协作规范的目标是：

- 每个成员可以独立开发一个模块，不随意改动他人模块。
- 模块之间通过明确 API、数据库迁移和事件约定协作。
- 仪器控制、权限、审计、Agent 代操作等高风险功能必须可追溯、可回滚、可审核。
- 每次合并到主分支前，都能通过最小测试和代码审核。

默认规则：

- 设计文档优先级高于口头约定。
- API 变化必须先改 `api-contract.md`，再改实现。
- 权限变化必须对照 `permission-audit.md`。
- 项目、日报和实验批次变化必须对照 `project-design.md`。
- AI 问答变化必须对照 `ai-qa-codex.md`，默认只读、带权限和来源。
- Agent 自动入库变化必须对照 `agent-auto-review.md`，不得绕过后端规则门控。
- 仪器控制变化必须对照 `instrument-security.md` 和 `仪器白名单.yaml`。
- 数据库结构变化必须通过迁移文件完成，禁止直接手工改生产库。

## 2. 模块拆分与依赖关系

项目分为 13 个独立模块。每个模块都要有清楚的负责人、接口、测试和合并条件。

| 编号 | 模块 | 主要职责 | 建议负责人 |
|------|------|----------|------------|
| M1 | 认证鉴权 | 登录、JWT、刷新 token、角色、ACL、服务账号、设备身份 | 后端负责人 |
| M2 | 项目管理 | project、project_members、生命周期、项目 ACL、项目仪表盘基础 | 后端/前端协作 |
| M3 | 日志管理 | daily_reports 日容器、daily_report_items/logs 项目记录、附件关联、日志查询、日志修改审核 | 后端/前端协作 |
| M4 | 问题管理 | Issue 创建、项目归属、状态流转、解决记录、图片附件、评论 | 后端/前端协作 |
| M5 | 经验库 | 从日志和 Issue 提炼经验、候选、审核、发布、归档 | 后端/Agent 协作 |
| M6 | 计划与实验批次 | 实验计划、任务拆分、experiment_runs、负责人、截止时间、状态跟踪 | 后端/前端协作 |
| M7 | 传感器 | 设备数据推送、签名校验、时序数据入库、告警 | 后端负责人 |
| M8 | 仪器控制 | 独立仪器权限、仪器租约、命令白名单、SCPI 执行、紧急停止、审计 | 后端/仪器负责人 |
| M9 | Agent | LightAgent 解析、候选动作、双门控自动入库、用户偏好、人工审批 | Python/Agent 负责人 |
| M10 | AI 问答 | 只读查询函数、权限过滤 RAG、来源引用、桌面侧边栏/移动对话面板 | 后端/Agent/前端协作 |
| M11 | 前端 | Vue 3 页面、权限展示、表单、仪器控制台、审核台、AI 问答入口 | 前端负责人 |
| M12 | 数据库迁移 | PostgreSQL schema、索引、种子数据、回滚脚本、旧 SQLite 项目归属迁移 | 后端负责人 |
| M13 | 部署配置 | Docker、环境变量、GitHub Actions、备份、日志 | 运维/后端负责人 |

### 2.1 模块边界

- M1、M2、M12、M13 是基础模块，其他模块依赖它们。
- 业务模块不得直接访问其他业务模块的私有表。
- 前端只通过 Go API 访问后端，不能直接连接 PostgreSQL。
- Python Agent 只通过 Go API 提交解析结果或候选动作，不能直接写业务表。
- AI 问答模块只读，不能和快速录入、Agent 写入、仪器控制混成一个入口。
- 仪器控制只能通过 M8 暴露的受控接口执行，不能在 Agent 或前端绕过白名单直接发 SCPI。
- 传感器和设备推送必须经过签名校验、设备身份校验和审计。
- 项目级权限不继承为仪器权限；仪器权限、租约和命令白名单始终独立校验。

### 2.2 推荐目录结构

```text
lab-daily-report/
├── go-server/           # Go + chi API
│   ├── cmd/api/
│   ├── internal/auth/
│   ├── internal/projects/
│   ├── internal/logs/
│   ├── internal/issues/
│   ├── internal/experiences/
│   ├── internal/plans/
│   ├── internal/experimentruns/
│   ├── internal/qa/
│   ├── internal/sensors/
│   ├── internal/instruments/
│   ├── internal/audit/
│   └── internal/platform/
├── py-agent/             # Python + LightAgent
├── web-ui/               # Vue 3
├── migrations/           # PostgreSQL 迁移
├── deploy/               # Docker、Nginx、systemd、备份脚本
├── docs/                 # 设计和协作文档
└── tests/                # 跨模块集成测试
```

当前仓库已有 Python 脚本和设计文档，后续迁移到该结构时应分批进行，不要在一个 PR 中同时重构目录和实现大量业务功能。

### 2.3 依赖顺序

建议按下面顺序开发，避免前端、Agent 和仪器模块互相等待：

1. M12 数据库迁移：先定义用户、项目、项目成员、日报容器、项目日志/条目、Issue、附件、审计等基础表。
2. M1 认证鉴权：登录、JWT、用户角色、ACL、中间件。
3. M13 部署配置：本地 Docker Compose、PostgreSQL、基础 CI。
4. M2 项目管理：项目生命周期、项目成员、项目 ACL 和项目列表。
5. M3 日志管理：第一个完整业务闭环，用于验证项目权限、审计、附件和前端模式。
6. M4 问题管理：复用日志模块的附件、项目权限、审计模式。
7. M6 计划与实验批次：相对低风险，可与 M4 并行。
8. M5 经验库：依赖日志、Issue、权限和人工审核流程。
9. M9 Agent：先做解析和候选建议，冷启动全草稿；观察稳定后再做高置信自动入库。
10. M10 AI 问答：等项目、权限、日志、Issue、经验 API 稳定后做只读 MVP。
11. M7 传感器：依赖设备身份、签名和审计。
12. M8 仪器控制：必须在鉴权、审计、白名单、租约都具备后开发。
13. M11 前端：可从 M1-M4 开始并行，但每个页面必须以稳定 API 为准。

### 2.4 独立开发与独立测试要求

每个模块都必须满足：

- 有独立包或目录，不把业务逻辑写进公共工具包。
- 有清晰的入口：HTTP handler、service、repository 或 Agent tool。
- 有单元测试覆盖核心规则。
- 有最少一组 API 或集成测试覆盖正常路径和失败路径。
- 涉及写操作时必须写审计事件。
- 涉及权限时必须有无权限测试。
- 涉及项目数据时必须验证 `project_id`、项目生命周期和项目 ACL。
- 涉及仪器、Agent、设备推送时必须有拒绝危险操作的测试。
- 涉及 AI 问答时必须验证只读、权限过滤、来源引用和拒答路径。

## 3. Git 分支策略

小团队使用简化 Git Flow：

```text
main       # 稳定分支，只放可部署版本
develop    # 日常集成分支，所有 feature 先合入这里
feature/*  # 功能开发分支
fix/*      # 普通缺陷修复分支
hotfix/*   # 生产紧急修复分支，直接从 main 切出
docs/*     # 文档修改分支
chore/*    # CI、格式化、依赖、脚手架等非业务修改
```

### 3.1 分支命名

格式：

```text
类型/模块-简短描述
```

示例：

```text
feature/auth-jwt-login
feature/logs-create-api
feature/instruments-lease
feature/agent-log-parser
fix/issues-status-transition
docs/collab-guide
chore/github-actions
hotfix/token-refresh-expiry
```

规则：

- 分支名只用小写英文、数字和短横线。
- 一个分支只做一个主题。
- 不在 `main` 或 `develop` 上直接提交。
- 开发超过 2 天的分支，每天至少从 `develop` rebase 或 merge 一次，避免最后集中冲突。

### 3.2 提交信息规范

推荐格式：

```text
type(module): short summary
```

示例：

```text
feat(auth): add jwt login endpoint
fix(instruments): reject unsafe sweep range
test(logs): cover idempotent create
docs(api): update issue transition contract
chore(ci): add backend test workflow
```

常用 type：

- `feat`：新功能
- `fix`：修复缺陷
- `test`：测试
- `docs`：文档
- `refactor`：不改变行为的重构
- `chore`：构建、CI、依赖、格式化
- `migration`：数据库迁移

## 4. 代码审核流程

所有代码必须通过 Pull Request 合并。禁止直接 push 到 `main`。`develop` 也建议保护，至少需要 1 人审核。

### 4.1 PR 流程

1. 开发者从 `develop` 创建 feature 分支。
2. 开发过程中本地运行相关测试。
3. PR 提交到 `develop`。
4. PR 描述必须写清楚变更范围、测试结果、风险点。
5. 至少 1 名成员审核通过。
6. CI 全部通过。
7. 作者处理评论后合并。

PR 模板建议：

```markdown
## 变更内容
- 

## 涉及模块
- 

## 测试
- [ ] 单元测试
- [ ] API 测试
- [ ] 集成测试
- [ ] 手工验证

## 风险与回滚
- 

## 是否影响
- [ ] API 契约
- [ ] 数据库迁移
- [ ] 项目/日报/实验批次模型
- [ ] 权限/审计
- [ ] 仪器白名单
- [ ] Agent 自动入库/代操作
- [ ] AI 问答只读查询/RAG
```

### 4.2 审核检查清单

通用检查：

- 代码是否只解决本 PR 的问题。
- 是否存在明显重复代码、过度抽象或难以理解的命名。
- 是否有必要测试。
- 错误是否被正确返回和记录。
- 日志中是否泄露密码、token、设备密钥、个人敏感信息。
- 是否保持 API 错误格式与 `api-contract.md` 一致。
- 是否有数据库迁移，迁移是否可回滚。
- 是否更新相关文档。
- 是否保持项目维度口径：正式业务记录必须能归属主项目，日报只作为日容器。

后端检查：

- handler 是否只负责解析请求和返回响应，业务规则是否在 service 层。
- repository 是否使用参数化 SQL，禁止拼接用户输入。
- 所有写接口是否要求 `Idempotency-Key`。
- 权限检查是否在业务操作前执行。
- 项目权限、项目生命周期和对象权限是否都被校验。
- 仪器权限是否独立校验，没有从项目成员关系继承。
- 审计是否记录成功和失败的高风险操作。

Agent 检查：

- Agent 是否只产生候选动作或调用被允许的 API。
- 是否带 `X-Acting-User-ID`。
- 是否禁止删除、改权限、执行 red 仪器命令。
- 自动入库是否同时检查 `agent_confidence`、Go 后端 `rule_confidence` 和用户偏好。
- 冷启动/观察/自动三个阶段是否可配置，默认是否为全草稿。
- OCR、图片、指定设备相关记录是否能强制进入人工确认。
- Prompt、解析规则或工具列表变化是否有版本记录。

AI 问答检查：

- QA 是否只读，不能写日志、Issue、经验、计划或仪器命令。
- 是否只通过预定义只读查询函数和权限过滤 RAG 获取资料。
- 检索是否先按用户可读项目集合过滤，不能把权限判断交给 LLM。
- 回答是否带时间范围、来源链接和不确定性说明。
- MVP 是否只覆盖项目进展、相似历史问题、未解决风险和经验查询。
- 统计类问题是否在结构化字段补齐前拒答或说明口径不足。

前端检查：

- 是否根据后端权限控制按钮和页面入口。
- 是否正确展示后端错误信息，不吞掉错误。
- 表单是否有基本校验。
- 高风险操作是否有二次确认。
- 日报录入是否按项目拆分条目，而不是把 `project_id` 放到整张日报上。
- 桌面端 AI 问答是否使用右侧侧边栏，移动端是否使用底部导航对话面板。

仪器控制检查：

- 是否严格使用 `仪器白名单.yaml`。
- 是否校验参数范围和组合约束。
- 是否持有租约。
- 是否有超时、锁释放、状态恢复和审计。
- Agent 发起 yellow 命令是否走人工确认。

### 4.3 谁审核谁

3-4 人团队建议固定责任，但避免自己审核自己：

- 后端基础模块：由另一个后端成员或项目负责人审核。
- 前端页面：由前端成员主审，相关 API 负责人复审接口使用。
- Agent 代码：由 Agent 负责人外的成员审核安全边界，必要时后端负责人复审。
- AI 问答：由后端负责人审核权限和只读工具，前端负责人审核交互，Agent 负责人审核 RAG/提示词边界。
- 仪器控制：必须由仪器负责人和后端负责人共同确认。
- 数据库迁移：必须由后端负责人审核。
- 项目权限、审计、白名单、Agent 自动入库、AI 问答权限过滤：至少 2 人确认后再合并。

如果只有 3 人，最低要求：

- 普通业务 PR：1 人审核。
- 高风险 PR：2 人审核。
- 文档和小修复：1 人审核或负责人自审后合并，但不能影响生产行为。

## 5. 测试策略

测试分为单元测试、API 测试、集成测试和手工验收。不是所有 PR 都需要跑完整集成测试，但合并到 `main` 前必须跑完整测试。

### 5.1 单元测试

目标：验证单个函数、service 或工具类的核心规则。

责任分工：

- Go 后端：模块负责人编写 `*_test.go`。
- Python Agent：Agent 负责人编写 `pytest` 测试。
- Vue 前端：前端负责人编写组件测试或关键工具函数测试。

必须覆盖：

- 权限判断。
- 项目生命周期和项目 ACL。
- `daily_reports` 日容器与项目化条目/日志的关系。
- 参数校验。
- 状态流转。
- 幂等写入。
- 错误路径。
- 仪器命令参数收窄。
- Agent 解析不确定时进入人工审核。
- Agent 高置信自动入库必须通过后端规则和用户偏好。
- QA 只读查询、权限过滤 RAG 和拒答路径。

### 5.2 API 测试

目标：验证 HTTP 接口符合 `api-contract.md`。

建议使用：

- Go：`httptest` 或独立 API 测试。
- 跨语言：可用 `pytest + requests`。
- 手工调试：允许使用 HTTP 文件、curl 或 Postman，但不能替代自动测试。

每个 API 至少测试：

- 正常请求。
- 缺少认证。
- 权限不足。
- 参数错误。
- 重复 `Idempotency-Key`。
- 返回格式是否包含 `data` 或 `error` 和 `request_id`。

### 5.3 集成测试

目标：验证多个模块连起来后是否工作。

最低集成场景：

1. 用户登录后创建日志，审计表出现写入记录。
2. 用户无项目权限时不能读取项目日志。
3. 同一用户同一天只有一个 `daily_reports`，但可包含多个项目条目。
4. 归档项目不能新增正式日志，完成项目只能补录收尾类记录。
5. 创建 Issue，更新状态，关闭 Issue，并验证主项目权限。
6. Agent 解析日志文本，冷启动阶段只生成草稿。
7. Agent 高置信文本日报在自动阶段通过双门控后可入库，OCR 输入仍进入人工确认。
8. QA 回答有权限项目进展并返回来源；无权限项目拒答且不泄露存在性。
9. 设备推送传感器数据，签名错误时拒绝。
10. 仪器 yellow 命令没有租约时拒绝。
11. 仪器命令参数超出白名单时拒绝。
12. 数据库迁移从空库执行成功。

### 5.4 手工验收

适用于 UI、仪器联调和实验室实际流程。手工验收必须在 PR 中记录：

- 验收人。
- 验收时间。
- 操作步骤。
- 结果截图或日志摘要。
- 是否影响真实仪器。

真实仪器联调前必须先用 mock instrument 或 dry-run 模式验证。

## 6. 模块合并流程

项目使用 `feature -> develop -> main`。

### 6.1 feature 合并到 develop

允许合并条件：

- PR 范围清楚。
- 至少 1 人审核通过。
- CI 通过。
- 相关单元测试和 API 测试通过。
- API、权限、数据库、白名单变化已更新文档。
- 没有未解释的 TODO、临时代码或调试输出。

合并方式：

- 小 PR 推荐 squash merge，保持历史清楚。
- 多个有意义提交的 PR 可以 merge commit。
- 不建议在共享分支上 force push。

### 6.2 develop 合并到 main

合并时机：

- 一个可演示版本完成。
- 一组相关模块形成闭环。
- 准备部署到实验室测试环境或生产环境。

前置条件：

- 完整 CI 通过。
- 数据库迁移在测试库执行成功。
- 集成测试通过。
- 高风险功能完成手工验收。
- 更新版本说明。
- 确认回滚方案。

### 6.3 hotfix 合并流程

生产紧急修复：

1. 从 `main` 切出 `hotfix/*`。
2. 只修复紧急问题，不混入重构和新功能。
3. 通过最小测试和审核。
4. 合并回 `main` 并打 tag。
5. 再把 `main` 合并回 `develop`，避免修复丢失。

## 7. CI/CD 配置

GitHub Actions 最低可用配置应覆盖格式检查、测试和构建。建议先保持简单，后续再增加部署。

### 7.1 最低 CI 工作流

保存为 `.github/workflows/ci.yml`：

```yaml
name: ci

on:
  pull_request:
    branches: [develop, main]
  push:
    branches: [develop, main]

jobs:
  backend:
    name: backend
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: backend
    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_USER: lab
          POSTGRES_PASSWORD: lab
          POSTGRES_DB: lab_daily_test
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go mod download
      - run: gofmt -w .
      - run: test -z "$(git status --porcelain)" || (git diff && exit 1)
      - run: go test ./...
        env:
          DATABASE_URL: postgres://lab:lab@localhost:5432/lab_daily_test?sslmode=disable

  agent:
    name: agent
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: agent
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with:
          python-version: "3.11"
      - run: python -m pip install --upgrade pip
      - run: pip install -r requirements.txt
      - run: pytest

  frontend:
    name: frontend
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: frontend
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
          cache: npm
          cache-dependency-path: web-ui/package-lock.json
      - run: npm ci
      - run: npm run lint
      - run: npm run test -- --run
      - run: npm run build
```

说明：

- 当前仓库如果还没有 `go-server/py-agent/web-ui` 目录，可先按实际目录删减 job。
- CI 中的格式检查应只检查，不在 CI 自动提交格式化结果。
- 后续可增加 `migrations` job，专门验证 PostgreSQL 迁移。

### 7.2 部署最低要求

部署到测试环境前必须具备：

- `.env.example`，列出所有环境变量但不包含真实密码。
- Docker Compose 或等价启动脚本。
- 数据库迁移命令。
- 回滚说明。
- 日志输出到标准输出或统一日志目录。
- 生产密钥只放在服务器或 GitHub Secrets，禁止提交到仓库。

## 8. 开发环境搭建

新成员从零开始按以下步骤操作。

### 8.1 准备工具

安装：

- Git
- Go 1.22 或项目指定版本
- Python 3.11
- Node.js 20
- PostgreSQL 16 或 Docker
- Docker Desktop / Docker Engine
- VS Code 或其他编辑器

建议编辑器插件：

- Go 官方插件
- Python / Pylance
- Vue / Volar
- ESLint
- Prettier
- YAML

### 8.2 拉取代码

```bash
git clone <repo-url>
cd lab-daily-report
git checkout develop
```

### 8.3 配置环境变量

```bash
cp .env.example .env
```

本地 `.env` 至少应包含：

```text
APP_ENV=local
DATABASE_URL=postgres://lab:lab@localhost:5432/lab_daily?sslmode=disable
JWT_SECRET=local-dev-secret
AGENT_SERVICE_TOKEN=local-agent-token
DEVICE_HMAC_SECRET=local-device-secret
INSTRUMENT_DRY_RUN=true
```

规则：

- `.env` 不提交。
- 新增环境变量必须同步更新 `.env.example`。
- 本地默认 `INSTRUMENT_DRY_RUN=true`，禁止新成员直接连真实仪器。

### 8.4 启动数据库

推荐 Docker：

```bash
docker compose up -d postgres
```

执行迁移：

```bash
make migrate-up
```

如果还没有 Makefile，可临时用项目约定的迁移工具执行，但最终应收敛到统一命令。

### 8.5 启动后端

```bash
cd backend
go mod download
go run ./cmd/api
```

检查：

```bash
curl http://localhost:8080/healthz
```

### 8.6 启动 Agent

```bash
cd agent
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
pytest
python -m agent
```

Agent 本地开发默认只能调用测试 API 或 dry-run API。

### 8.7 启动前端

```bash
cd frontend
npm install
npm run dev
```

默认访问：

```text
http://localhost:5173
```

### 8.8 第一次提交前检查

```bash
git status
go test ./...
pytest
npm run lint
npm run build
```

按实际修改的模块运行对应命令。没有修改的模块可以不跑，但 PR 描述中要说明测试范围。

## 9. 编码规范

### 9.1 通用规范

- 代码优先清楚，不追求复杂设计。
- 一个函数只做一件主要事情。
- 命名表达业务含义，不使用 `data1`、`tmp2`、`handleStuff`。
- 禁止把密码、token、设备密钥写进代码或日志。
- 错误信息给开发者足够上下文，但给用户的响应不要泄露内部细节。
- 时间统一使用带时区的 RFC3339。
- ID 使用统一前缀，例如 `usr_`、`log_`、`iss_`、`ins_`。
- 写接口必须支持幂等。
- 高风险操作必须写审计。

### 9.2 Go 后端规范

代码风格：

- 使用 `gofmt` 和 `go test ./...`。
- 包名使用小写单词，不使用下划线。
- 文件名使用小写和下划线，例如 `lease_service.go`。
- HTTP 路由使用 chi，路由定义集中在模块的 `routes.go`。

分层建议：

```text
handler     # HTTP 请求解析、响应
service     # 业务规则、权限、审计编排
repository  # 数据库读写
model       # 领域对象
```

错误处理：

- 不忽略 `err`。
- service 返回业务错误，例如 `ErrPermissionDenied`、`ErrInvalidState`。
- handler 把业务错误转换为统一 API 错误格式。
- 数据库错误不要原样暴露给前端。

示例约定：

```go
if err != nil {
    return fmt.Errorf("create log: %w", err)
}
```

命名：

- 接口名按能力命名，如 `LogStore`、`AuditWriter`。
- handler 方法用动作命名，如 `CreateLog`、`ListLogs`。
- 布尔变量用清晰谓词，如 `canControl`、`requiresApproval`。

数据库：

- 使用参数化 SQL。
- 每次迁移一个清晰主题。
- 迁移文件命名：`YYYYMMDDHHMM_create_logs_table.sql`。
- 重要查询要考虑索引。
- 项目化业务表新增时默认要有 `project_id`，除非它是 `daily_reports` 这类明确的日容器或系统表。
- `daily_reports` 不直接承载项目归属，正式项目记录写入 `daily_report_items` 或 `logs`。
- 仪器权限相关表不得通过项目成员关系隐式授权。

### 9.3 Python Agent 规范

代码风格：

- 使用 Python 3.11。
- 使用 `ruff` 或 `black` 统一格式，项目确定后固定一种。
- 函数和变量使用 `snake_case`。
- 类名使用 `PascalCase`。
- 类型标注用于 Agent 输入、输出和工具参数。

Agent 边界：

- Agent 不直接写 PostgreSQL。
- Agent 不直接控制仪器。
- Agent 只调用 Go API 暴露的受控能力。
- 低置信度解析结果必须进入人工审核。
- 高置信自动入库必须同时满足 Agent 自评、后端规则校验和用户偏好。
- 默认冷启动全草稿；观察阶段只标记“本可自动”，不能直接提交。
- OCR 和图片来源默认不能自动入库；指定设备相关记录必须支持强制确认。
- 用户原文、OCR 内容和 prompt 版本要可追溯，但审计中避免保存大段敏感原文。

错误处理：

- 网络调用必须设置 timeout。
- API 调用失败要记录 request_id。
- 不用裸 `except:`。
- 重试必须有次数上限，写接口重试必须复用同一个 `Idempotency-Key`。

示例：

```python
try:
    result = client.create_log(candidate, idempotency_key=key)
except ApiPermissionError:
    return CandidateResult(status="rejected", reason="permission_denied")
```

测试：

- 解析规则要有固定输入输出样例。
- 工具调用要 mock。
- 高风险工具必须测试拒绝路径。
- 自动入库要测试高置信、降级、重复冲突、用户偏好和撤回入口。

### 9.4 AI 问答规范

边界：

- QA 模块只读，不执行写操作、仪器命令、权限变更或批量导出。
- 第一版只支持项目进展、相似历史问题、未解决风险和经验查询。
- 不开放通用 Text-to-SQL。
- 统计类问题在结构化字段、阈值和质量标记补齐前不得强行上线。

实现：

- 只调用预定义只读查询函数，例如 `projects.summary`、`logs.search`、`issues.search`、`issues.similar`、`plans.status`、`experiences.search`。
- RAG chunk 必须带 `project_id`、对象类型、对象 ID、时间和权限标签。
- 检索前先计算用户可读项目集合，不能先召回后让 LLM 自行过滤。
- 回答必须包含时间范围、来源链接和不确定性说明。
- 写入意图、仪器控制意图、越权查询和闲聊必须稳定拒答。

### 9.5 Vue 3 前端规范

代码风格：

- 使用 Vue 3 Composition API。
- 组件文件使用 `PascalCase.vue`。
- composable 使用 `useXxx.ts` 命名。
- API 客户端集中管理，不在组件里散落 `fetch`。
- 表单校验和错误展示要统一。

目录建议：

```text
web-ui/src/
├── api/
├── components/
├── views/
├── composables/
├── stores/
└── router/
```

权限与交互：

- 后端是最终权限来源，前端隐藏按钮只是用户体验，不是安全措施。
- 仪器控制、删除、导出、权限变更等操作必须有确认。
- API 错误要展示可理解信息，并保留 request_id 方便排查。
- 列表页必须支持加载中、空状态、错误状态。
- 日报录入页面按项目条目组织，支持一份日报包含多个项目记录。
- 桌面端 AI 问答使用右侧侧边栏。
- 移动端 AI 问答使用底部导航进入对话面板，不使用遮挡录入流程的悬浮球作为主入口。
- QA 回答的来源卡片必须能点回原始日志、Issue、计划或经验。

命名：

- 组件名表达业务用途，如 `LogEditor.vue`、`IssueStatusBadge.vue`。
- 事件名使用短横线，如 `submit-log`、`close-issue`。
- store 按领域命名，如 `useAuthStore`、`useInstrumentStore`。

### 9.6 SQL 和迁移规范

- 表名使用复数或统一项目约定，确定后不要混用。
- 字段名使用 `snake_case`。
- 所有业务表建议包含 `id`、`created_at`、`updated_at`。
- 项目化业务表建议包含 `project_id`，并建立项目 + 时间索引。
- `daily_reports` 使用 `report_date + author_id` 唯一约束，项目归属下沉到条目或正式日志。
- `experiment_runs` 只做轻量实验批次，不在第一阶段实现子项目树。
- 需要软删除的表增加 `deleted_at`。
- 审计表 append-only，应用账号不得有 UPDATE/DELETE 权限。
- 外键、唯一约束和索引必须随迁移一起提交。
- 迁移必须能在空库执行，也要考虑已有数据升级。

## 10. 安全与实验室特殊规则

### 10.1 权限和审计

- 所有业务写操作必须写审计。
- 所有 Agent 代用户操作必须写明 `actor_id` 和 `acting_user_id`。
- 项目级权限控制项目内日志、Issue、计划、测试数据、经验候选和报告。
- 仪器权限独立于项目权限，不能从项目角色继承。
- 数据导出、附件下载、仪器命令、权限变化必须审计。
- 审计参数必须脱敏。

### 10.2 仪器控制

- 默认使用 dry-run。
- 未进入白名单的命令一律拒绝。
- red 命令默认拒绝远程执行。
- yellow 命令必须有租约、参数校验、互斥锁、超时和审计。
- 紧急停止入口必须简单可见。
- 真实仪器联调必须有人在现场。

### 10.3 数据保护

- 生产数据库定期备份。
- 图片和附件路径不能允许路径穿越。
- 日志和审计中不存明文 token、密码、设备密钥。
- 测试数据不要混入真实敏感数据。
- QA 审计默认保存问题 hash、意图、工具和对象 ID，不默认复制完整敏感问答内容。

## 11. 例行协作节奏

小团队建议：

- 每周开始：确认本周模块目标和负责人。
- 每天开发前：同步正在做的分支和阻塞点。
- PR 不超过 400 行核心代码；超过时优先拆分。
- 每周至少一次合并 `develop` 到测试环境。
- 每个版本结束后整理变更说明、已知问题和下一步任务。

推荐任务拆分粒度：

- 一个 API endpoint。
- 一个页面。
- 一个数据库迁移。
- 一个 Agent tool。
- 一个仪器命令族。
- 一个权限规则。

## 12. 完成定义

一个模块或任务只有同时满足以下条件，才算完成：

- 功能按设计工作。
- 相关测试通过。
- 权限和审计符合设计文档。
- API 文档、迁移、环境变量说明已更新。
- PR 已审核并合并。
- 测试环境可运行。
- 高风险功能有手工验收记录。

对实验室系统而言，“能跑”不是完成；“可审核、可回滚、可追溯”才是完成。
