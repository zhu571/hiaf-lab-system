# B-1：白名单加载 + SCPI 驱动

> 仅地基层，不含安全（B-2）和命令解析（B-3）。

## 产出

### 1. symlink: `go-server/instruments/仪器白名单.yaml`
符号链接 → `../../docs/仪器白名单.yaml`

### 2. `go-server/instruments/whitelist.go`
- `go:embed 仪器白名单.yaml` 编译时嵌入
- 启动时 YAML 解析 + schema 校验（字段完整性），失败则 panic
- 导出查询接口：
  - `IsCommandAllowed(instrumentID, commandName string, riskLevel string) bool`
  - `GetCommand(instrumentID, commandName string) (*CommandDef, error)`
  - `ListCommands(instrumentID string) []CommandDef`
- 命令结构体对 yaml: name/description/risk/scpi/build/timeout_ms/params/returns

### 3. `go-server/instruments/model.go`（扩展已有）
新增 SCPI 相关结构体：
- `SCPIConnection` — 仪器 TCP 连接（addr, terminator, timeout, conn net.Conn）
- `CommandDef` — 白名单命令定义
- `CommandResult` — 执行结果

### 4. `go-server/instruments/service.go`（扩展已有 Service）
新增 SCPI 驱动方法：
- `NewSCPIConnection(addr, terminator string) (*SCPIConnection, error)` — TCP 连接
- `(c *SCPIConnection) Send(cmd string) (string, error)` — 逐行发送，`?` 结尾读响应，无 `?` 只发送
- `(c *SCPIConnection) Close() error`
- 分号处理：build 模板按分号拆分行，去掉分号，逐行发送
- terminator 追加：E5063A `\n`，Hioki `\r\n`

## 不包含（留给 B-2/B-3）
- 串行 worker 队列
- 角色/权限校验
- 租约/互斥锁
- 限流
- 结构化命令解析（`/set_sweep_range start=3e6`）
- *OPC? 轮询 / SYST:ERR? 校验

## 验证
`go build ./...` 通过即可。测试留给 B-7。
