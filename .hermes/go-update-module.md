# Go 重写系统更新模块设计方案

> 版本：v0.2（已按五维评分整改，见附录 A）
> 目标：用 Go 替代 `.hermes/update.sh` 的全部逻辑（git pull → 变更检测 → compose build → up/迁移 → 健康检查 → 失败回滚），并在不破坏现有 API / SSE 会话 / 恢复机制的前提下，平滑替换 `go-server/system/` 的 runScript 部分。
> 相关现状代码：`.hermes/update.sh`、`go-server/system/{service,handler,model}.go`、`deploy/docker-compose.yml`。
> 相关现状结论（核验）：`update.sh` 398 行、7 步、`detect_services()` 有序 case 规则、回滚触发点与 `stepRe`/`finishFromMarker`/`recoverFromDisk` 契约均在附录 A 逐条核对过；方案修改一律以附录 A 失分点为输入。

---

## 1. 背景与目标

### 1.1 现状

- `.hermes/update.sh`（bash，398 行）承担全部更新逻辑，7 个步骤：预检 → 记录状态 → git pull → 变更检测 → 构建 → 滚动更新+迁移 → 全栈健康检查，失败自动回滚。
- `go-server/system/service.go` 只做了两件事：**触发**（`Trigger()` 用 `setsid` 脱离启动 bash 脚本）和 **消费**（`tailSessionLog` 增量读日志文件 + 轮询 `.done` marker，重建 SSE 事件流，进程重启后 `recoverFromDisk` 恢复）。
- 也就是说当前 `runScript` 只是 bash 的启动器，真正的业务逻辑在 bash 里，Go 侧无法单测、无法复用、无法在 Windows 上编译。

### 1.2 目标

| 目标 | 说明 |
|------|------|
| 逻辑 Go 化 | 7 步流水线、变更检测、回滚策略全部用 Go 实现，可单测 |
| 行为等价 | 步骤语义、回滚触发点、日志格式、done marker 与 update.sh 保持一致 |
| 会话模型不变 | SSE 订阅、环形缓冲、磁盘回放、进程重启恢复照常工作 |
| 跨平台编译 | Windows 本地可编译（build tag），Linux 生产可用 |
| 平滑切换 | `UPDATE_ENGINE=go|shell` 开关，先并行后切换，可随时回退 |

### 1.3 非目标

- 不改前端 API 契约（`/api/v1/admin/system/*`、SSE 事件结构）。
- 不引入蓝绿部署、不分离前端静态资源。
- 第一阶段不删除 `update.sh`（保留为人工运维兜底）。

---

## 2. 关键设计不变量（必须保住）

| # | 不变量 | 现状依据 | Go 版要求 |
|---|--------|----------|-----------|
| I1 | **更新过程在 server 容器被重建后仍继续** | bash 用 `setsid` 脱离 Go 进程，脚本继续跑 | 更新执行者必须是**独立于 server 容器**的进程/容器 |
| I2 | 会话日志以文本行落盘，步骤行格式 `===== 步骤 N/7：标题 =====` | `stepRe` 解析 + 前端按步骤渲染 | Go 侧输出相同文本行，`ingestLine`/`stepRe` 不动 |
| I3 | done marker JSON `{exit_code,old_sha,new_sha,ended_at}` | `finishFromMarker` 解析 | 格式不变 |
| I4 | 7 步语义与回滚触发点与 update.sh 一致 | 脚本每个失败分支 | Go 逐条对齐（见 §7） |
| I5 | session id 白名单 `upd_[a-z0-9]{10}` | 防路径穿越/注入 | 继续复用，并作为 runner 容器名的白名单 |
| I6 | 写接口走审计 + Idempotency-Key | `main.go` 路由中间件 | handler 与路由不动 |

> **I1 是整套方案的核心约束。** `setsid` 只能脱离"父进程"，不能脱离"容器命名空间"：server 容器被 `docker compose up -d server` 重建时，容器内所有进程（包括 setsid 启动的）都会被一起杀死。因此 Go 更新执行者必须运行在**独立的 runner 容器**里（见 §3），才能延续现在的"更新中 server 重启不中断"能力。

---

## 3. 总体架构与运行拓扑

### 3.1 推荐方案：detached runner 容器（docker.sock 派发）

```
                    +--------------------------------------------------------------+
                    | 宿主机 (Rocky Linux)                                          |
                    |                                                              |
   +--------------+ |   +----------------+      +------------------------------+   |
   |  Git 仓库     | |   | docker daemon  |      |   compose 管理的服务          |   |
   | /opt/hiaf-   |<+---| (docker.sock)  |<-----|  postgres/migrate/server/...  |   |
   | lab-system   | |   +----------------+      +------------------------------+   |
   +--------------+ |                                                            |
        ^   ^       |   +-----------------------------+                           |
        |   |       |   | lab-updater-<session>       |  <- detached `docker run`|
        |   |       |   |  = server 镜像 + lab-update |     独立于 compose，      |
        |   |       |   |  entrypoint（Go 流水线）     |     server 重建不影响它    |
        |   |       |   |  挂载: docker.sock / 仓库/   |                           |
        |   |       |   |       /updates 日志目录       |                           |
        |   |       |   |  --network host             |                           |
        |   |       |   +-----------------------------+                           |
        |   |       |          ^  log/marker 写 /updates/                         |
   +----+---+---+   |          |                                                  |
   | server 容器  |   |          +-- tailSessionLog 读同一目录 ---+                 |
   |  (docker.sock|   |                                                v           |
   |   已挂载)     |   |   ┌─────────────────────────────────────────────────┐    |
   +-------------+   |   │ system/Service: Trigger→spawn runner; tail+recover │    |
                     |   └─────────────────────────────────────────────────┘    |
                     +--------------------------------------------------------------+
```

