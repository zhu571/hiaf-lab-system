# 前端系统更新功能 — 设计方案

> 版本：v3  
> 目标：Settings 页面新增"系统更新"卡片，管理员可查看版本、一键触发 update.sh，并通过 SSE 实时查看更新日志。
> 修订：v2 补齐 SSE 文件回放 + session 重建、带缓冲非阻塞广播、time.Ticker、goroutine 泄漏防护与脚本超时、重连退避与连接数上限、token 过期重连处理。
> 修订：v3 修复 5 个残留 —— ① update.sh 以 setsid + 独立 runner 脱离 Go/容器，server 重启不杀脚本；② 回放 >512 行与 done 竞态，done 事件改为回放结束后投递；③ token 仅在 401 时刷新（fetch 型 SSE 可识别状态码）+ `refreshPromise` 单例；④ `unsubscribe` 关闭 ch 且补齐 TTL sweep 实现；⑤ update.sh 管道行缓冲 + git 命令超时。

---

## 1. 功能概览

```
┌─ Settings 页面 ──────────────────────────────────────────────┐
│  ┌─ 个人设置 ──────────────────────────────────────┐        │
│  │  头像 / 用户名·角色 / 修改密码 / 语言切换       │        │
│  └─────────────────────────────────────────────────┘        │
│                                                              │
│  ┌─ 系统更新 (仅 admin 可见) ───────────────────────┐       │
│  │  当前版本  abc1234  ───►  最新版本  def5678      │       │
│  │  [检查更新]  [开始更新]  ← 更新中禁用            │       │
│  │  ┌──────────────────────────────────────┐        │       │
│  │  │  [UPDATE] ===== 步骤 1/7：预检 ===== │ ◄───── │ 日志流
│  │  │  [UPDATE] 当前 commit: abc1234        │  SSE   │
│  │  │  [UPDATE] ===== 步骤 2/7：... =====   │        │
│  │  │  ...                                  │        │
│  │  │  [UPDATE]   更新成功！abc1234 → def   │        │
│  │  └──────────────────────────────────────┘        │       │
│  │  ● 运行中...  共 42 行                           │       │
│  └──────────────────────────────────────────────────┘       │
└──────────────────────────────────────────────────────────────┘
```

### 交互流程

1. 管理员打开 Settings 页面，看到"系统更新"卡片。
2. 卡片自动加载当前版本（`GET /version`）。
3. 点击「检查更新」→ 显示远程最新 commit SHA 及落后数。
4. 点击「开始更新」→ `POST /update` 触发，拿到 `session_id`。
5. 立即连接 `GET /stream/{session_id}`（SSE），日志逐行吐出。
6. 日志区域自动滚到底部。
7. 更新完成 → 显示"更新成功"或"更新失败"。
8. 若 server 重启导致 SSE 断开 → 前端展示"连接中断，等待服务恢复…"，轮询 `/health` 后自动重连。

---

## 2. 后端 API 设计

### 2.1 模块位置

新建 `go-server/system/`，按项目模块规范分文件：

```
go-server/system/
├── handler.go       # HTTP handler
├── service.go       # 业务逻辑、exec update.sh、日志流
├── model.go         # 请求/响应类型
└── handler_test.go  # 测试
```

### 2.2 路由注册（main.go）

```go
// 系统更新 — admin only
r.Route("/api/v1/admin/system", func(r chi.Router) {
    r.Use(mw.AuthRequired)
    r.Use(mw.RequireRole(auth.RoleAdmin))

    // 版本查询 — 只读，无审计/幂等
    r.Get("/version", systemHandler.GetVersion)

    // 触发更新 — 写操作，需审计+幂等
    r.Group(func(r chi.Router) {
        r.Use(mw.Audit(db))
        r.Use(mw.RequireIdempotencyKey(db))
        r.Post("/update", systemHandler.TriggerUpdate)
    })

    // SSE 日志流 — 流式，无审计/幂等
    r.Get("/update/stream/{sessionId}", systemHandler.UpdateStream)
})
```

> 遵循 `.hermes/update.sh` 的 `SCRIPT_DIR/REPO_ROOT` 约定：Go 后端从环境变量 `REPO_ROOT`（默认 `/opt/hiaf-lab-system`）拼接脚本路径。

### 2.3 API 详细定义

#### GET /api/v1/admin/system/version

**请求**：无参数

**响应**（200）：
```json
{
  "data": {
    "current": "abc1234",
    "current_short": "abc1234",
    "latest": "def5678",
    "latest_short": "def5678",
    "behind": 3,
    "can_update": true
  }
}
```

**服务端逻辑**：
```go
func (s *Service) GetVersion() (*VersionInfo, error) {
    current := gitRevParse("HEAD")           // git rev-parse HEAD
    latest  := gitRevParse("origin/main")    // git ls-remote origin HEAD | awk '{print $1}'
    behind  := gitRevListCount(current, "origin/main")  // git rev-list --count HEAD..origin/main
    return &VersionInfo{...}, nil
}
```

`git ls-remote` 需要网络可达；若失败则 `latest` 为空字符串，`can_update` 为 false。

#### POST /api/v1/admin/system/update

**请求**：无请求体（可选 `{"force": true}` 未来扩展）

**请求头**：`Idempotency-Key`（必填）

**响应**（200）：
```json
{
  "data": {
    "session_id": "upd_a1b2c3d4e5",
    "current": "abc1234"
  }
}
```

**错误**（409）：
```json
{
  "error": {
    "code": "update_in_progress",
    "message": "已有更新任务正在执行",
    "details": { "session_id": "upd_xxxxxxxx" }
  }
}
```

**服务端逻辑**：
1. 检查单例锁是否空闲（同一时刻只允许一个更新任务）。
2. 生成 `sessionId = "upd_" + nanoid(10)`。
3. 创建 log buffer（环形缓冲，最多保留 5000 行）+ 在共享日志目录（宿主 `/tmp`，见 2.4 部署前置）创建占位日志文件 `/tmp/lab-update-{sessionId}.log`。
4. **以 `setsid` 脱离 Go 父进程、脱离 server 容器启动独立 runner**（见 2.4「server 自重启场景」）：通过挂载的 docker socket 以 `docker run --rm -d` 起独立容器执行 `update.sh`，或直接以 `setsid` 新会话在宿主命名空间执行。传环境变量 `UPDATE_SESSION_ID`、`UPDATE_LOG_FILE`、`UPDATE_DONE_FILE`。
5. Go 侧启动 **tail goroutine**（200ms 增量轮询日志文件 + 轮询 `.done` marker，见 4.2 `runScript`）：每读到新行 → stripANSI → 解析 step → 写入 RingBuffer → 带缓冲非阻塞广播给 SSE 订阅者。文件写入由脚本自身完成（`tee` 行缓冲），Go 只读不写，进程重启不丢日志。
6. tail goroutine 检测到 `.done` marker（脚本在退出 trap 中写入，内容含 `exit_code/old_sha/new_sha`）→ 解析 → 构造 `doneEvent` → 广播 + `close(done)` + 标记 Status=done。
7. 立即返回 sessionId。

**审计**：记录为 `action = "system.update.trigger"`，details 含 `session_id`。

#### GET /api/v1/admin/system/update/stream/{sessionId}

**请求**：`Accept: text/event-stream`

**响应**：SSE 流，Content-Type `text/event-stream`。

**SSE 帧格式**：

```
id: {seq}
data: {"seq":1,"ts":"2026-07-31T10:00:01+08:00","type":"line","text":"[UPDATE] ===== 步骤 1/7：预检 ====="}

id: {seq}
data: {"seq":2,"ts":"...","type":"step","step":1,"step_total":7,"title":"预检"}

id: {seq}
data: {"seq":3,"ts":"...","type":"line","text":"[UPDATE] 当前 commit: abc1234"}

...

id: {seq}
data: {"seq":N,"ts":"...","type":"done","exit_code":0,"success":true,"old_sha":"abc1234","new_sha":"def5678"}
```

**帧类型说明**：

| type | 说明 | 字段 |
|------|------|------|
| `line` | 普通日志行 | `text` — 已去除 ANSI 转义码 |
| `step` | 步骤标记（解析自 `步骤 X/7`） | `step`, `step_total`, `title` |
| `done` | 更新完成 | `exit_code`, `success`, `old_sha`, `new_sha` |
| `error` | 内部错误（如脚本启动失败） | `message` |
| `keepalive` | 心跳（~15秒空 cycle 后发送） | `: keepalive\n\n` |

**history 重连**：首次连接时，将历史日志全量推送（每帧一条），seq 从 1 开始。后续新日志实时推送。历史来源优先级：

1. **内存 RingBuffer**（同进程内重连，首选，最快）。
2. **日志文件回放**（Go 进程已重启、内存 session 丢失）：从 `/tmp/lab-update-{sessionId}.log` 读取已有行重建事件序列。
3. **文件 marker**：若同时存在 `/tmp/lab-update-{sessionId}.done`，据此推断 session 已结束，回放完历史后推送 `done` 事件。

