# B-2：安全中间件（InstrumentWorker）

> 在 B-1 裸 SCPIConnection 之上包装安全层。不含 B-3 命令解析。

## 产出

### 1. `go-server/instruments/model.go`（扩展）
新增结构体：
- `WorkerConfig` — 仪器 ID、地址、terminator、限流参数
- `QueueCommand` — 入队命令（Name、Params、Risk、Priority、ResponseCh）
- `WorkerState` — 运行状态（running / rate_limited / needs_reconnect / error）

### 2. `go-server/instruments/worker.go`（新文件）

**核心结构** `InstrumentWorker`：
```go
type InstrumentWorker struct {
    cfg       WorkerConfig
    conn      *SCPIConnection
    cmdQueue  chan *QueueCommand  // 普通命令队列
    stopCh    chan struct{}
    state     WorkerState
    mu        sync.RWMutex
    // 限流
    lastCmdTimes []time.Time       // 最近 10 次时间戳
    rateLimited  bool
    rateLimitedAt time.Time
}
```

**方法**：
- `NewInstrumentWorker(cfg WorkerConfig) *InstrumentWorker` — 创建
- `Start() error` — 启动 goroutine：连接仪器 → 消费队列
- `Submit(cmd *QueueCommand) error` — 入队（永不超过 10 个元素？还是不限？）
- `Stop()` — 优雅关闭
- `EmergencyStop() error` — 插队头
- `State() WorkerState` — 返回当前状态

**主循环逻辑**（`run()` goroutine）：
```
for {
    select {
    case cmd := <-cmdQueue:
        // 1. 限流检查（yellow 命令）
        if cmd.Risk == "yellow" {
            if rateLimitExceeded() → 标记 rate_limited, 返回错误
        }
        recordCmdTime()
        // 2. 连接检查/重连
        if conn == nil { reconnect() }
        // 3. 执行
        result := conn.Send(buildSCPI(cmd))
        cmd.ResponseCh <- result
    case <-stopCh:
        return
    }
}
```

**限流算法**：
```
rateLimitExceeded():
    now := time.Now()
    清理 lastCmdTimes 中 >10 秒前的
    if len(lastCmdTimes) >= 10:  // 10 秒内发生 ≥10 次
        rateLimited = true
        return true
    lastCmdTimes = append(lastCmdTimes, now)
    return false
```
emergency-stop 不计入限流、不检查限流。

**SCPI 构建**（临时，B-3 会替换）：
- 从 CommandDef 取 Build 或 SCPI 模板
- 简单字符串替换 `{param_name}` → 参数值
- 返回字符串给 conn.Send()

### 3. 文件列表（B-2 之后）

```
go-server/instruments/
├── model.go           # PiezoStatus + SCPIConnection + WorkerConfig + QueueCommand
├── service.go         # EPICS Piezo (不变)
├── handler.go         # Piezo handler (不变)
├── whitelist.go       # B-1
├── whitelist_embedded.yaml  # B-1
├── 仪器白名单.yaml     # B-1 symlink
├── worker.go          # B-2 新
└── service_test.go    # 已有
```

## 不包含
- handler 集成（B-3）
- 租约系统
- 审批流
- 测试

## 验证
`go build ./...` 通过。