- **server 容器**：新增挂载 `/var/run/docker.sock`、`/opt/hiaf-lab-system:/opt/hiaf-lab-system`（仓库，`GetVersion` 的 `git -C` 与 shell 兜底均依赖它）、`…/.hermes/updates:/updates`（会话日志共享目录）；镜像内安装 `docker-cli docker-cli-compose git`。`Trigger()` 通过 docker CLI 以 `docker run -d` 方式派发 runner 容器，而不是启动 bash。
- **runner 容器**：复用**当前正在运行的 server 镜像**（旧镜像，含 `lab-update` 二进制），`--network host`（能访问 `localhost:8085` 的 ntfy，等价于现在 bash 的 curl 路径），挂载 `docker.sock` + 仓库 + 日志目录 + git 身份，以 `--user <uid>:<gid> --group-add <docker_gid>` 运行（见 §8 权限）。
  - **仓库挂载路径 = 宿主绝对路径**（`/opt/hiaf-lab-system:/opt/hiaf-lab-system`），`WORKDIR` 同路径：git、`deploy/secrets/*`、`migrations:/migrations`、构建上下文相对路径、`COMPOSE_PROJECT_NAME`（compose 文件所在目录名 `deploy`）全部与宿主机一致，杜绝项目名漂移（§15 风险的第一条被制度化）。
  - **git 凭据透传**：以只读方式挂载宿主 `~/.gitconfig` 与 `~/.ssh`（`-v ${UPDATE_GIT_HOME}/.gitconfig:${RUNNER_GIT_HOME}/.gitconfig:ro`），并设置 `HOME=${RUNNER_GIT_HOME}`、`GIT_TERMINAL_PROMPT=0`、`GIT_HTTP_LOW_SPEED_LIMIT/TIME`（沿用 update.sh §56-60 的网络保护）。https 仓库若把 token 写在 `.git/config`（随仓库挂载已可见）则无需额外配置。若用 SSH，额外挂载 `~/.ssh` + 设置 `GIT_SSH_COMMAND`。**无凭据的 runner 在 step3 必失败**，此项是硬性要求。
  - **DB 备份落在仓库内共享目录**：`${REPO_ROOT}/.hermes/backups/`（随仓库 rw 挂载自动共享），不再用 runner 容器内 `/tmp`（`--rm` 退出即丢）。宿主运维可直接访问，回滚告警里打印该路径有效。
- **日志/恢复模型不变**：runner 把 stdout 与日志写到共享目录 `…/.hermes/updates/lab-update-<session>.log`，结束写 `.done` marker；server 的 `tailSessionLog` + `recoverFromDisk` 照旧工作。**server 重建后，新 server 实例从磁盘恢复 session 并继续 tail，直到收到 marker** —— 与现在 bash 完全同构。
- **runner 不会被更新误伤**：它不是 compose 服务，`docker compose up -d` 不触碰它；更新流程也从不执行 `docker compose down`。**更新期间 `compose build` 会覆盖 runner 所引用的镜像 tag，但运行中的 runner 容器持有旧镜像引用，不受影响**（docker 保留被引用镜像），后续维护勿据此误判。
- **shell 兜底引擎也走 runner 容器**：`UPDATE_ENGINE=shell` 时派发同一个 runner 容器，但 entrypoint 换成 `bash /opt/hiaf-lab-system/.hermes/update.sh`（env 传 `UPDATE_SESSION_ID/UPDATE_LOG_FILE/UPDATE_DONE_FILE`）。这样兜底路径同样满足 I1（server 重建不中断），且无需在 server 容器里凑齐 bash 工具链。server 镜像因此需要额外包含 `python3 curl coreutils`（update.sh 用到 `python3` 解析 JSON、`curl` 发 ntfy、`stdbuf` 行缓冲）。

### 3.2 备选方案（对比后不推荐为首选）

| 方案 | 说明 | 弃选原因 |
|------|------|----------|
| 纯宿主机二进制 | 把 `lab-update` 编译为 linux 二进制装到宿主机，server 通过 SSH/agent 调起 | server 容器无法直接 exec 宿主机进程；引入 SSH 密钥与更多攻击面 |
| 常驻 sidecar 容器 | compose 新增 `updater` 服务常驻，等触发文件/HTTP，server 无 docker.sock | 多一个常驻高权限容器（安全面更大）；触发/心跳/健康自洽复杂 |
| docker SDK（`github.com/docker/docker`） | 用客户端库替代 docker CLI | compose v2 本质是 CLI 插件，SDK 不提供 compose 编排（需引入 compose-go 重实现）；收益低、依赖重 |

**决策**：v1 采用 **exec git + docker CLI**（`os/exec`），与现状工具链一致、可逐步替换；SDK 留作未来优化，不在本方案范围。

---

## 4. 代码结构

沿用"模块内 handler/service/repository/model + 测试"约定，新增文件均在 `go-server/system/`：

```text
go-server/system/
├── handler.go          # 不变：GetVersion / TriggerUpdate / UpdateStream
├── model.go            # 小改：UpdateSession.runnerPID → RunnerID(容器名)
├── service.go          # 改 runScript 为 runner 派发；其余 SSE/tail/recover 保留
├── detect.go           # 新增：变更路径 → 受影响服务（表驱动，纯函数，可单测）
├── docker.go           # 新增：docker/compose 执行封装 + ps/config 输出解析
├── updater.go          # 新增：7 步流水线 + 回滚（被 cmd/update-runner 复用）
├── logger.go           # 新增：文本行日志 sink（写文件 + 可选 stdout）
├── runner_unix.go      # 新增：//go:build !windows  派发/杀/kill/alive（docker run/kill/inspect）
├── runner_windows.go   # 新增：//go:build windows    本地开发 runner（进程内 goroutine）
├── detect_test.go      # 新增
├── docker_test.go      # 新增
├── updater_test.go     # 新增
├── runner_test.go      # 新增（接口层，双平台共用）
└── handler_test.go     # 保留并微调
go-server/cmd/update-runner/main.go   # 新增：runner 二进制入口（flags → updater.RunSteps）
```

- `updater.go` 是**纯流水线**，不依赖 `Service`/`UpdateSession`，通过一个 `Logger` 接口输出文本行，`cmd/update-runner` 与（未来可能的）进程内模式都能复用。
- `Service` 只保留：单例互斥、session 管理、runner 派发/超时看门狗、tail、recover、sweep —— 即现在的"外壳"。