**连接数上限**：每个 session 最多允许 `maxSubs`（默认 4）个并发 SSE 订阅者，超出返回 409 `too_many_subscribers`，防止多 tab 或误触拖垮服务。

**服务端实现要点**：
```go
type UpdateSession struct {
    ID        string
    Status    string  // "running" | "done" | "error"
    ExitCode  int
    OldSHA    string
    NewSHA    string
    LogBuffer *RingBuffer  // 固定大小的环形日志缓冲（内存回放）
    history   []SSEEvent   // recoverFromDisk 重建的历史事件序列
    doneEvent SSEEvent     // finish 时记录的最终 done 事件（重连回放用）
    logFile   string      // /tmp/lab-update-{id}.log，跨进程持久化（脚本 tee 写入）
    doneFile  string      // /tmp/lab-update-{id}.done
    subs      map[chan SSEEvent]struct{}
    subsCount int
    maxSubs   int        // 订阅者上限（注入 Service.maxSubs）
    seq       int        // 已发出的事件序号（done 事件用 seq+1）
    done      chan struct{}
    once      sync.Once  // finish 幂等保护
    DoneAt    time.Time  // 结束时间（用于 TTL sweep）
    mu        sync.Mutex

    // v3 新增（setsid 脱离运行 + 文件 tail + TTL）
    runnerPID    int            // 宿主命名空间执行时的进程组 PID（setsid 后）
    timeoutTimer *time.Timer    // 超时看门狗，finish 时 Stop
    tailing      bool           // tail goroutine 是否已在跑（recoverFromDisk 防重复启动）
}

// nextSeq 生成递增事件序号（线程安全）
func (s *UpdateSession) nextSeq() int {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.seq++
    return s.seq
}

// ring buffer holds up to 5000 lines
type RingBuffer struct {
    lines [][]byte
    head  int
    size  int
    cap   int
    mu    sync.RWMutex
}

// Snapshot 返回按写入顺序的原始行切片（重连回放用）
func (rb *RingBuffer) Snapshot() []string
```

**步骤解析器**：正则提取更新脚本中的步骤标记和 tag：
```go
var stepRe = regexp.MustCompile(`===== 步骤 (\d+)/(\d+)：(.+) =====`)
var tagRe  = regexp.MustCompile(`^\[(UPDATE|WARN|ERROR)\] (.+)`)
```

解析示例：
```
"[UPDATE] ===== 步骤 1/7：预检 ====="
→ step事件 {step:1, step_total:7, title:"预检"}
→ line事件 {text:"[UPDATE] ===== 步骤 1/7：预检 ====="}

"[UPDATE] 当前 commit: abc1234"
→ line事件 {text:"[UPDATE] 当前 commit: abc1234"}

"[ERROR] Docker 未安装或不在 PATH"
→ line事件 {text:"[ERROR] Docker 未安装或不在 PATH"}

"  OK  server: healthy"
→ line事件 {text:"  OK  server: healthy"}

"[UPDATE]   更新成功！abc1234 → def5678"
→ line事件
→ 脚本退出 exitCode=0 → done事件
```

### 2.4 关键边界条件

#### 并发控制
- 全局单例 `sync.Mutex`，POST `/update` 先 TryLock，失败返回 409。
- 允许多个 SSE 客户端同时订阅同一 session（如多个 tab 打开）。

#### Server 自重启场景 — 文件回放 + session 重建

- update.sh 第 6 步会 `docker compose stop server` + `up -d server`，当前 Go 进程会被 kill。
- SSE 连接断开，前端自动进入"重连中"状态（见 3.4 重连退避）。
- 新 Go 进程启动后，前端重新连接 SSE。

**核心机制：更新脚本脱离 server 容器运行，日志文件与内存状态解耦。**

`runScript` 必须以 `setsid`（新会话 + 新进程组）spawn 脚本，并保证脚本**不在 server 容器内**运行，否则 `docker compose stop server` 会连 PID namespace 一起杀掉脚本（setsid 在容器内无法对抗容器停止）。两种落地方式：

1. **docker socket 独立 runner**（推荐）：server 容器挂载 `/var/run/docker.sock` 与共享日志目录，Go 以 `docker run --rm -d` 起独立容器执行 `update.sh`（容器内挂载仓库目录 + docker socket + 宿主 `/tmp`）。脚本在自己独立的 PID namespace 运行，server 容器重启不影响它。
2. **宿主命名空间直接执行**：server 容器以 `--pid=host` 运行或脚本经 ssh 到宿主执行；Go 以 `syscall.SysProcAttr{Setsid: true}` 创建新会话，Go 进程死亡不向脚本转发信号。

两种方式下脚本都以**日志文件为唯一输出通道**：脚本自身将每行输出 tee 到 `/tmp/lab-update-{sessionId}.log`（行缓冲），Go 侧 tail 该文件重建事件；脚本结束在 EXIT trap 中写 `.done` marker。Go 进程重启只丢 tail 游标，不丢日志。

**部署前置（`deploy/docker-compose.yml` + `deploy/Dockerfile`）**：
- server 服务挂载 `/var/run/docker.sock:/var/run/docker.sock`（只读 socket 写权限）与宿主日志目录（如 `/tmp/lab-update:/tmp`）与仓库目录（`/opt/hiaf-lab-system`）。
- server 镜像需含 `bash`、`git`、`timeout`、`stdbuf`（`coreutils`）、`curl`（alpine 基础镜像需 `apk add bash git coreutils`；仅当走方案 1 时 Go 只调用 docker，runner 容器内才需要这些工具）。
- 供 runner 使用的最小镜像（可复用 `alpine + git + docker-cli + bash + coreutils`）。

**触发时的写入路径（`runScript`）**：
1. `sessionId` 生成后立即在共享日志目录创建 `/tmp/lab-update-{sessionId}.log`（空文件占位）。
2. Go 以 `setsid` 启动独立 runner 执行 `update.sh`，传 `UPDATE_SESSION_ID`、`UPDATE_LOG_FILE`、`UPDATE_DONE_FILE` 环境变量。
3. **update.sh 自身**把 stdout/stderr 行缓冲重定向到日志文件（`exec > >(stdbuf -oL tee -a "$UPDATE_LOG_FILE")`），每行即时落盘（见 .hermes/update.sh 改动）。
4. Go 的 tail goroutine 增量读日志文件 → `stripANSI` → 解析 step → **同时**：
   - `LogBuffer.Append(line)`（内存）
   - 记录 tail 偏移（新 session 从 0 起读；进程重启后恢复的 running session 从文件末尾续读，seq 接续历史，见 4.2 `recoverFromDisk`）
5. 脚本结束（成功/失败/被 kill）→ 脚本 EXIT trap 写 marker `/tmp/lab-update-{sessionId}.done`，内容为 JSON：
   ```json
   {"exit_code":0,"old_sha":"abc1234","new_sha":"def5678","ended_at":"2026-07-31T10:05:00+08:00"}
   ```

**读取路径（`Subscribe`）— session 重建**：

```
Subscribe(sessionID)
 ├─ Service.sessions[sessionID] 存在（内存未重启）
 │    ├─ Status == running → 订阅实时流，先回放 RingBuffer 历史
 │    └─ Status == done     → 回放 RingBuffer + 推 done 事件，关闭
 └─ 内存不存在（进程已重启）→ 文件重建 recoverFromDisk(sessionID)
      ├─ .log 存在 + .done 存在 → 逐行读回 → 生成 SSEEvent 序列 → 追加 done 事件
      ├─ .log 存在但 .done 不存在 → 检查 runner 是否仍存活
      │    ├─ 仍存活（docker inspect / 进程组存活）→ Status=running，
      │    │   继续 tail 同一文件，等脚本结束写 marker（不判中断！）
      │    └─ 已消亡 → 追加 error 事件 {message:"服务重启导致更新中断，请重新触发"}
      └─ .log 不存在 → 404 session_not_found
```

**重建后的 session 生命周期**：重建的 session 注册进 `Service.sessions` 但**不**启动新脚本 goroutine；若 runner 仍存活则继续 tail（`recoverFromDisk` 返回 running 状态，由现有 Subscribe 实时流分支接管）。回放结束后 handler 关闭，无 goroutine 泄漏。

**文件写入一致性**：日志文件只由 update.sh（tee）写入，Go 侧只有 tail goroutine 一个 reader（200ms 轮询，`os.File.Seek` 增量读），不允许多 goroutine 直接写文件句柄。文件读取失败不阻断内存广播（记录 slog.Warn，`can_update` 语义不变）。

**清理**：session 完成（done/error）后，内存中的 session 保留 1 小时（`Service.sessions` 内 `DoneAt` + 定时 sweep goroutine，实现见 4.2 `sweepSessions`）供重连回放；超过 TTL 或进程重启后由磁盘文件兜底。临时文件不主动删除（单次更新 < 1MB，由系统 `/tmp` 清理）。

