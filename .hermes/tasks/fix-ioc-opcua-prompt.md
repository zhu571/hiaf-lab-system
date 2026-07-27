修复 IOC 的 OPC UA 连接问题。

文件：/home/zhuhaofan/hiaf-lab-system/py-agent/ioc/hiaf_ioc_final.py

## 问题
`_ensure_connected()` 函数已定义（约 391 行）但从未被调用，导致 `self._opc` 永远为 None，OPC UA 连不上，PI 循环跳过阀门写入。

## 修复
在 `_sensor_poll_loop` 的断连分支中，当 `self._opc is None or self._valve_node is None` 时调用 `await self._ensure_connected()`。

参考之前的正确模式（在传感器轮询循环中）：
```python
if self._opc is None or self._valve_node is None:
    await self._ensure_connected()
    if self._opc is None or self._valve_node is None:
        await asyncio.sleep(hiaf_config.SENSOR_POLL_SEC)
        continue
```

另外在 `Piezo_ValveSP.putter` 中，当断连时应该返回错误而不是静默成功，这样 API 不会返回 200 欺骗用户。

验证：
```bash
cd /home/zhuhaofan/hiaf-lab-system && python3 -m py_compile py-agent/ioc/hiaf_ioc_final.py
```