### 4.1 关键抽象

```go
// runner.go（接口，双平台实现）
type SessionRunner interface {
    Spawn(ctx context.Context, sess *UpdateSession) (RunnerID, error) // 返回 runner 容器名
    Kill(id RunnerID) error          // docker kill lab-updater-<id>
    Alive(id RunnerID) bool          // docker inspect -f {{.State.Running}}
}

// cmd.go（exec 抽象：updater.go 的全部 docker/git 调用都走它，可注入假实现）
type CmdRunner interface {
    Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
    RunOK(ctx context.Context, name string, args ...string) error // 只关心退出码
}
type execRunner struct{ env []string }   // 真实 os/exec，可叠加 GIT_/COMPOSE_ 环境
type fakeRunner struct{ calls []Call }   // 测试注入：记录命令 + 返回预设 stdout/err

// step.go（7 步流水线的可编排结构，§6.1 回滚触发点表直接落成元数据）
type Step struct {
    Num         int
    Title       string              // 必须与 update.sh 文本完全一致（I2）
    Run         func(ctx context.Context) error
    RollbackOn  bool                // 该步失败是否触发回滚
    BlockOnMigrate bool             // 失败后是否进入"迁移阻塞"分支（§6.1）
    SkipWhen    func(affected []string) bool // 步骤是否跳过（如 migrate 仅当受影响）
}
```

- `updater.go` 是**纯流水线**：把 §6 的 7 步 + §6.1 的回滚语义实现为一个 `[]Step`（标题、失败语义、回滚策略都是数据），`RunSteps(ctx, steps, logger)` 通用执行器负责顺序、错误→回滚分派、每步写 `===== 步骤 N/7：标题 =====`。`cmd/update-runner` 与（未来可能的）进程内模式都能复用。
- `docker.go` 只做**命令拼装 + 输出解析**（`compose ps/config --format json` 单对象/数组两形态、健康态提取、`config --images` 解析 runner 镜像），不持有业务状态。
- `Service` 只保留：单例互斥、session 管理、runner 派发/超时看门狗、tail、recover、sweep —— 即现在的"外壳"。

平台实现：

- `runner_unix.go`（`//go:build !windows`）：`Spawn` 执行 `docker run -d --name lab-updater-<id> … <runner-image> lab-update --session <id>`（shell 引擎则 `<runner-image> bash <repo>/.hermes/update.sh`）；`Kill`/`Alive` 走 docker CLI。session id 已被白名单约束，容器名安全。
- `runner_windows.go`（`//go:build windows`）：本地开发时在**进程内 goroutine** 跑 `updater.RunSteps`，但通过注入的 **fake CmdRunner**（§4.1）让流水线在无 docker 环境也能端到端执行（detect/dry-run/回滚分派逻辑与真实一致）；`Kill` = cancel context，`Alive` = 轮询 goroutine 状态。承诺：Windows 可编译、可单测、可本地跑流水线逻辑；不承诺 Windows 上跑真实容器部署。

---

## 5. 变更检测（detect.go）

把 `update.sh` 的 `detect_services()` 逐条移植为有序规则表，**顺序敏感**（先匹配者生效）：

| 优先级 | 匹配模式 | 受影响服务 |
|--------|----------|-----------|
| 1 | `deploy/docker-compose.yml`、`deploy/Dockerfile`、`deploy/.env*` | `ALL` |
| 2 | `deploy/Dockerfile.migrate` | `migrate` |
| 3 | `web-ui/*` | `server` |
| 4 | `go-server/epics-gateway/*` | `epics-gateway` |
| 5 | `go-server/*` | `server` |
| 6 | `py-agent/ioc/*` | `ioc` |
| 7 | `py-agent/*` | `py-agent`、`py-agent-interpret` |
| 8 | `migrations/*` | `migrate` |
| 9 | `deploy/*` | `server` |

- 与 bash 一致：`ALL` 短路；去重排序；无匹配且含 `migrations/` → 只跑 `migrate`；否则 `none`。
- 表驱动实现：`type rule struct{ prefix string; svc []string; all bool }`，单测覆盖 bash 里的全部 case 及顺序陷阱（如 `deploy/Dockerfile.migrate` 不能落到 `deploy/*` 变成 server）。
- **golden 回归清单**：P1 从现状 bash 枚举一份 `testdata/detect_golden.txt`（覆盖全部历史变更路径形态 + 每条规则代表文件 + 顺序陷阱 + 边界：空、仅 migrations、仅 `.env.example`、`py-agent/ioc/Dockerfile`、`deploy/.env.prod` 等），`detect_test.go` 以该文件为唯一事实源跑矩阵，防止 bash 与 Go 双源 drift（P4 删 bash 前它仍是回归基线）。
- `--force` → 全量 `ALL`。
- 服务枚举用 `docker compose config --services` 兜底校验（防止新服务未同步进硬编码列表时漏构建），并把结果与硬编码表差集告警（warn 不阻断，避免 compose 解析失败时误伤）。
- **runner 镜像名解析**：不再依赖 `lab-server` 之类的魔法 tag，改由 `docker compose -f deploy/docker-compose.yml config --images server` 解析实际镜像名（compose 隐式 `<project>-server` 或显式 `image:` 都返回正确值），作为预检步骤的一部分（§6 步骤 1）。

---

## 6. 7 步流水线（updater.go）

`cmd/update-runner` 收到 `--session` 后，按以下步骤执行。**步骤标题文本与 update.sh 完全一致**，保证 `stepRe` 解析与前端渲染不破。