#### 错误处理
| 场景 | HTTP 状态 | 错误码 | 说明 |
|------|-----------|--------|------|
| 非 admin 访问 | 403 | `permission_denied` | RequireRole 中间件 |
| 更新已在进行 | 409 | `update_in_progress` | 返回已有 sessionId |
| session 不存在 | 404 | `session_not_found` | SSE 连接时（内存与磁盘均无） |
| 订阅者过多 | 409 | `too_many_subscribers` | 单 session SSE 订阅 > `maxSubs` |
| 脚本路径不存在 | 500 | `script_missing` | Trigger 时 |
| 脚本启动失败 | 500 | `script_start_failed` | Trigger 时 |
| 脚本超时被 kill | 日志流中 `error` 事件 | — | 超时（默认 30min）看门狗 kill 整个进程组（`syscall.Kill(-pgid)` / `docker stop -t 0`） |
| 进程重启导致中断 | 日志流中 `error` 事件 | — | 文件重建时 `.done` 缺失且 runner 已消亡 |
| git fetch 超时 | 在日志流中体现 | — | update.sh 内 `timeout $GIT_TIMEOUT git ...`，脚本 exitCode ≠ 0 |

### 2.5 安全约束

- **角色**：仅 admin，中间件 `RequireRole(auth.RoleAdmin)` 统一拦截。
- **审计**：Trigger 必须记录（`system.update.trigger`），SSE 流不审计。
- **幂等**：Trigger 要求 `Idempotency-Key`（并发控制 + session 单例已天然幂等）。
- **不写入数据库**：更新日志仅存于内存环形缓冲 + 临时文件（容器重启后清理）。
- **不修改业务数据**：系统更新模块不操作 PostgreSQL（无 repository.go，如有需要可加 audit_log 写入）。

---

## 3. 前端实现

### 3.1 新文件

```
web-ui/src/api/system.ts   — 系统更新 API
```

### 3.2 API 层 (`web-ui/src/api/system.ts`)

```typescript
import { request, requestWithMeta } from './client'

export interface VersionInfo {
  current: string
  current_short: string
  latest: string
  latest_short: string
  behind: number
  can_update: boolean
}

export interface UpdateTriggerResult {
  session_id: string
  current: string
}

export interface SSELineEvent {
  seq: number
  ts: string
  type: 'line'
  text: string
}

export interface SSEStepEvent {
  seq: number
  ts: string
  type: 'step'
  step: number
  step_total: number
  title: string
}

export interface SSEDoneEvent {
  seq: number
  ts: string
  type: 'done'
  exit_code: number
  success: boolean
  old_sha: string
  new_sha: string
}

export type SSEEvent = SSELineEvent | SSEStepEvent | SSEDoneEvent

/** 获取版本信息 */
export function getVersion() {
  return request<VersionInfo>({ url: '/admin/system/version' })
}

/** 触发系统更新，返回 session_id */
export function triggerUpdate() {
  return requestWithMeta<UpdateTriggerResult>({
    url: '/admin/system/update',
    method: 'POST'
  })
}

/** 刷新会话：旋转 access_token Cookie（仅在 SSE 401 时调用，见 connectUpdateStream） */
export function refreshSession() {
  return request<{ csrf_token?: string }>({ url: '/auth/refresh', method: 'POST' })
}

/** SSE 连接句柄 */
export interface UpdateStreamHandlers {
  onEvent: (event: SSEEvent) => void
  /** 服务端返回 401 —— 仅此时才需要刷新 token */
  onAuthError: () => void
  /** 网络/服务中断（非 401），无需刷新 token，直接按退避重连 */
  onNetworkError: () => void
}

/** 建立 SSE 连接，返回断开函数
 *  用 fetch + ReadableStream 而非 EventSource，原因：
 *  1. 需要识别 HTTP 状态码 —— 401（token 过期）只在这一种情况下才刷新 token；
 *     EventSource 的 onerror 拿不到状态码，无法区分"401"与"网络断"；
 *  2. 不依赖 EventSource 固定 3s 自动重连（无法退避），
 *     断开后由调用方 close() 并自行指数退避重连（见 SettingsView.connectStream）。
 *  Cookie 由 credentials: 'include' 自动携带（HttpOnly access_token），无需手动加头。
 */
export function connectUpdateStream(
  sessionId: string,
  handlers: UpdateStreamHandlers
): { close: () => void } {
  const controller = new AbortController()
  let closed = false

  ;(async () => {
    try {
      const res = await fetch(`/api/v1/admin/system/update/stream/${sessionId}`, {
        headers: { Accept: 'text/event-stream' },
        credentials: 'include',
        signal: controller.signal,
      })
      if (res.status === 401) {          // token 过期 —— 唯一需要刷新 token 的场景
        handlers.onAuthError()
        return
      }
      if (!res.ok || !res.body) {        // 409 等其它错误按网络层处理
        handlers.onNetworkError()
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      while (!closed) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        let idx
        while ((idx = buf.indexOf('\n\n')) >= 0) {   // SSE 帧以空行分隔
          const frame = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          const dataLine = frame.split('\n').find(l => l.startsWith('data:'))
          if (!dataLine) continue        // 跳过 keepalive 注释帧 / id: / event: 行
          try {
            handlers.onEvent(JSON.parse(dataLine.slice(5).trim()) as SSEEvent)
          } catch { /* 忽略解析失败的帧 */ }
        }
      }
      handlers.onNetworkError()          // 流被服务端关闭（服务重启/脚本结束但未收到 done）
    } catch (e) {
      if (!closed && (e as Error).name !== 'AbortError') handlers.onNetworkError()
    }
  })()

  return { close: () => { closed = true; controller.abort() } }
}
```

> 注意：access_token 通过 HttpOnly Cookie 携带，`fetch` 需 `credentials: 'include'`。
>
> **token 过期重连处理**：access_token 有效期 15 分钟，更新过程可能更长。**仅当** SSE 返回 401 时才调 `POST /auth/refresh` 旋转 Cookie；网络中断不刷新 token，直接按退避重连。并发多次 401 通过 `refreshPromise` 单例共享同一次 refresh（见 3.4），避免风暴。refresh 失败（refresh_token 也失效）→ 停止重连，提示"会话已过期，请重新登录"，跳转登录页。

### 3.3 SettingsView.vue 改动

在现有 Settings 页面的 `<section class="panel settings-card">` 和 `<section class="quick-links">` 之间，新增系统更新卡片：

```vue
<!-- 系统更新卡片 — 仅 admin 可见 -->
<section v-if="auth.isAdmin" class="panel update-card">
  <h3 class="section-title">{{ t('settings.systemUpdate') }}</h3>

  <!-- 版本信息 -->
  <div class="version-row">
    <div class="version-item">
      <span class="version-label">{{ t('settings.currentVersion') }}</span>
      <el-tag v-if="version?.current_short" type="info">{{ version.current_short }}</el-tag>
      <span v-else class="muted">—</span>
    </div>
    <el-icon class="version-arrow"><ArrowRight /></el-icon>
    <div class="version-item">
      <span class="version-label">{{ t('settings.latestVersion') }}</span>
      <el-tag v-if="version?.latest_short" :type="version.behind > 0 ? 'warning' : 'success'">
        {{ version.latest_short }}
      </el-tag>
      <span v-else class="muted">{{ t('settings.versionCheckFailed') }}</span>
    </div>
    <span v-if="version && version.behind > 0" class="behind-badge">
      {{ t('settings.commitsBehind', { n: version.behind }) }}
    </span>
  </div>

  <!-- 操作按钮 -->
  <div class="update-actions">
    <el-button :loading="versionLoading" @click="refreshVersion">
      {{ t('settings.checkUpdate') }}
    </el-button>
    <el-button
      type="primary"
      :disabled="!version?.can_update || updateRunning"
      :loading="updateStarting"
      @click="startUpdate"
    >
      {{ t('settings.startUpdate') }}
    </el-button>
    <span v-if="version && !version.can_update && !versionLoading" class="hint muted">
      {{ t('settings.cannotUpdate') }}
    </span>
  </div>

  <!-- 更新日志区域 -->
  <div v-if="updateSessionId" class="update-log">
    <div class="log-header">
      <span v-if="updateRunning" class="running-indicator">
        <span class="pulse" /> {{ t('settings.updating') }}
      </span>
      <span v-else-if="updateResult === 'success'" class="result-ok">
        <el-icon><CircleCheckFilled /></el-icon> {{ t('settings.updateSuccess') }}
      </span>
      <span v-else-if="updateResult === 'failed'" class="result-fail">
        <el-icon><CircleCloseFilled /></el-icon> {{ t('settings.updateFailed') }}
      </span>
      <span class="log-stats">{{ t('settings.logLines', { n: logLines.length }) }}</span>
    </div>
    <div ref="logContainer" class="log-terminal">
      <pre><code><template v-for="(line, i) in logLines" :key="i">
<span :class="logLineClass(line)" v-text="line" />
</template></code></pre>
    </div>
  </div>

  <!-- 重连提示 -->
  <el-alert
    v-if="updateRunning && streamDisconnected"
    :title="t('settings.reconnecting')"
    type="warning"
    :closable="false"
    show-icon
  />
</section>
```

