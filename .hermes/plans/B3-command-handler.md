# B-3：命令 Handler + 路由注册

> 在 B-2 InstrumentWorker 之上，将仪器控制暴露为 HTTP API。

## 产出

### 1. `go-server/instruments/handler.go`（扩展）

当前 handler 只有 Piezo 方法。新增：

```go
type Handler struct {
    svc     *Service
    workers map[string]*InstrumentWorker  // B-3 新增: e5063a / hioki_im3536
}

func NewHandler(svc *Service, workers map[string]*InstrumentWorker) *Handler
```

新增 HTTP handler：

**`GET /api/v1/instruments/{id}/status`**
- 从 workers[id] 取 State()
- 返回 `{"instrument_id": "e5063a", "state": "running", "rate_limited": false}`

**`POST /api/v1/instruments/{id}/commands`**
- 请求体：`{"command": "set_sweep_range", "params": {"start_freq": 3e6, "stop_freq": 5e6, ...}}`
- 白名单校验：IsCommandAllowed(id, cmd, risk)
- green 命令直接 Submit
- yellow 命令需租约（暂 stb：先 check role 即可，租约 B-5 后补）
- 返回 CommandResult

**`POST /api/v1/instruments/{id}/emergency-stop`**
- 所有登录用户可调用
- 调 worker.EmergencyStop()

**`GET /api/v1/instruments`**
- 遍历 workers，返回仪器列表 + 各自 State
- `[{"id":"e5063a","name":"Keysight E5063A","state":"running"}, ...]`

**`GET /api/v1/instruments/whitelist`**
- 返回所有仪器 ListCommands 的并集

### 2. `go-server/main.go`（扩展路由）

当前 `/api/v1/instruments` 只有 piezo。新增：

```go
// 仪器控制（B-3）
r.Get("/api/v1/instruments", instrumentsHandler.ListInstruments)
r.Get("/api/v1/instruments/whitelist", instrumentsHandler.GetWhitelist)
r.Get("/api/v1/instruments/{id}/status", instrumentsHandler.InstrumentStatus)
r.Post("/api/v1/instruments/{id}/commands", instrumentsHandler.ExecuteCommand)
r.Post("/api/v1/instruments/{id}/emergency-stop", instrumentsHandler.EmergencyStop)
```

写操作（commands / emergency-stop）加 `RequireRole(maintainer, admin)`，emergency-stop 例外所有人可用 → 不加角色限制但保持 AuthRequired。

### 3. Handler 初始化

main.go 里创建 workers：

```go
e5063aWorker := instruments.NewInstrumentWorker(instruments.WorkerConfig{
    InstrumentID: "e5063a",
    Addr:         "10.51.12.157:5025",   // XXX 待 SSH 隧道
    Terminator:   "\n",
})
hiokiWorker := instruments.NewInstrumentWorker(instruments.WorkerConfig{
    InstrumentID: "hioki_im3536",
    Addr:         "10.51.12.101:3500",
    Terminator:   "\r\n",
})

workers := map[string]*instruments.InstrumentWorker{
    "e5063a":       e5063aWorker,
    "hioki_im3536": hiokiWorker,
}
instrumentsHandler := instruments.NewHandler(instrumentsSvc, workers)

// Start workers
e5063aWorker.Start()
hiokiWorker.Start()
```

### 4. 路由安全

| 端点 | Auth | 角色 |
|------|:--:|------|
| GET /instruments | ✅ | 所有 |
| GET /instruments/whitelist | ✅ | 所有 |
| GET /instruments/{id}/status | ✅ | 所有 |
| POST /instruments/{id}/commands | ✅ | maintainer+ |
| POST /instruments/{id}/emergency-stop | ✅ | **所有**（安全关键） |

## 不包含
- 租约系统（B-5 migrations 之后）
- 审批确认流
- 结构化输入 `/set_sweep_range start=3e6`（B-3 MVP 用 JSON body）
- *OPC? / SYST:ERR? 后置校验（后续补）

## 规则
- worker.Start() 失败不阻塞启动（仪器没开机时容忍）
- 仪器 IP 先硬编码，后续改为 env/config
- go build ./... 验证
- 不做 git commit