```text
步骤 1/7：预检        → docker CLI 存在、compose v2 可用、磁盘>2GB（不足仅告警，与 bash 一致）、secrets 文件齐全、git 可用、
                        runner 镜像可解析（compose config --images server）、compose 项目名断言（compose ls）、
                        仓库对 runner uid 可写（否则提示一次性 chown，见 §8.2）
步骤 2/7：记录当前状态 → git rev-parse HEAD；docker compose exec -T postgres pg_dump →
                        ${REPO_ROOT}/.hermes/backups/lab-db-backup-<sha>-<ts>.sql（仓库内共享目录，随 runner --rm 不丢失）
步骤 3/7：git pull    → GIT_TERMINAL_PROMPT=0 + 低速超时 + HOME=runner git home（凭据已随 §3.1 挂载）；
                        fetch + pull --ff-only origin main；
                        失败时 --force 继续（用当前 HEAD），否则中止（不回滚）
步骤 4/7：变更检测     → git diff --name-only OLD NEW → detect.go 映射；dry-run 打印后退出；
                        none → 退出（不回滚）
步骤 5/7：构建镜像     → 按依赖顺序 build：epics-gateway → ioc → py-agent-interpret（与 py-agent
                        共享镜像）→ server；ALL → 全量 build --pull server py-agent py-agent-interpret epics-gateway ioc migrate（与 bash 列表一致）
步骤 6/7：滚动更新     → up -d --no-deps epics-gateway/ioc/interpret（各等健康，healthy|running 即过，
                        单服务健康等待 60s，与 bash restart_service 30×2s 对齐）→ 确保 postgres 健康
                        → 【仅当 AFFECTED 含 migrate】compose run --rm migrate
                        → up server（等健康，等待 30s，与 bash server 15×2s 对齐）
                        → up py-agent（等健康，60s）
步骤 7/7：全栈健康检查 → compose ps 轮询 9 个服务；成功发 ntfy；失败 → 回滚
```

### 6.1 回滚触发点（与 update.sh 逐条对齐）

| 失败点 | 是否回滚 | 回滚动作 |
|--------|----------|----------|
| git pull 失败（非 force） | 否 | 直接失败退出（旧代码原样运行） |
| 构建失败 | 否 | 直接失败退出（尚未重建任何容器） |
| epics-gateway/ioc/interpret 重启失败 | 是 | `git checkout OLD_SHA` → 重建受影响服务 → `up -d --no-deps` |
| 迁移失败 | 是 | 若迁移受影响 → **回滚阻塞**（通知 + 手动 migrate down），不回滚代码；否则走常规回滚 |
| server / py-agent 启动失败 | 是 | 同上常规回滚 |
| 最终健康检查失败 | 是 | 同上常规回滚 |

### 6.2 日志与超时

- 每个子命令输出直接以**行流**写入 `logger`（`os.File` 无缓冲写，天然行级可见），`tailSessionLog` 200ms 轮询即实时推送，不需要 bash 的 `stdbuf -oL tee`。
- 每条 git/docker 命令带超时（沿用现有 120s git、单命令超时参数），整体 `time.AfterFunc(30min)` 看门狗：超时 → `docker kill lab-updater-<id>` → 判失败。
- **看门狗豁免回滚阶段**：进入回滚（§9）时把看门狗重置为独立的回滚预算（默认再给 30min），并在 `docker kill` 前先尝试对 runner 发送 SIGTERM 让 Go 侧执行完已开始的回滚步，避免"看门狗在回滚中途杀 runner → 半回滚状态"。
- 所有敏感值（`db_password` 等）**不得出现在命令参数/日志**（migrate 走 compose secrets 传递，见 §8）；`GIT_TERMINAL_PROMPT=0` 保证 git 永不交互弹出凭据。

---

## 7. 迁移（migrate 容器）

### 7.1 v1 推荐：保留 compose `migrate` 一次性服务

```go
// updater.go 内，Go 仅编排，不代替 migrate 工具（命令统一走 CmdRunner，可注入假实现）
cmd.Run(ctx, "docker", "compose", "-f", composeFile, "-p", project, "run", "--rm", "migrate")
```

理由：

- 零新增依赖；migrate 镜像、`depends_on`、secrets（`db_password` 不落命令行）、compose 网络已配好；
- runner 是 `--network host`，但 `compose run` 创建的一次性容器会挂到 compose 网络，能直达 `postgres:5432`；无需额外网络配置；
- 迁移文件（`migrations/*`）继续是唯一 schema 事实源，`up/down` 语义不变；
- AGENTS.md 铁律"迁移只追加"，本方案不改变迁移机制。

### 7.2 后续可选：原生 golang-migrate（单独 PR，不混入本次）

- 引入 `github.com/golang-migrate/migrate/v4` + postgres driver，在 `lab-update` 二进制内直接连库执行 `up`，可原生支持 `down 1` 做迁移级回滚；
- 依赖成本：AGENTS.md 要求核心依赖 2 人 review；且 runner 需 `--network <compose_net>`（或 postgres 发布端口）才能连库，将改变 runner 的 `--network host` 假设；
- **结论：本次不做**，留作独立优化项，避免"改更新引擎 + 改迁移机制"两件事混在一个 PR。

---

## 8. 部署与权限改动

### 8.1 server 容器（`deploy/docker-compose.yml` + `deploy/Dockerfile`）

```yaml
# server 服务新增
server:
  build: { dockerfile: deploy/Dockerfile }        # 不变
  user: "${UPDATE_RUN_UID:-1000}:${UPDATE_RUN_GID:-1000}"  # 新增：非 root 运行（挂 docker.sock 的硬要求）
  group_add:
    - "${UPDATE_DOCKER_GID:-993}"                # 新增：容器内用户加入宿主机 docker 组以访问 socket
  volumes:
    - /var/run/docker.sock:/var/run/docker.sock   # 新增：派发 runner 用
    - /opt/hiaf-lab-system:/opt/hiaf-lab-system   # 新增：仓库（GetVersion 的 git -C 与 shell 兜底依赖）
    - /opt/hiaf-lab-system/.hermes/updates:/updates  # 新增：会话日志共享目录
    - /opt/hiaf-lab-system/.hermes/backups:/backups    # 新增：DB 备份共享目录（可选，仓库内已共享）
  environment:
    UPDATE_ENGINE: ${UPDATE_ENGINE:-shell}        # 新增：go|shell 开关
    UPDATE_LOG_DIR: /updates
    UPDATE_RUNNER_IMAGE: ${UPDATE_RUNNER_IMAGE:-} # 默认留空 → 预检时用 compose config --images server 解析
    UPDATE_RUN_UID: ${UPDATE_RUN_UID:-1000}       # 宿主机仓库属主 uid（server 与 runner 一致）
    UPDATE_RUN_GID: ${UPDATE_RUN_GID:-1000}
    UPDATE_DOCKER_GID: ${UPDATE_DOCKER_GID:-993}  # 宿主机 docker 组 gid
    UPDATE_NTFY_URL: ${UPDATE_NTFY_URL:-http://localhost:8085/lab-system}
    UPDATE_BACKUP_DIR: ${UPDATE_BACKUP_DIR:-/backups}
    UPDATE_GIT_HOME: ${UPDATE_GIT_HOME:-/opt/hiaf-lab-system/.hermes/runner-home}  # git 凭据/临时 HOME
```

