修复 IOC 中缺失的 `_hyst_cycle` 方法。

## 问题
`_pi_control_loop` 在 HYST 模式下调用 `await self._hyst_cycle(error)`（line 712），但该方法从未定义，导致 AttributeError → PI 循环异常 → 自动停止。

## 修复
在 `_pi_cycle` 方法前面（或后面）添加 `_hyst_cycle` 方法。

参考 `_pi_cycle` 的实现模式（744-769 行），`_hyst_cycle` 应实现一个简单的滞回控制：

```python
async def _hyst_cycle(self, error: float) -> None:
    """Simple hysteresis (bang-bang) control: ramp valve toward target when error is large."""
    try:
        current_v = float(await self._valve_node.read_value())
    except Exception:
        current_v = float(self.Piezo_ValveSP.value)

    hyst_band = getattr(hiaf_config, 'HYST_BAND', 5.0)  # Pa, deadband
    hyst_step = getattr(hiaf_config, 'HYST_STEP', 1.0)  # %, per-cycle step

    if abs(error) <= hyst_band:
        return  # within deadband, do nothing

    # Ramp toward target
    if error > 0:
        new_valve = current_v + hyst_step
    else:
        new_valve = current_v - hyst_step

    new_valve = max(hiaf_config.VALVE_MIN, min(hiaf_config.VALVE_MAX, new_valve))
    await self.Piezo_ValveSP.write(new_valve)
```

文件：/home/zhuhaofan/hiaf-lab-system/py-agent/ioc/hiaf_ioc_final.py

验证：python3 -m py_compile py-agent/ioc/hiaf_ioc_final.py
