# IOC / OPC UA 诊断数据

## 环境
gascell 服务器: 10.144.144.12
SSH 密码: imp123456

## 容器状态
```
lab-server: Up 3 minutes (healthy) — 刚重建
lab-ioc: Up 34 seconds (unhealthy) — 刚重建
```

## IOC 健康检查失败原因
Health check 命令: `python -c "import urllib.request; urllib.request.urlopen('http://localhost:5080/health', timeout=3)"`
IOC 健康 HTTP 服务器在端口 5080 运行。它返回 503 当 `opc_ok = self._opc is not None` 为 False 时（OPC UA 客户端未连接）。
由于 urllib 将 503 视为 HTTPError → 非零退出码 → Docker 标为 unhealthy。

## IOC 日志（无 OPC UA 错误）
IOC 启动正常，caproto（EPICS）工作正常，3 个客户端已连接。
但日志中没有任何 OPC UA 连接/失败相关的输出。

## OPC UA 配置
OPC_URL 环境变量: opc.tcp://10.51.12.158:4862
IOC 的 OPC UA 客户端可能连接失败或未初始化，但日志中没有记录错误。

## API 写入测试
POST /api/v1/instruments/gascell/start → HTTP 200 (5ms)
POST /api/v1/instruments/gascell/stop → HTTP 200 (5ms)
HTTP 层面成功，但硬件未响应。问题可能出在 EPICS 网关→IOC→硬件的链路。

## 需要排查
1. OPC UA 服务器 (10.51.12.158:4862) 是否可达
2. IOC 的 OPC UA 客户端初始化是否有报错（可能被静默吞掉）
3. EPICS 网关写入 IOC 的 PV 后，IOC 是否真的转发到了 OPC UA
4. 为什么 `self._opc` 为 None