### 3.4 `<script setup>` 新增逻辑

```typescript
import { ref, reactive, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { ArrowRight, CircleCheckFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import * as systemApi from '../api/system'
import type { VersionInfo, SSEEvent } from '../api/system'

// ---- 版本状态 ----
const version = ref<VersionInfo | null>(null)
const versionLoading = ref(false)

// ---- 更新状态 ----
const updateSessionId = ref<string | null>(null)
const updateRunning = ref(false)
const updateStarting = ref(false)
const updateResult = ref<'success' | 'failed' | null>(null)
const logLines = ref<string[]>([])
const logContainer = ref<HTMLElement>()
const streamDisconnected = ref(false)
let streamClose: (() => void) | undefined
let reconnectTimer: ReturnType<typeof setTimeout> | undefined
let lastSeq = 0                       // 已消费的最大 seq，用于重连去重

// ---- 重连退避参数（指数退避 + 抖动） ----
const RECONNECT_BASE_MS = 500         // 初始 500ms
const RECONNECT_MAX_MS = 15000        // 上限 15s
const RECONNECT_ATTEMPTS = 10         // 最多 10 次
let reconnectAttempts = 0
let authFailed = false                // 仅 401 时置位，重连前才刷新 token

// refreshPromise 单例：并发多次 401 共享同一次 refresh，避免刷新风暴
let refreshPromise: Promise<void> | null = null
function refreshTokenOnce(): Promise<void> {
  if (!refreshPromise) {
    refreshPromise = systemApi.refreshSession().finally(() => { refreshPromise = null })
  }
  return refreshPromise
}

onMounted(async () => {
  if (auth.isAdmin) await refreshVersion()
})

onBeforeUnmount(() => {
  streamClose?.()
  clearTimeout(reconnectTimer)
})

// ---- 版本刷新 ----
async function refreshVersion() {
  versionLoading.value = true
  try {
    version.value = await systemApi.getVersion()
  } catch {
    version.value = null
  } finally {
    versionLoading.value = false
  }
}

// ---- 触发更新 ----
async function startUpdate() {
  updateStarting.value = true
  try {
    const { data, requestId } = await systemApi.triggerUpdate()
    updateSessionId.value = data.session_id
    updateRunning.value = true
    updateResult.value = null
    logLines.value = []
    lastSeq = 0
    reconnectAttempts = 0
    streamDisconnected.value = false
    connectStream(data.session_id)
  } catch (err) {
    ElMessage.error(/* 附带 requestId */)
  } finally {
    updateStarting.value = false
  }
}

// ---- SSE 连接 ----
function connectStream(sessionId: string) {
  streamClose?.()
  const { close } = systemApi.connectUpdateStream(sessionId, {
    onEvent: (event: SSEEvent) => {
      streamDisconnected.value = false
      reconnectAttempts = 0          // 收到任意帧即复位退避计数
      if (event.seq <= lastSeq) return  // 历史回放 + 实时帧去重
      lastSeq = event.seq
      if (event.type === 'line' || event.type === 'step') {
        const text = event.type === 'step'
          ? `\n===== ${t('settings.stepLabel', { step: event.step, total: event.step_total })}：${event.title} =====\n`
          : event.text
        logLines.value.push(text)
        if (logLines.value.length > 2000) logLines.value.splice(0, logLines.value.length - 2000)
        nextTick(() => scrollLogToBottom())
      } else if (event.type === 'error') {
        logLines.value.push(`[ERROR] ${event.message}`)
      } else if (event.type === 'done') {
        updateRunning.value = false
        updateResult.value = event.success ? 'success' : 'failed'
      }
    },
    onAuthError: () => {
      // 401：token 过期。置位标记，重连前先走 refreshPromise 单例刷新 Cookie
      if (updateRunning.value) {
        streamDisconnected.value = true
        authFailed = true
        scheduleReconnect(sessionId)
      }
    },
    onNetworkError: () => {
      // 网络/服务中断（非 401）：不刷新 token，直接按退避重连
      if (updateRunning.value) {
        streamDisconnected.value = true
        scheduleReconnect(sessionId)
      }
    }
  })
  streamClose = close
}

function scheduleReconnect(sessionId: string) {
  if (reconnectAttempts >= RECONNECT_ATTEMPTS) {
    updateRunning.value = false
    updateResult.value = 'failed'
    return
  }
  clearTimeout(reconnectTimer)
  reconnectAttempts++
  const backoff = Math.min(RECONNECT_BASE_MS * 2 ** reconnectAttempts, RECONNECT_MAX_MS)
  const jitter = Math.floor(Math.random() * 250)
  reconnectTimer = setTimeout(() => reconnectStream(sessionId), backoff + jitter)
}

async function reconnectStream(sessionId: string) {
  // 仅 401 才刷新 token（authFailed 由 onAuthError 置位）；网络中断直接重连
  if (authFailed) {
    authFailed = false
    try {
      await refreshTokenOnce()       // 单例：并发 401 共享同一次 refresh
    } catch {
      updateRunning.value = false
      updateResult.value = 'failed'
      streamDisconnected.value = true
      ElMessage.error(t('settings.sessionExpired'))
      return
    }
  }
  connectStream(sessionId)
}

function scrollLogToBottom() {
  const el = logContainer.value
  if (el) el.scrollTop = el.scrollHeight
}

function logLineClass(line: string): string {
  if (line.startsWith('[ERROR]')) return 'log-error'
  if (line.startsWith('[WARN]'))  return 'log-warn'
  return ''
}
```

### 3.5 样式（追加到现有 `<style scoped>` 中，或使用单独的 CSS class）

```css
.update-card { }
.version-row { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.version-item { display: flex; flex-direction: column; gap: 4px; }
.version-label { font-size: 13px; color: var(--el-text-color-secondary); }
.version-arrow { color: var(--el-text-color-secondary); }
.behind-badge {
  font-size: 12px; color: var(--el-color-warning);
  background: var(--el-color-warning-light-9); padding: 2px 8px; border-radius: 4px;
}

.update-actions { margin-top: 12px; display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }

.update-log { margin-top: 16px; }
.log-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; }
.log-stats { font-size: 12px; color: var(--el-text-color-secondary); }
.log-terminal {
  background: #1e1e1e; color: #d4d4d4; border-radius: 6px;
  padding: 12px; max-height: 480px; overflow-y: auto;
  font-family: 'Cascadia Code', 'Fira Code', 'JetBrains Mono', 'Consolas', monospace;
  font-size: 13px; line-height: 1.6; white-space: pre-wrap; word-break: break-all;
}
.log-terminal .log-error { color: #f44747; }
.log-terminal .log-warn  { color: #e5c07b; }

.running-indicator { font-size: 13px; color: var(--el-color-primary); display: flex; align-items: center; gap: 6px; }
.pulse {
  width: 8px; height: 8px; border-radius: 50%; background: var(--el-color-primary);
  animation: pulse 1.5s infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}
.result-ok { font-size: 13px; color: var(--el-color-success); display: flex; align-items: center; gap: 4px; }
.result-fail { font-size: 13px; color: var(--el-color-danger); display: flex; align-items: center; gap: 4px; }
```

### 3.6 i18n 新增 key

#### zh.ts — 在 `settings` 对象末尾追加：

```typescript
// 系统更新
systemUpdate: '系统更新',
currentVersion: '当前版本',
latestVersion: '最新版本',
versionCheckFailed: '获取失败',
commitsBehind: '落后 {n} 个提交',
checkUpdate: '检查更新',
startUpdate: '开始更新',
cannotUpdate: '无法连接远程仓库',
updating: '更新中…',
updateSuccess: '更新成功',
updateFailed: '更新失败',
logLines: '共 {n} 行',
stepLabel: '步骤 {step}/{total}',
reconnecting: '连接中断，等待服务恢复…',
sessionExpired: '会话已过期，请重新登录',
```

#### en.ts — 对应：

```typescript
systemUpdate: 'System Update',
currentVersion: 'Current version',
latestVersion: 'Latest version',
versionCheckFailed: 'Unavailable',
commitsBehind: '{n} commits behind',
checkUpdate: 'Check for updates',
startUpdate: 'Start update',
cannotUpdate: 'Cannot reach remote',
updating: 'Updating…',
updateSuccess: 'Update successful',
updateFailed: 'Update failed',
logLines: '{n} lines',
stepLabel: 'Step {step}/{total}',
reconnecting: 'Connection lost, waiting for server…',
sessionExpired: 'Session expired, please log in again',
```

