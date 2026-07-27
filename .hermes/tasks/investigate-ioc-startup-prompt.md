排查 IOC 启动钩子未执行的问题。

## 症状
IOC 容器运行正常，caproto 启动成功（端口 5064），OPC UA 连接成功。
但 IOC 自定义的启动日志（"HiafGasCellIOC starting up..."、"Sensor poll loop started"、"PI control loop started"）完全没有出现。
这说明 `@Piezo_Running.startup` 装饰的启动函数没有被 caproto 调用。

## 需要检查
1. `@Piezo_Running.startup` 在 caproto 中的触发条件是什么？为什么 PV 已创建但 startup 不执行？
2. 查看 hiaf_ioc_final.py 中 Piezo_Running 的 pvproperty 定义（约 241 行）和 startup 函数（约 908 行）
3. 看是否有异常在 IOC 初始化过程中被静默吞掉
4. 检查 `_sensor_poll_loop` 和 `_pi_control_loop` 是否因为其他原因没有启动

## 参考文件
py-agent/ioc/hiaf_ioc_final.py

## 输出排查结果
写到 .hermes/investigations/ioc-startup-failure.md