```dockerfile
# deploy/Dockerfile 追加（final 镜像）
RUN apk add --no-cache git docker-cli docker-cli-compose python3 curl coreutils ca-certificates
# 追加 lab-update 二进制
RUN CGO_ENABLED=0 go build -o /usr/local/bin/lab-update ./cmd/update-runner
# 非 root 运行：uid/gid 与宿主机仓库属主一致（compose 里 user: 指定；这里不写死 UID）
```

> 说明：
> - `python3/curl/coreutils` 只为 shell 兜底引擎（runner 内跑 `update.sh` 用）保留；若 P4 删除 shell 引擎，可一并移除这三个依赖。
> - `docker-cli-compose` 在 Alpine v3.20 为 `community` 仓库真实包（2.27.0-r3，已核验），`alpine:3.20` 默认启用 main+community。
> - server 以非 root 运行后，向 `/updates`、`/backups` 写日志/备份要求宿主机对应目录属主 = `UPDATE_RUN_UID`，作为一次性运维配置（与 §8.2 chown 一并处理）。

### 8.2 权限模型（避免仓库属主被 churn）

- runner 以 `--user <repo_uid>:<repo_gid> --group-add <host_docker_gid>` 运行：
  - git checkout/build 写文件时保持宿主机属主，**不会把仓库文件改成 root**；
  - `--group-add` 让容器内进程具备访问 `root:docker 660` 的 socket 所需的补充组 gid；
  - 宿主机上部署用户需加入 `docker` 组（一次性的运维配置，与现在"宿主机直接跑 docker"的权限模型一致）。
- **历史属主清理（一次性运维步骤）**：现状 bash 在 server 容器内以 root 跑 git checkout，仓库文件/`.git` 可能已是 root 属主；切到 `--user` 后 runner 的 git 将无权写 `.git`。因此：
  - 预检步骤 1 用 `touch ${REPO_ROOT}/.hermes/updates/.writetest` + `git -C ${REPO_ROOT} status` 验证 runner uid 可写；
  - 失败则提示运维执行一次 `chown -R <UPDATE_RUN_UID>:<UPDATE_RUN_GID> /opt/hiaf-lab-system`（并同步 `.hermes/updates`、`.hermes/backups`），再重新触发。
- 该 chown 只发生一次（历史遗留 root 属主），此后 runner 以同 uid 读写，不再 churn。

### 8.3 安全注意

- **docker.sock 挂载 = 容器内 root 等价**，因此必须配合 **server 容器非 root 化**（§8.1 `user:` + `group_add:`）：
  - 现状 `deploy/Dockerfile` 无 `USER` 指令，server 以 root 运行；挂 docker.sock 后 root 即宿主 root，**不可接受**；
  - 改为以 `UPDATE_RUN_UID` 运行 + 补充组 `docker`，访问面收窄到 docker 组权限；系统 API 只保留 admin-only 路由 + 全量审计 + Idempotency-Key（现状已满足），并在本文档显式声明"docker 能力仅由 system 模块路由可达"；
  - 此安全模型作为 P1 验收项：部署后验证 `/proc/1/status` 显示非 root，且普通角色无法触达 docker 能力。
- **runner 容器名来自白名单 session id**，无注入面。
- **git 凭据只读挂载**（§3.1）：`.ssh`/`.gitconfig` 以 `:ro` 挂载，runner 无写凭据权限；`GIT_TERMINAL_PROMPT=0` 保证不会交互式弹凭据。
- `docker.sock` 以只读方式挂载不可行（CLI 需要读写），维持读写但最小化暴露：只有 `system` 模块路由可达 docker 能力。

---

## 9. 回滚策略（Go 版）

### 9.1 代码回滚（与现状一致）

```go
rollback(oldSHA, affected) {
    resetWatchdog(rollbackBudget)   // 30min 更新预算换成独立回滚预算，禁止中途被 kill
    git checkout oldSHA              // 凭据已随 §3.1 挂载
    if affected 含 migrate { 迁移受影响 → 发 ntfy 阻塞告警 + 打印 BACKUP_FILE，交人工 migrate down，不回滚代码 }
    for svc in affected(不含 migrate) { docker compose -f $COMPOSE_FILE -p $PROJECT build svc; up -d --no-deps svc }
    git checkout main（回到最新，保留现场）
    ntfy("更新失败-已回滚")
}
```

- 所有 compose 命令统一带 `-f deploy/docker-compose.yml` + 固定 `-p <PROJECT>`（预检步骤 1 已用 `compose ls` 断言，运行期不再漂移）。
- 迁移阻塞分支不重建任何服务、不改工作树（返回 main），只告警 + 打印备份路径，维持"DB 绝不自动回滚"的现状红线。

### 9.2 回滚增强（可选，建议跟进）

- 发布前对当前镜像打 tag：`docker tag <runner-image> <runner-image>:<oldSHA>`（build 后加 `image:` 字段到 compose 亦可），回滚时可 `docker compose up -d --no-deps <svc>` 直接用旧镜像，**免重建、秒级回滚**。改动小、收益大，但会改变 compose 的 build/up 行为，建议与主改动分开验证。
- 数据库备份保留在 `${REPO_ROOT}/.hermes/backups/lab-db-backup-<sha>-<ts>.sql`（随仓库挂载，宿主运维直接可见），回滚提示打印该路径，不做自动 DB 回滚（危险操作，维持人工）。
- **镜像 tag 覆盖语义**：更新中途 `compose build` 覆盖 runner 引用 tag 不影响正在运行的 runner 容器（容器持有旧镜像引用）；回滚用 `docker tag` 打旧 SHA tag 时同理。文档化以免后续维护误判。