---

## 4. Go 后端实现要点

### 4.1 model.go

```go
package system

import (
    "errors"
    "time"
)

type VersionInfo struct {
    Current      string `json:"current"`
    CurrentShort string `json:"current_short"`
    Latest       string `json:"latest"`
    LatestShort  string `json:"latest_short"`
    Behind       int    `json:"behind"`
    CanUpdate    bool   `json:"can_update"`
}

type TriggerResponse struct {
    SessionID string `json:"session_id"`
    Current   string `json:"current"`
}

type SSEEvent struct {
    Seq       int    `json:"seq"`
    Timestamp string `json:"ts"`
    Type      string `json:"type"` // "line" | "step" | "done" | "error"
    // line
    Text string `json:"text,omitempty"`
    // step
    Step      int    `json:"step,omitempty"`
    StepTotal int    `json:"step_total,omitempty"`
    Title     string `json:"title,omitempty"`
    // done
    ExitCode int    `json:"exit_code,omitempty"`
    Success  bool   `json:"success,omitempty"`
    OldSHA   string `json:"old_sha,omitempty"`
    NewSHA   string `json:"new_sha,omitempty"`
    // error
    Message string `json:"message,omitempty"`
}

// 错误与配置常量
var (
    ErrUpdateInProgress    = errors.New("已有更新任务正在执行")
    ErrSessionNotFound     = errors.New("session 不存在")
    ErrTooManySubscribers  = errors.New("订阅者过多")
    ErrScriptMissing       = errors.New("更新脚本不存在")
    ErrScriptStartFailed   = errors.New("更新脚本启动失败")
)

const (
    defaultLogDir     = "/tmp"
    defaultMaxSubs    = 4
    defaultSubBuffer  = 512
    defaultTimeout    = 30 * time.Minute
    historyTTL        = time.Hour      // 内存 session 保留时长
    sweepInterval     = 5 * time.Minute // TTL sweep 轮询周期
    logFileMaxLines   = 5000        // 磁盘回放时最多重建行数（防止超长日志拖垮重建）
)
```

### 4.2 service.go

```go
package system

type Service struct {
    repoRoot   string
    scriptPath string
    mu         sync.Mutex
    active     *UpdateSession
    sessions   map[string]*UpdateSession  // 全部 session（含已完成，TTL 后 sweep）
    logDir     string                     // 日志目录，默认 /tmp
    maxSubs    int                        // 单 session 订阅上限，默认 4
    timeout    time.Duration              // 脚本超时，默认 30min
}

func NewService(repoRoot string) *Service

// GetVersion 获取版本信息
func (s *Service) GetVersion() (*VersionInfo, error)

// Trigger 触发更新，返回 sessionID
func (s *Service) Trigger() (string, error)

// Subscribe 订阅指定 session 的日志流，返回 channel 和断开函数
// 内部先回放历史（RingBuffer 或磁盘文件重建），再转发实时事件
func (s *Service) Subscribe(sessionID string) (<-chan SSEEvent, func(), error)

// SessionStatus 获取 session 状态（用于重连时判断是否存活）
func (s *Service) SessionStatus(sessionID string) (*UpdateSession, bool)

// recoverFromDisk 进程重启后根据日志文件 + marker 重建 session
func (s *Service) recoverFromDisk(sessionID string) *UpdateSession
```

**执行脚本的 goroutine 要点（v3：setsid 脱离 + 文件 tail）**：

> 关键改动：脚本以 `setsid` 新会话脱离 Go 父进程，且运行环境独立于 server 容器（docker socket 独立 runner 或宿主命名空间，见 2.4）。因此 Go **不读 stdout/stderr 管道**（进程脱离后管道即断），改为"脚本自己写日志文件 + Go tail 文件"双通道。超时 kill 必须杀整个进程组（`setsid` 后脚本不是 Go 直系子进程的进程组内）。

```go
const defaultTimeout = 30 * time.Minute

// spawnRunner 以 setsid 新会话启动更新脚本（脱离 Go 父进程与 server 容器）。
// 方式 A（推荐，docker socket）：docker run --rm -d --name lab-update-{id}
//      -v /var/run/docker.sock:/var/run/docker.sock
//      -v /opt/hiaf-lab-system:/repo -v /tmp:/tmp
//      镜像 lab-update-runner bash /repo/.hermes/update.sh
//      stdout/stderr 由 runner 自身 tee 到 UPDATE_LOG_FILE。
// 方式 B（宿主命名空间直接执行）：
//      cmd := exec.Command("/bin/bash", s.scriptPath)
//      cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}  // 新会话，脱离进程组
// 无论哪种方式，脚本都不能跑在会被 update.sh 第 6 步 stop 的 server 容器内。
func (s *Service) runScript(session *UpdateSession) {
    // panic 兜底：异常时也保证 session 收尾（finish 内部 sync.Once，幂等）
    defer func() {
        if r := recover(); r != nil {
            slog.Error("runScript panic", "session", session.ID, "err", r)
            s.finish(session, -1, false)
        }
    }()

    pid, err := s.spawnDetached(session)   // setsid / docker run -d，返回进程组 PID 或容器 ID
    if err != nil {
        session.broadcast(SSEEvent{Type: "error", Message: err.Error()})
        s.finish(session, -1, false)
        return
    }
    session.runnerPID = pid

    // 超时看门狗先于 tail 注册：finish 内会 Stop，避免 finish 与赋值竞态
    timer := time.AfterFunc(s.timeout, func() {
        s.killRunner(session)   // 方式 A: docker stop -t 0 <name>；方式 B: syscall.Kill(-pid, SIGKILL)
        session.broadcast(SSEEvent{Type: "error", Message: "更新超时已终止"})
        s.finish(session, -2, false)
    })
    session.timeoutTimer = timer

    // tail goroutine：增量读日志文件 + 轮询 done marker。
    // 新触发 session 文件为空，从偏移 0 开始；seq 由 ingestLine 的 nextSeq 从 0 递增。
    session.tailing = true
    go s.tailSessionLog(session, 0)
}

// tailSessionLog 每 200ms 增量读日志文件，并把新行推入 RingBuffer + 广播；
// 同时轮询 .done marker，出现即收尾。Go 进程重启后由 recoverFromDisk 重建游标继续 tail。
// startOffset：新触发 session 传 0；恢复的 running session 传当前文件末尾
// （历史行已由 recoverFromDisk 重建，tail 只接续新增行，避免重复 ingest）。
func (s *Service) tailSessionLog(session *UpdateSession, startOffset int64) {
    f, err := os.Open(session.logFile)
    if err != nil {
        s.finish(session, -1, false)
        return
    }
    defer f.Close()
    offset := startOffset
    var lastLineAt time.Time

    // 用 time.NewTicker 替代循环内 time.After：避免每轮循环创建新 Timer 造成 GC 压力
    ticker := time.NewTicker(200 * time.Millisecond)
    defer ticker.Stop()

    for {
        buf := make([]byte, 64*1024)
        n, rerr := f.ReadAt(buf, offset)
        if n > 0 {
            offset += int64(n)
            for _, line := range strings.Split(string(buf[:n]), "\n") {
                line = stripANSI(line)
                if line == "" { continue }
                lastLineAt = time.Now()
                session.ingestLine(line)   // RingBuffer.Append + nextSeq + broadcast
            }
        }
        if rerr == io.EOF {
            // 检查 done marker：脚本 EXIT trap 写入
            if _, err := os.Stat(session.doneFile); err == nil {
                s.finishFromMarker(session)
                return
            }
            // 无 marker 但 runner 已消亡（被 kill，未走 EXIT trap）→ 判中断
            if time.Since(lastLineAt) > 30*time.Second && !s.runnerAlive(session) {
                session.broadcast(SSEEvent{Type: "error", Message: "更新进程异常终止"})
                s.finish(session, -1, false)
                return
            }
        }
        select {
        case <-session.done:   // 已被其它路径 finish（如超时 kill），tail 结束
            return
        case <-ticker.C:
        }
    }
}

// finishFromMarker 从 .done marker 解析结果并收尾
func (s *Service) finishFromMarker(session *UpdateSession) {
    data, err := os.ReadFile(session.doneFile)
    if err != nil {
        s.finish(session, -1, false)
        return
    }
    var m struct {
        ExitCode int    `json:"exit_code"`
        OldSHA   string `json:"old_sha"`
        NewSHA   string `json:"new_sha"`
    }
    if json.Unmarshal(data, &m) != nil {
        s.finish(session, -1, false)
        return
    }
    session.mu.Lock()
    session.OldSHA = m.OldSHA
    session.NewSHA = m.NewSHA
    session.mu.Unlock()
    s.finish(session, m.ExitCode, m.ExitCode == 0)
}

// finish 广播 done 事件并标记 session 为 done；sync.Once 保证只执行一次（防双 close）
func (s *Service) finish(session *UpdateSession, exitCode int, success bool) {
    session.once.Do(func() {
        if session.timeoutTimer != nil {
            session.timeoutTimer.Stop()
        }
        session.mu.Lock()
        session.Status = "done"
        session.ExitCode = exitCode
        session.DoneAt = time.Now()
        doneEvent := SSEEvent{
            Seq:       session.seq + 1,
            Timestamp: time.Now().Format(time.RFC3339Nano),
            Type:      "done",
            ExitCode:  exitCode,
            Success:   success,
            OldSHA:    session.OldSHA,
            NewSHA:    session.NewSHA,
        }
        session.doneEvent = doneEvent  // 存副本供重连回放
        session.mu.Unlock()

        session.writeMarker(exitCode)  // /tmp/lab-update-{id}.done（幂等，脚本若已写则覆盖为同值）
        session.broadcast(doneEvent)
        close(session.done)
    })
}
```

