在 gascell 服务器上排查气压自动控制失效的原因。

## 现状
- 刚重新部署了最新代码，但自动控制（启动/停止）和手动调阀仍然失效
- lab-server 容器已重启，代码是最新的
- lab-ioc 容器状态 unhealthy（健康检查失败）

## 排查方向

### 1. IOC 问题
在 gascell 上（10.144.144.12, SSH 密码 imp123456, root 密码也是 imp123456）：
- 检查 lab-ioc 的健康检查为何失败（docker inspect lab-ioc）
- 检查 IOC 进程日志（docker logs lab-ioc）
- IOC 运行的是 hiaf_ioc_final.py，看是否有 Python 报错
- 检查 IOC 是否能正常响应 EPICS CA 请求

### 2. OPC UA 问题
- OPC UA 服务器是否在运行
- lab-server 连接 OPC UA 是否正常
- 检查 lab-server 日志中关于 OPC UA 和 gas/write 相关的错误

### 3. 写操作链路
从 lab-server 写 GasCell PV 的完整链路：
lab-server handler → service.WriteGasCellPV → gateway.Put → IOC → hardware
检查每一步是否有错误。

### 4. 验证命令
```bash
# 检查 IOC 健康
docker inspect lab-ioc --format '{{json .State.Health}}'

# IOC 日志
docker logs lab-ioc --tail 50

# lab-server 日志（最近，含 gas/write 相关）
docker logs lab-server --tail 200

# 测试 EPICS 连接（在 IOC 容器内或通过 gateway）
```

## 输出
排查结果写到 .hermes/investigations/ioc-opcua-failure.md