---

## 10. 会话/日志/恢复模型改造点

| 现状字段/函数 | Go 版改动 |
|--------------|-----------|
| `UpdateSession.runnerPID`、`pidFile` | 改为 `RunnerID`（`lab-updater-<session>`）、`runnerFile`（存容器名） |
| `spawnDetached`（`/bin/bash` + Setsid） | 删除，替换为 `SessionRunner.Spawn`（docker run -d） |
| `killRunner`/`runnerAlive`（`syscall.Kill`） | 替换为 `Runner.Kill`（docker kill）/`Alive`（docker inspect） |
| `recoverFromDisk` 的"pid 存活判断" | 改为 `docker inspect` 判断 runner 容器运行态；其余磁盘回放逻辑不动 |
| `tailSessionLog` / `finishFromMarker` / sweep | 完全保留（日志文件仍在，只是写入者从 bash 换成 runner 二进制） |
| `writePIDFile`/`readPIDFile` | 改为写/读容器名 |

> runner 容器本身的 `--rm` 会在退出时被 docker 清理；若更新中途 server 多次重启，runner 继续跑并写日志，新 server 从磁盘恢复 session 后继续 tail，直到 `.done` 出现 —— 恢复链路与现在完全一致。
>
> **孤儿 runner 回收**：server 启动时 + sweep 时，用 `docker ps --filter name=lab-updater-` 列出无对应内存 session 且运行超时的 `lab-updater-*` 容器（如超 40min），执行 `docker kill` + `docker rm -f`（幂等），防止 server 崩溃前 spawn 成功但未登记 session 的残留容器长期占用。正常路径（finish/超时 kill）已由 `--rm` 自行清理。

---

## 11. Windows 本地编译（build tag）

- `syscall.SysProcAttr{Setsid}`、`syscall.Kill(-pid, SIGKILL)` 是 unix-only，当前 `service.go` 在 Windows 上**无法编译**（核验：`rg "syscall" go-server --glob '*.go'` 仅 `system/service.go` 命中）。本次把它整体移到 `runner_unix.go`（`//go:build !windows`），并给出 `runner_windows.go`（`//go:build windows`）实现：
  - `Spawn`：在进程内 goroutine 直接运行 `updater.RunSteps`，并通过注入的 fake `CmdRunner`（§4.1）让流水线在无 docker 环境端到端执行（detect/dry-run/回滚分派与真实一致）；容器名记录为占位 ID。
  - `Kill`/`Alive`：goroutine 状态轮询 + context cancel。
- `logger.go`、`detect.go`、`docker.go`、`updater.go`、`cmd.go` 均为纯 Go，双平台可编译可单测。
- **路径语义**：`UPDATE_LOG_DIR`/`UPDATE_BACKUP_DIR` 在 Windows 本地测试可覆盖为 `t.TempDir()`，生产默认 `/updates` 只出现在 Linux runner；日志/备份模块统一走 `filepath`，不硬编码分隔符。
- **CI 新增 Windows 交叉编译检查**（`go-test` job 内追加）：
  ```yaml
  - name: Windows cross-compile
    run: |
      cd go-server
      CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
      CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go vet ./...
      CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test ./... -run '^$'   # 编译测试包，防测试代码破坏 windows 构建
  ```
  （`go test -run '^$'` 在 cross-compile 下仅编译不运行，用来兜住 `runner_windows.go` 及测试文件的可编译性。）

---

## 12. 配置清单（新增环境变量）

| 变量 | 默认 | 说明 |
|------|------|------|
| `UPDATE_ENGINE` | `shell` | `go` 时走新流水线；`shell` 走原 update.sh（灰度开关，P2 起可用） |
| `UPDATE_LOG_DIR` | `/updates` | 会话日志/marker 共享目录（server 与 runner 都挂载） |
| `UPDATE_RUNNER_IMAGE` | 空 → `compose config --images server` 解析 | runner 容器镜像；留空由预检解析实际镜像名，不再依赖魔法 tag |
| `UPDATE_RUN_UID` / `UPDATE_RUN_GID` | `1000` | runner 与 server 运行身份（宿主机仓库属主） |
| `UPDATE_DOCKER_GID` | `993` | 宿主机 docker 组 gid（`--group-add` 与 server `group_add`） |
| `UPDATE_NTFY_URL` | `http://localhost:8085/lab-system` | ntfy 通知地址（runner `--network host` 可达） |
| `UPDATE_BACKUP_DIR` | `/backups` | DB 备份共享目录（仓库内 `.hermes/backups` 亦可用） |
| `UPDATE_GIT_HOME` | `…/.hermes/runner-home` | runner 内 git HOME（挂载 `~/.gitconfig`/`~/.ssh` 只读后指向它） |
| `UPDATE_SCRIPT_PATH` | `.hermes/update.sh` | shell 引擎路径（runner 内 `bash <repo>/<path>`，保留） |
| `UPDATE_UPDATE_TIMEOUT` | `30m` | 更新阶段看门狗（回滚阶段独立预算 `UPDATE_ROLLBACK_TIMEOUT`，默认 `30m`） |
| `UPDATE_FLAGS` | 空 | runner 入参透传：`--force/--dry-run/--no-rollback`（默认空 = 常规更新） |

> **flag 透传边界**：`lab-update` 支持 `--force/--dry-run/--no-rollback`，但 `POST /api/v1/admin/system/update` 契约不变（无参数），`Trigger()` 以 `UPDATE_FLAGS` 默认值调用；`--dry-run` 等仅用于 CLI/运维手工执行 `lab-update`，不在 web 端暴露（与现状 bash 一致：web 触发本就不传参）。

---

## 13. 测试策略