> **done 事件投递次序（v3 修复 #2）**：`finish` 的 `broadcast(doneEvent)` 只服务"正在实时消费"的订阅者；对"正在回放 >512 行历史"的订阅者，其 channel 缓冲很可能已满，`broadcast` 的 `select default` 会**丢帧**，前端将永远等不到 done。因此 `Subscribe` 的订阅 goroutine 在**回放完成后**重新检查 `Status`，若已 done 则直接把 `doneEvent` 通过 `select`（阻塞式，直到投递成功或客户端断开）补投，保证不丢（见 4.2 Subscribe）。

**session 广播 — 带缓冲 channel + 非阻塞 send**：

```go
const subBufferSize = 512  // 每个订阅者独立缓冲，缓解慢消费者阻塞

// broadcast 非阻塞向所有订阅者投递事件。
// 订阅者消费慢或已断开时 select default 直接丢帧，绝不阻塞脚本主循环。
// 丢帧由历史回放（RingBuffer/磁盘文件）兜底——重连后 seq 去重补齐。
// 注意：done 事件对"回放中的订阅者"不靠 broadcast 保证（缓冲满会丢），
// 由 Subscribe 在回放结束后补投（见下），因此这里允许 done 被 select default 丢弃。
func (s *UpdateSession) broadcast(evt SSEEvent) {
    s.mu.Lock()
    defer s.mu.Unlock()
    for ch := range s.subs {
        select {
        case ch <- evt:
        default: // 缓冲满 → 丢弃该帧
        }
    }
}

// subscribe 为每个连接创建独立带缓冲 channel
// 上限取自 Service.maxSubs（默认 defaultMaxSubs=4），通过 UpdateSession 注入
func (s *UpdateSession) subscribe() (chan SSEEvent, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.subsCount >= s.maxSubs {
        return nil, ErrTooManySubscribers
    }
    ch := make(chan SSEEvent, subBufferSize)
    s.subs[ch] = struct{}{}
    s.subsCount++
    return ch, nil
}

// unsubscribe 从 map 移除并 close(ch)。
// close(ch) 在 mu 保护内执行，与 broadcast（同持 mu）互斥，不会对已关闭 channel send；
// handler 侧读到 <-ch 的 ok=false 即返回，订阅 goroutine 随之退出，无泄漏。
func (s *UpdateSession) unsubscribe(ch chan SSEEvent) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if _, ok := s.subs[ch]; ok {
        delete(s.subs, ch)
        s.subsCount--
        close(ch) // 通知 handler 退出
    }
}

// ingestLine 由 tailSessionLog 调用：把一行日志写入内存 RingBuffer、分配 seq 并广播。
// v3 起 Go 不再写日志文件（文件由 update.sh tee 写入），只消费文件行。
func (s *UpdateSession) ingestLine(line string) {
    evt := SSEEvent{
        Seq:       s.nextSeq(),
        Timestamp: time.Now().Format(time.RFC3339Nano),
        Type:      "line",
        Text:      line,
    }
    if m := stepRe.FindStringSubmatch(line); m != nil {
        evt.Type = "step"
        evt.Step, _ = strconv.Atoi(m[1])
        evt.StepTotal, _ = strconv.Atoi(m[2])
        evt.Title = m[3]
    }
    s.LogBuffer.Append(line)
    s.broadcast(evt)
}

// writeMarker 写 done marker 文件（幂等）。
// 正常结束由 update.sh 的 EXIT trap 写；超时 kill 时 Go 兜底写一份，标记超时结果。
func (s *UpdateSession) writeMarker(exitCode int) {
    marker, _ := json.Marshal(map[string]any{
        "exit_code": exitCode,
        "old_sha":   s.OldSHA,
        "new_sha":   s.NewSHA,
        "ended_at":  time.Now().Format(time.RFC3339),
    })
    _ = os.WriteFile(s.doneFile, marker, 0o644)
}

// runnerName 返回独立 runner 容器名（方式 A 用）
func runnerName(sessionID string) string { return "lab-update-" + sessionID }

// spawnDetached 以 setsid 脱离方式启动脚本（实现见 runScript 注释）。
// 返回方式 A：docker run -d 的容器名（runnerPID=0）；方式 B：进程组 PID。
func (s *Service) spawnDetached(session *UpdateSession) (int, error) {
    // 方式 A：docker socket 独立 runner
    //   docker run --rm -d --name lab-update-{id} \
    //     -e UPDATE_SESSION_ID={id} -e UPDATE_LOG_FILE={log} -e UPDATE_DONE_FILE={done} \
    //     -v /var/run/docker.sock:/var/run/docker.sock \
    //     -v /opt/hiaf-lab-system:/repo -v /tmp:/tmp \
    //     lab-update-runner bash /repo/.hermes/update.sh
    // 方式 B：宿主命名空间直接执行
    //   cmd := exec.Command("/bin/bash", s.scriptPath)
    //   cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}  // 新会话 + 新进程组
    //   cmd.Env = append(os.Environ(),
    //       "UPDATE_SESSION_ID="+session.ID,
    //       "UPDATE_LOG_FILE="+session.logFile,
    //       "UPDATE_DONE_FILE="+session.doneFile)
    //   cmd.Start(); return cmd.Process.Pid
    return 0, ErrScriptStartFailed // 占位，按部署方式实现
}

// killRunner 中止脚本：方式 A docker stop -t 0；方式 B 杀整个进程组。
// 必须杀进程组（Setsid 后脚本不是 Go 直系子进程）：syscall.Kill(-pgid, syscall.SIGKILL)
func (s *Service) killRunner(session *UpdateSession) {
    if session.runnerPID > 0 {
        _ = syscall.Kill(-session.runnerPID, syscall.SIGKILL)
        return
    }
    _ = exec.Command("docker", "stop", "-t", "0", runnerName(session.ID)).Run()
}
```

> 注：`broadcast` 的 `select default` 只在缓冲写满时丢帧；正常消费速度（SSE 写出）远快于脚本输出（毫秒级 vs 构建秒级），实际几乎不丢帧。历史回放保证最终一致性。

**recoverFromDisk — 文件重建 session（跨进程重启）**：

