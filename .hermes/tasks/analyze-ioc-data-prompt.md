分析 IOC 和 OPC UA 问题。诊断数据在 .hermes/investigations/ocp-ua-data.md。

排查逻辑：
1. OPC UA 客户端为什么没连上（self._opc is None）→ 检查 IOC 代码中的 OPC UA 初始化流程
2. API 写入返回 200 但硬件没反应 → 检查 EPICS 网关写入 IOC 的链路
3. IOC 日志中没有 OPC UA 相关输出 → 可能有异常被静默吞掉

需要读的代码：
- py-agent/ioc/hiaf_ioc_final.py — IOC 主文件，看 OPC UA 初始化逻辑
- go-server/instruments/service.go — WriteGasCellPV 方法
- go-server/instruments/handler.go — gas control handlers

输出排查结论到 .hermes/investigations/ioc-opcua-analysis.md