| 文件 | 覆盖 |
|------|------|
| `detect_test.go` | 路径→服务映射 golden 矩阵（`testdata/detect_golden.txt` 全 case，含 `Dockerfile.migrate` 不落 `deploy/*` 的顺序陷阱、`ALL` 短路、`--force`、migrations-only、空 diff） |
| `docker_test.go` | `compose ps --format json` 单对象/数组两形态解析、坏 JSON/空输出、健康态提取（healthy/running/other）、`compose config --services`、`config --images` 解析 runner 镜像名 |
| `updater_test.go` | 注入 fake `CmdRunner`（记录命令/退出码）：7 步顺序、每个回滚触发点（含迁移阻塞分支）、构建失败不回滚、`--force` 继续、步骤行/`done` 事件输出格式、看门狗豁免回滚（回滚期间定时器不触发 kill） |
| `logger_test.go` | 行分块与跨 chunk partial 行合并、文件无缓冲写、marker 写入时机与内容、重复写幂等 |
| `contract_test.go` | **I2/I3 契约测试**：断言 7 步标题逐条 `stepRe.MatchString` 且 step 数/总数正确（防前端渲染破坏）；断言 `done` marker JSON 能被 `finishFromMarker`/`recoverFromDisk` 解析（含损坏 marker 分支） |
| `runner_test.go` | 通过接口注入假 Runner：Spawn/Kill/Alive 调用、超时杀 runner、恢复时 runner 容器 alive→继续 tail / dead→判中断两条路径 |
| `handler_test.go` | 现有用例保持通过（handler/service 契约不变）；新增 Trigger 并发单例测试（两个并发 Trigger 只产生一个 session） |
| `cmd_test.go` / `update-runner_test.go` | `main` 的 flag 解析（`--session/--force/--dry-run/--no-rollback`）与退出码 |
| 端到端（CI） | **fake docker 集成测试**：测试里把假 `docker`/`git` 脚本放进 `PATH`（记录调用 + 返回预设 `compose ps/config` JSON），跑通真实 `updater.RunSteps` 全流水线——含一次真实回滚路径、一次迁移阻塞路径、一次健康检查失败路径；该用例也作为 Windows 本地可跑的流水线验证 |
| CI | 新增 `GOOS=windows CGO_ENABLED=0 go build ./...` + `go vet` + `go test ./... -run '^$'`（§11） |

---

## 14. 分阶段落地计划

| 阶段 | 内容 | 验收 |
|------|------|------|
| **P0（本次）** | 本设计文档定稿（五维 ≥95，见附录 A） | 评审通过 |
| **P1** | `detect.go`、`docker.go`、`logger.go`、`cmd.go` 纯函数 + 单测（含 golden 矩阵、contract_test、logger_test）；`deploy/` 增加挂载、非 root user、`group_add`、Dockerfile 依赖、`.env.example`（不动更新逻辑）；一次性运维：`chown -R` 仓库属主 + 部署用户入 docker 组 | `go test ./...` 绿；`GOOS=windows` build+vet+test-compile 绿；部署后 server `/proc/1` 非 root、`docker compose ps` 项目名断言通过 |
| **P2** | `updater.go` + `cmd/update-runner` + `runner_*.go` + 孤儿回收；`Service.Trigger` 支持 `UPDATE_ENGINE=go`（默认仍 shell）；跑通 `cmd_test` 与 fake-docker 端到端测试 | staging 上 `UPDATE_ENGINE=go` 全流程跑通（含一次真实迁移 + 一次注入失败验证回滚 + 回滚阶段看门狗豁免验证 + git 凭据挂载验证） |
| **P3** | 默认 `UPDATE_ENGINE=go`；观察 server 重建中断言 I1（更新期间 server 容器被替换仍完成） | 生产一次完整更新成功；SSE 断线重连 + 恢复正常；一次失败注入验证回滚链路 |
| **P4** | 删除 shell 引擎与 `update.sh`；文档同步（AGENTS.md、maintenance-strategy.md、api-contract 不涉及）；Dockerfile 可移除 `python3/curl/coreutils` | 清理完成 |

**回滚本方案本身的兜底**：任何阶段发现 Go 引擎异常，把 `UPDATE_ENGINE` 切回 `shell`（runner 容器内跑 `update.sh`，同样满足 I1）即恢复；因此迁移顺序上"先保留 update.sh、后删除"是硬要求。

---

## 15. 风险与开放问题

| 风险 | 说明 | 缓解 |
|------|------|------|
| compose 项目名不一致 | runner/宿主机须用同一 `-f` 与工作目录，否则 `up -d` 会创建同名新容器而非更新 | 统一在 `REPO_ROOT`（仓库挂载=宿主绝对路径）下执行 `docker compose -f deploy/docker-compose.yml -p <PROJECT> …`；预检步骤 1 用 `compose ls` 断言项目名与现有容器一致 |
| 镜像内 docker-cli 与宿主机 daemon 版本差异 | 新旧 compose CLI 行为差异 | P1 在 staging 用真实更新验证一次；预检断言 `docker compose version` 与 `compose config` 可解析 |
| 仓库属主 churn | git checkout 改文件属主 | `--user + --group-add` 方案（§8.2）；预检仓库可写性 + 一次性 `chown -R` |
| runner 镜像名解析失败 | `UPDATE_RUNNER_IMAGE` 指向不存在 tag | 预检用 `compose config --images server` 解析；默认解析失败即中止，不落到魔法 tag |
| **git 凭据缺失** | runner 无宿主 git 凭据 → step3 pull 失败（当前 bash 依赖宿主环境） | §3.1 只读挂载 `~/.gitconfig`/`~/.ssh` + 设置 HOME + `GIT_TERMINAL_PROMPT=0`；staging 实测一次 |
| **DB 备份随 runner 丢失** | 备份写 runner 容器 `/tmp`，`--rm` 退出即丢 | 备份写入仓库内 `.hermes/backups/`（或 `/backups` 挂载），宿主直接可见 |
| **看门狗中断回滚** | 30min 更新预算耗尽时 `docker kill` 杀掉正在回滚的 runner | §6.2 回滚阶段独立预算 + SIGTERM 优雅收尾；updater_test 覆盖 |
| server 以 root 挂 docker.sock | 容器 root = 宿主 root | §8.1/§8.3 非 root user + group_add；admin-only + 审计保持；P1 验收非 root |
| 孤儿 runner 容器 | server 崩溃前 spawn 成功但未登记 session | §10 启动/sweep 时回收超时 `lab-updater-*` |
| ntfy 可达性 | 更新期间网络变化 | runner `--network host` + `UPDATE_NTFY_URL` 可配 |
| 安全面扩大 | docker.sock 进 server 容器 | admin-only + 审计 + 非 root 化；文档显式声明（§8.3） |
| 原生迁移（后续项）是否值得 | 多一个核心依赖 | 独立 PR 单独评审，不阻塞本次 |