```go
// recoverFromDisk 进程重启后根据日志文件 + marker 重建 session。
// .done 存在 → 视为已结束，回放历史 + done。
// .done 不存在但 runner 仍存活（setsid 脱离后脚本继续跑）→ Status=running，恢复 tail。
// .done 不存在且 runner 已消亡 → 服务/脚本中断，追加 error 事件。
func (s *Service) recoverFromDisk(sessionID string) *UpdateSession {
    logPath := filepath.Join(s.logDir, "lab-update-"+sessionID+".log")
    donePath := logPath + ".done"
    if _, err := os.Stat(logPath); err != nil {
        return nil // 磁盘也没有 → 404
    }

    sess := &UpdateSession{
        ID:       sessionID,
        subs:     make(map[chan SSEEvent]struct{}),
        done:     make(chan struct{}),
        logFile:  logPath,
        doneFile: donePath,
        mu:       sync.Mutex{},
    }

    if data, err := os.ReadFile(logPath); err == nil {
        lines := strings.Split(string(data), "\n")
        for _, ln := range lines {
            if ln == "" { continue }
            evt := SSEEvent{Seq: len(sess.history) + 1, Type: "line", Text: stripANSI(ln)}
            if m := stepRe.FindStringSubmatch(ln); m != nil {
                evt.Type = "step"
                evt.Step, _ = strconv.Atoi(m[1])
                evt.StepTotal, _ = strconv.Atoi(m[2])
                evt.Title = m[3]
            }
            sess.history = append(sess.history, evt)
        }
    }

    sess.mu.Lock()
    if data, err := os.ReadFile(donePath); err == nil {
        var m struct {
            ExitCode int    `json:"exit_code"`
            OldSHA   string `json:"old_sha"`
            NewSHA   string `json:"new_sha"`
        }
        if json.Unmarshal(data, &m) == nil {
            sess.ExitCode = m.ExitCode
            sess.OldSHA = m.OldSHA
            sess.NewSHA = m.NewSHA
            sess.Status = "done"
            sess.DoneAt = time.Now()
            sess.doneEvent = SSEEvent{
                Seq: len(sess.history) + 1, Type: "done",
                ExitCode: m.ExitCode, Success: m.ExitCode == 0,
                OldSHA: m.OldSHA, NewSHA: m.NewSHA,
            }
            sess.history = append(sess.history, sess.doneEvent)
        }
    } else if s.runnerAlive(sess) {
        // 脚本因 setsid 脱离仍在运行（server 容器重启了，脚本没死）→ 继续 tail
        sess.Status = "running"
    } else {
        // 进程被 kill、脚本中断
        sess.Status = "done"
        sess.DoneAt = time.Now()
        sess.history = append(sess.history, SSEEvent{
            Seq: len(sess.history) + 1, Type: "error",
            Message: "服务重启导致更新中断，请重新触发",
        })
    }
    sess.mu.Unlock()

    // 恢复的 running session：seq 接续历史（下一行 = N+1），tail 从文件末尾续读新行
    if sess.Status == "running" {
        sess.mu.Lock()
        sess.seq = len(sess.history)
        sess.mu.Unlock()
    }

    s.mu.Lock()
    s.sessions[sessionID] = sess
    s.mu.Unlock()

    // runner 仍存活 → 恢复 tail（新进程继续消费同一日志文件直到 done marker）。
    // tailing 标志防并发 Subscribe 同时 recoverFromDisk 导致重复 tail（finish 幂等，重复也无害）。
    sess.mu.Lock()
    shouldTail := sess.Status == "running" && !sess.tailing
    if shouldTail {
        sess.tailing = true
    }
    sess.mu.Unlock()
    if shouldTail {
        var size int64
        if st, err := os.Stat(logPath); err == nil {
            size = st.Size()
        }
        go s.tailSessionLog(sess, size)
    }
    return sess
}

// runnerAlive 判断脚本进程是否仍在运行
func (s *Service) runnerAlive(sess *UpdateSession) bool {
    if sess.runnerPID > 0 { // 方式 B：宿主命名空间 setsid，用信号 0 探测进程组存活
        return syscall.Kill(-sess.runnerPID, 0) == nil || syscall.Kill(sess.runnerPID, 0) == nil
    }
    // 方式 A：docker 独立 runner
    out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", runnerName(sess.ID)).Output()
    return err == nil && strings.TrimSpace(string(out)) == "true"
}

**ANSI 清除**：
```go
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
    return ansiRe.ReplaceAllString(s, "")
}
```

**Subscribe 实现 — 历史回放 + 实时转发 + 连接数限制**：

```go
func (s *Service) Subscribe(sessionID string) (<-chan SSEEvent, func(), error) {
    // 1) 内存命中；否则磁盘重建
    sess, ok := s.SessionStatus(sessionID)
    if !ok {
        sess = s.recoverFromDisk(sessionID)
        if sess == nil {
            return nil, nil, ErrSessionNotFound
        }
    }

    // 2) 注册订阅者（带缓冲 channel），超限返回错误
    ch, err := sess.subscribe()
    if err != nil {
        return nil, nil, err
    }

    // 3) 回放历史：内存 RingBuffer（同进程）或磁盘重建的 history
    //    stop 由客户端断开时触发，保证订阅 goroutine 有明确退出路径
    stop := make(chan struct{})
    go func() {
        defer sess.unsubscribe(ch)
        history := sess.replaySnapshot()   // 见下：Buffer 或 history 快照
        last := 0
        for _, evt := range history {
            last = evt.Seq
            select {
            case ch <- evt:
            case <-stop:      // 客户端断开 → 提前退出，不泄漏
                return
            }
        }
        // 回放完成后再确认状态（修复 #2：done 必须排在回放之后投递）。
        // 若历史 >512 行，回放期间 channel 缓冲可能已满，finish 的 broadcast(doneEvent)
        // 会被 select default 丢帧；这里直接阻塞式补投，保证重连方一定能收到 done。
        sess.mu.Lock()
        doneEvt := sess.doneEvent
        isDone := sess.Status == "done"
        sess.mu.Unlock()
        if isDone {
            if doneEvt.Seq > last {   // 快照若已含 done（seq 相同）则跳过，避免重复
                select {
                case ch <- doneEvt:   // 阻塞投递：缓冲满会等 SSE 消费，直到成功或断开
                case <-stop:
                    return
                }
            }
            return
        }
        // running：继续从 broadcast 收实时帧，直到 stop/done 关闭
        select {
        case <-stop:
        case <-sess.done:
        }
    }()

    // done 是幂等的：handler defer 调用，断开订阅 goroutine + 移除订阅者
    var once sync.Once
    return ch, func() {
        once.Do(func() { close(stop) })
    }, nil
}

// replaySnapshot 返回按 seq 排序的历史事件快照。
// 磁盘重建 session：直接返回 history（含 done/error 事件）。
// 同进程 session：从 RingBuffer 的原始行重建 line/step 事件（seq 按行序推导），
// 若已 done 再追加 doneEvent。
func (s *UpdateSession) replaySnapshot() []SSEEvent {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.history != nil {
        return append([]SSEEvent{}, s.history...)
    }
    var out []SSEEvent
    for i, line := range s.LogBuffer.Snapshot() {
        evt := SSEEvent{Seq: i + 1, Timestamp: "", Type: "line", Text: line}
        if m := stepRe.FindStringSubmatch(line); m != nil {
            evt.Type = "step"
            evt.Step, _ = strconv.Atoi(m[1])
            evt.StepTotal, _ = strconv.Atoi(m[2])
            evt.Title = m[3]
        }
        out = append(out, evt)
    }
    if s.Status == "done" && s.doneEvent.Seq != 0 {
        out = append(out, s.doneEvent)
    }
    return out
}
```

> 说明：`done` channel 在脚本结束时关闭；订阅者断开时 `unsubscribe` 删除 map 项并 `close(ch)`（在 `mu` 保护内，与 broadcast 互斥，不会对已关闭 channel send）让 handler 返回；回放中的订阅者通过 `stop` channel 提前退出。所有 goroutine 都有明确的退出条件，无泄漏。

**NewService 初始化 + TTL sweep（修复 #4）**：

```go
func NewService(repoRoot string) *Service {
    s := &Service{
        repoRoot: repoRoot,
        scriptPath: filepath.Join(repoRoot, ".hermes", "update.sh"),
        sessions: make(map[string]*UpdateSession),
        logDir:   defaultLogDir,
        maxSubs:  defaultMaxSubs,
        timeout:  defaultTimeout,
    }
    // 后台 TTL sweep：只清"已完成且超期"的 session，运行中的 session 永不回收
    go s.sweepSessions()
    return s
}

// sweepSessions 每 sweepInterval（默认 5min）清理超期（historyTTL=1h）的已完成 session。
// 注意：只对 Status=="done" 且 DoneAt 超期的 session 执行 delete；
// running 的 session 由单例锁 + session 生命周期管理，绝不能被 sweep 误删。
// 已删除的 session 若再被订阅，走 recoverFromDisk 从磁盘兜底重建。
func (s *Service) sweepSessions() {
    ticker := time.NewTicker(sweepInterval)
    defer ticker.Stop()
    for range ticker.C {
        now := time.Now()
        s.mu.Lock()
        for id, sess := range s.sessions {
            sess.mu.Lock()
            expired := sess.Status == "done" && now.Sub(sess.DoneAt) > historyTTL
            sess.mu.Unlock()
            if expired {
                slog.Info("sweep expired update session", "session", id)
                delete(s.sessions, id)
            }
        }
        s.mu.Unlock()
    }
}
```

**UpdateSession 补充字段**（供 v3 脱离运行与 tail 使用，追加到 2.3 结构体）：

```go
type UpdateSession struct {
    // ...既有字段...
    runnerPID    int            // 方式 B 的进程组 PID（setsid 后 = 子进程 PID）
    timeoutTimer *time.Timer    // 超时看门狗（finish 时 Stop）
    tailing      bool           // tail goroutine 是否已在跑（recoverFromDisk 防重复启动）
}
```

### 4.3 handler.go

```go
func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
    info, err := h.svc.GetVersion()
    if err != nil {
        common.WriteError(w, r, http.StatusInternalServerError, "version_failed", err.Error(), nil)
        return
    }
    common.WriteSuccess(w, r, info)
}

func (h *Handler) TriggerUpdate(w http.ResponseWriter, r *http.Request) {
    sessionID, err := h.svc.Trigger()
    if err != nil {
        if errors.Is(err, ErrUpdateInProgress) {
            common.WriteError(w, r, http.StatusConflict, "update_in_progress", err.Error(), nil)
            return
        }
        common.WriteError(w, r, http.StatusInternalServerError, "update_trigger_failed", err.Error(), nil)
        return
    }
    common.WriteSuccess(w, r, TriggerResponse{SessionID: sessionID})
}

func (h *Handler) UpdateStream(w http.ResponseWriter, r *http.Request) {
    sessionID := chi.URLParam(r, "sessionId")
    ch, done, err := h.svc.Subscribe(sessionID)
    if err != nil {
        if errors.Is(err, ErrTooManySubscribers) {
            common.WriteError(w, r, http.StatusConflict, "too_many_subscribers", err.Error(), nil)
            return
        }
        common.WriteError(w, r, http.StatusNotFound, "session_not_found", err.Error(), nil)
        return
    }
    defer done()

    flusher, ok := w.(http.Flusher)
    if !ok {
        common.WriteError(w, r, http.StatusInternalServerError, "stream_unsupported", "", nil)
        return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("X-Accel-Buffering", "no")

    // 用 time.NewTicker 替代循环内 time.After：避免每轮循环创建新 Timer
    // 造成 GC 压力与 timer 泄漏；defer Stop 释放。
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    idle := 0
    for {
        select {
        case <-r.Context().Done():
            return
        case evt, ok := <-ch:
            if !ok {
                return  // channel closed = session done / unsubscribe
            }
            idle = 0
            payload, _ := json.Marshal(evt)
            fmt.Fprintf(w, "id: %d\ndata: %s\n\n", evt.Seq, payload)
            flusher.Flush()
        case <-ticker.C:
            idle++
            if idle%150 == 0 {  // ~15s
                w.Write([]byte(": keepalive\n\n"))
                flusher.Flush()
            }
        }
    }
}
```

### 4.4 main.go 集成

```go
import "github.com/zhu571/hiaf-lab-system/go-server/system"

// 创建 system service
repoRoot := commonEnv("REPO_ROOT", "/opt/hiaf-lab-system")
systemSvc := system.NewService(repoRoot)
systemHandler := system.NewHandler(systemSvc)

// 注册路由（见 2.2 节）
```

---

## 5. 实施步骤

### Phase 1 — 后端 API（~4 小时）

1. 创建 `go-server/system/` 模块（model.go → service.go → handler.go）。
2. 在 `main.go` 注册路由。
3. 单元测试：mock `exec.Command` 验证 SSE 流、步骤解析、并发互斥、超时 kill（进程组）、非阻塞广播丢帧、done-after-replay 竞态（回放 >512 行 + 中途 finish）。
4. 集成测试：在 dev 环境手动触发，验证日志流回显。

### Phase 2 — 前端 UI（~3 小时）

1. 创建 `web-ui/src/api/system.ts`（含 `refreshSession` + fetch 型 `connectUpdateStream`）。
2. 在 `SettingsView.vue` 添加更新卡片（模板 + 逻辑 + 样式）。
3. 在 `zh.ts` / `en.ts` 添加 i18n key。
4. 搭建 Vite dev server，Mock 后端 SSE 验证 UI 行为（含断连退避、401→refresh、仅 401 才刷新）。

### Phase 3 — 联调 + 边界测试（~2 小时）

1. 在 dev 环境端到端触发一次完整更新。
2. 验证：SSE 断连重连、409 冲突、权限拦截、审计日志。
3. 验证：更新完成后版本号刷新。
4. 验证：kill 掉 Go 进程（脚本 setsid 脱离继续跑）后前端重连，能从日志文件恢复并收到最终 done；token 过期（401）时重连先 refresh 成功，网络中断时不刷新直接退避重连。
5. 验证：脚本超时（临时调小 `timeout`）触发进程组 kill 并输出 error 事件。
6. 验证：TTL sweep——mock `DoneAt` 超过 `historyTTL` 的 session 被清理，运行中 session 不被误删。

---

## 6. 风险与缓解

| 风险 | 缓解 |
|------|------|
| Go server / server 容器重启，脚本与日志全丢 | 脚本以 `setsid` 在独立 runner 运行，`docker compose stop server` 不影响脚本；日志由脚本 tee 到共享 `/tmp` 文件，Go 只 tail |
| 慢消费者阻塞脚本主循环 | 每订阅者独立 512 缓冲 channel + `broadcast` 的 `select default` 非阻塞丢帧 |
| update.sh 长时间运行（构建镜像）导致 SSE 连接超时 | 每 ~15s keepalive；Nginx/反向代理设长 `proxy_read_timeout` |
| update.sh 挂死（网络/构建卡住） | 超时看门狗 30min 到期 kill 整个进程组（`syscall.Kill(-pgid)` / `docker stop -t 0`）；日志流输出 error 事件 |
| 回放 >512 行时 done 事件被缓冲满丢帧，前端永远停在"更新中" | done 事件改为在 `Subscribe` 回放完成后由订阅 goroutine 阻塞式补投（见 4.2），不再依赖 `broadcast` 的 select default |
| 循环内 `time.After` 造成 timer 泄漏/GC 压力 | 改用 `time.NewTicker` + `defer Stop()`（handler 与 tail 均如此） |
| 订阅者 goroutine 泄漏 | `unsubscribe` 在 `mu` 保护下 `close(ch)`（与 broadcast 互斥），handler 随 `r.Context().Done()` 退出；重建 session 不重复启动脚本 |
| 已完成 session 无限驻留内存 | `sweepSessions` 每 5min 清理 `Status==done` 且超 `historyTTL`(1h) 的 session；运行中 session 永不被误删 |
| 同时打开多个 Settings tab 触发多次更新 | 服务端 mutex + 前端 polling version 只显示一次 |
| 多 tab 同时订阅打爆服务 | 单 session 订阅数上限（默认 4），超限 409 `too_many_subscribers` |
| token 过期导致 SSE 重连 401 死循环 | fetch 型 SSE 识别 401 → `refreshPromise` 单例刷新 Cookie；网络中断（非 401）不刷新直接退避重连；refresh 失败则停止并提示重新登录 |
| 并发 401 触发多次 refresh | `refreshPromise` 单例共享同一次 `POST /auth/refresh`，Promise 串行化 |
| 网络抖动导致频繁重连 | 指数退避（500ms→15s）+ 抖动 + 最多 10 次，收到任意帧即复位 |
| `git ls-remote` 在内网环境超时 | `GIT_TERMINAL_PROMPT=0` + 5 秒 timeout；超时返回 `can_update: false`；update.sh 内 git fetch/pull 也加 `timeout` |
| update.sh 输出因管道块缓冲而滞后/缺尾行 | 脚本 `exec > >(stdbuf -oL tee -a "$UPDATE_LOG_FILE")` 行缓冲落盘，Go 按行读取 |
| 普通用户访问 admin 接口 | `RequireRole(auth.RoleAdmin)` 中间件 + frontend `v-if="auth.isAdmin"` |

---

## 7. 文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `go-server/system/model.go` | 新建 | 数据结构 |
| `go-server/system/service.go` | 新建 | 核心逻辑（setsid spawn、文件 tail、SSE 广播、TTL sweep） |
| `go-server/system/handler.go` | 新建 | HTTP handler |
| `go-server/system/handler_test.go` | 新建 | 测试 |
| `go-server/main.go` | 修改 | 注册路由 + 初始化 service |
| `web-ui/src/api/system.ts` | 新建 | 前端 API 封装（含 refreshSession + fetch 型 SSE，可识别 401） |
| `web-ui/src/views/SettingsView.vue` | 修改 | 新增系统更新卡片 |
| `web-ui/src/i18n/zh.ts` | 修改 | 新增 15 个 key |
| `web-ui/src/i18n/en.ts` | 修改 | 新增 15 个 key |
| `.hermes/update.sh` | 修改 | ① stdout/stderr 行缓冲 tee 到 `$UPDATE_LOG_FILE`；② EXIT trap 写 `.done` marker；③ git fetch/pull 加 `timeout` + `GIT_TERMINAL_PROMPT=0` |
| `deploy/Dockerfile` | 修改 | server 镜像补充 `bash git coreutils`（或仅需 docker CLI） |
| `deploy/docker-compose.yml` | 修改 | server 挂载 `/var/run/docker.sock`、仓库目录、共享 `/tmp` 日志目录 |
| `deploy/update-runner/Dockerfile`（可选） | 新建 | 独立 runner 镜像：`alpine + git + docker-cli + bash + coreutils` |
| `.hermes/frontend-update-plan.md` | 本文件 | 设计方案（v3） |

---

## 8. 后续扩展

- **ntfy 通知打通**：update.sh 已内置 ntfy 通知；前端可加一个"通知已发送"提示。
- **更新历史**：在 PostgreSQL 加 `system_updates` 表，记录每次更新的 old/new SHA、耗时、结果，前端可展示历史。
- **回滚触发**：前端加「回滚」按钮 → 调用 `git checkout $OLD_SHA && ./update.sh --force`。
- **dry-run 预览**：POST body 传 `{"dry_run": true}`，只输出变更检测结果，不实际构建。