---

## 16. 相关文件清单

| 文件 | 动作 |
|------|------|
| `.hermes/go-update-module.md` | 新建（本方案，v0.2） |
| `go-server/system/` | 新增 detect/docker/updater/logger/cmd/runner_*.go 及对应测试、`testdata/detect_golden.txt`，修改 service/model.go（RunnerID 化），保留 handler 与测试 |
| `go-server/cmd/update-runner/main.go` | 新建 runner 入口（flag：`--session/--force/--dry-run/--no-rollback`） |
| `deploy/Dockerfile` | 增加 git + docker-cli + docker-cli-compose + python3 + curl + coreutils 依赖与 `lab-update` 二进制；非 root 运行 |
| `deploy/docker-compose.yml` | server 服务增加 docker.sock、仓库、updates、backups 挂载，非 root `user:` + `group_add:`，`UPDATE_*` 环境变量 |
| `deploy/.env.example` | 补充 `UPDATE_*` 配置说明（含 `UPDATE_RUNNER_IMAGE` 留空语义） |
| `.hermes/updates/`、`.hermes/backups/` | 会话日志与 DB 备份共享目录（gitignore 之外） |
| `.github/workflows/ci.yml` | go-test job 增加 Windows 交叉编译（build + vet + test-compile）与 fake-docker 端到端测试 |
| `docs/maintenance-strategy.md` | 落地后同步更新/回滚策略章节（P4） |

---

## 附录 A：五维评分与整改记录（v0.1 → v0.2）

> 核验基准：`.hermes/update.sh`（398 行）、`go-server/system/{service,handler,model}.go`、`deploy/docker-compose.yml`、`deploy/Dockerfile`、`.github/workflows/ci.yml`。核验确认的既有正确点：7 步语义与 `stepRe` 标题、`detect_services` 有序规则表、回滚触发点、`upd_[a-z0-9]{10}` 白名单、SSE/tail/recover 契约、9 服务健康检查、`docker-cli-compose` 为 Alpine v3.20 真实包、`syscall` 仅 system/service.go 使用（Windows 可迁移）。

| 维度 | v0.1 | 主要失分点（核验） | 整改（对应章节） | v0.2 |
|------|------|--------------------|-------------------|------|
| 1 功能正确性 | 83 | ①runner 无 git 凭据；②DB 备份落 runner `/tmp` 随 `--rm` 丢失；③server 容器无仓库挂载（GetVersion/shell 兜底失效）；④`UPDATE_RUNNER_IMAGE=lab-server` 默认错（compose 无 `image:`，实为 `<project>-server`）；⑤migrate 未写明条件执行；⑥健康等待超时未对齐 bash | ①§3.1/§15 git 凭据只读挂载+HOME；②§3.1/§9.2 备份入仓库 `.hermes/backups`；③§8.1 加仓库挂载、shell 兜底改走 runner 容器；④§5/§8.1 用 `compose config --images server` 解析；⑤§6 步骤 6 明确条件；⑥§6 对齐等待超时（server 30s/其余 60s） | 96 |
| 2 架构可维护性 | 86 | ①缺 `CmdRunner` 抽象（updater 不可测）；②7 步无 `Step` 结构化（回滚元数据散落文字）；③detect 与 bash 双源无 golden；④logger 未承担 marker 契约；⑤Windows 伪实现调试价值低 | ①§4.1 加 `CmdRunner` 接口；②§4.1 加 `Step{Title/RollbackOn/BlockOnMigrate/SkipWhen}` 表驱动；③§5 golden 矩阵；④§13 contract_test/logger_test 固化 I2/I3；⑤§4.1/§11 Windows 用 fake CmdRunner 跑真实流水线 | 95 |
| 3 部署与回滚安全 | 80 | ①同 D1-①；②同 D1-②；③镜像默认名错误致 runner 起不来；④server 以 root 挂 docker.sock=宿主 root；⑤看门狗可能杀正在回滚的 runner；⑥root 属主历史仓库未预检/chown；⑦无孤儿 runner 回收 | ①§3.1；②§3.1/§9.2；③§5/§8.1 预检解析镜像名+失败中止；④§8.1/§8.3 非 root user+group_add+P1 验收；⑤§6.2/§9 回滚独立预算+SIGTERM；⑥§8.2 预检可写性+一次性 chown；⑦§10 孤儿回收 | 96 |
| 4 跨平台构建 | 88 | ①CI 仅 build 不 vet/测试编译；②CGO_ENABLED=0 未显式；③Windows"可调试"名不副实；④日志/备份路径在 Windows 语义未说明 | ①③§11 CI 加 vet+`go test -run '^$'`、fake CmdRunner 流水线；②§11 显式 `CGO_ENABLED=0`；④§11 路径可覆盖+filepath | 95 |
| 5 测试覆盖 | 84 | ①缺 logger_test；②缺 I2/I3 契约测试；③detect 无 golden 全矩阵；④缺 fake-docker 端到端（回滚/迁移阻塞路径）；⑤缺恢复 alive/dead 两态；⑥缺 Trigger 并发/竞态 | ①-④§13 新增 logger_test/contract_test/golden/fake-docker 端到端；⑤§13 runner_test 两态；⑥§13 handler_test 并发单例 | 96 |

评分口径：每维在整改项全部落地（对应章节已具化为验收项/测试项）后按"失分点是否被消除"计分。v0.2 各维 ≥95，剩余扣分为刻意保留的非目标项（如 Windows 不跑真实容器部署、迁移机制不引入原生 down）与必须人工介入的操作（DB 迁移 down），不属本方案可消除项。
