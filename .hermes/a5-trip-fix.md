# A5 Overpressure Trip 逻辑修复方案

> 状态: 方案待实施 | 日期: 2026-07-31

## 问题描述

当前 `Safety_A5Max = 10 Pa` 过于敏感，真空腔正常操作时频繁误触发 trip，导致压电阀意外关闭、PI 控制中断。

## 现状分析

**当前逻辑 (hiaf_ioc_final.py 640-677 行):**

```
每个 sensor poll 周期 (1Hz):
  a5_val = 直采数据_A5
  ┌─ if None or NaN  → Safety_A5Trip=2, _running=False  ← 立即 trip
  └─ if > _a5_max    → Safety_A5Trip=1, 关阀, _running=False  ← 立即 trip
```

**关键状态变量:**
| 变量 | 位置 | 当前值 |
|------|------|--------|
| `_a5_max` | :311 | `10.0` (Pa) |
| `_a5_tripped` | :310 | `False` — 一旦 true, 写入 1 即永久锁存直到手动 Clear |

**PV 定义:**
| PV 名称 | 类型 | 含义 |
|---------|------|------|
| `Safety:A5Max` | float, RW | 阈值 (Pa) |
| `Safety:A5Trip` | int, RO | 0=normal, 1=overpressure, 2=sensor fault |
| `Safety:A5TripTime` | str, RO | 触发时间 (ISO) |
| `Safety:A5TripPV` | str, RO | 触发时的 A5 值 |
| `Safety:A5Clear` | int, RW | 写 1 清除保护状态 |

**受影响的相关逻辑:**
- `Piezo_Running` putter (:937-950): trip 状态下禁止启动 PI
- `Piezo_ValveSP` putter (:952-968): trip 状态下拒绝手动写阀
- `Safety_A5Clear` putter (:975-988): 仅当 A5 当前值 ≤ 阈值时才允许清除

## 修复方案

### 改动 1: 阈值从 10 Pa → 50 Pa

**影响行:** :311 (`__init__`), :261-264 (`Safety_A5Max` PV 默认值)

```python
# hiaf_ioc_final.py:311
self._a5_max = 50.0  # was: 10.0

# hiaf_ioc_final.py:262
Safety_A5Max = pvproperty(
    name="Safety:A5Max", value=50.0, dtype=float,  # was: 10.0
    doc="A5 上限阈值 (Pa)",
)
```

### 改动 2: NaN 不触发 trip，只告警

**当前行为:** `a5_val is None or a5_val != a5_val` → 写 `Safety_A5Trip=2` + `_running=False`

**新行为:** 仅记录 WARNING 日志，不改变运行状态。

**影响段:** :640-647 → 替换为:

```python
# 替换 lines 640-647
a5_val = self._sensor_values.get("直采数据_A5")
if a5_val is None or a5_val != a5_val:  # None or NaN
    LOGGER.warning("A5 sensor read returned NaN or None — skipped this cycle")
    # 不 trip，不清 _running，仅告警
```

> **注意:** 需要把下面的 `elif` (:648) 改为 `if`，因为 NaN 分支不再 return/break。

### 改动 3: 连续 3 次超阈值才 trip（防毛刺）

**新增状态变量** (`__init__` :310 附近):

```python
self._a5_tripped = False
self._a5_over_count = 0   # NEW: 连续超阈值计数
self._a5_max = 50.0
```

**覆盖 trip 判断逻辑** (:640-674 段整体改为):

```python
# ── A5 overpressure safety check (debounced: 3 consecutive readings) ──
TRIP_CONSECUTIVE = 3
a5_val = self._sensor_values.get("直采数据_A5")

if a5_val is None or a5_val != a5_val:  # NaN → warn only
    LOGGER.warning("A5 sensor read returned NaN or None — skipped this cycle")
    self._a5_over_count = 0  # reset counter on bad reading
elif a5_val > self._a5_max:
    self._a5_over_count += 1
    LOGGER.warning("A5 above threshold: %.4f Pa (limit=%.1f, count=%d/%d)",
                   a5_val, self._a5_max, self._a5_over_count, TRIP_CONSECUTIVE)
    if self._a5_over_count >= TRIP_CONSECUTIVE and not self._a5_tripped:
        self._a5_tripped = True
        await self.Safety_A5Trip.write(1)
        await self.Safety_A5TripTime.write(datetime.now().isoformat())
        await self.Safety_A5TripPV.write(f"{a5_val:.4f}")
        self._running = False
        await self.Piezo_Running.write(0)
        try:
            async with self._valve_lock:
                if self._opc is not None and self._valve_node is not None:
                    await self._valve_node.write_value(0.0)
                await self.Piezo_ValveSP.write(0.0)
        except Exception as e:
            LOGGER.error("A5 safety: close valve failed: %s", e)
        LOGGER.warning("A5 TRIP: A5=%.4f Pa (limit=%.1f, %d consecutive) — valve closed",
                       a5_val, self._a5_max, self._a5_over_count)
        # Meow notification
        if hiaf_config.MEOW_NAME:
            try:
                msg = f"A5超压 {a5_val:.1f}Pa ({self._a5_over_count}次连续) 阀门已关闭"
                async with aiohttp.ClientSession() as session:
                    await session.get(
                        f"https://api.chuckfang.com/{hiaf_config.MEOW_NAME}/A5超压/{quote(msg)}",
                        timeout=aiohttp.ClientTimeout(total=5),
                    )
            except Exception:
                pass
else:
    # Below threshold — reset counter
    if self._a5_over_count > 0:
        LOGGER.info("A5 back to normal: %.4f Pa — reset over-count", a5_val)
    self._a5_over_count = 0
```

> **注意:** 此处不需要改动 `_valve_lock` 的作用域（原代码 :656 已用），写法不变。

### 联动清理

`Safety_A5Clear` putter (:975-988) 也需要同步重置计数器:

```python
@Safety_A5Clear.putter
async def Safety_A5Clear(self, instance, value):
    if int(value) == 1:
        a5_val = self._sensor_values.get("直采数据_A5")
        if a5_val is None or a5_val != a5_val or a5_val > self._a5_max:
            LOGGER.warning("A5 safety clear refused — A5=%.4f > limit=%.1f",
                           a5_val or 999, self._a5_max)
            return 0
        self._a5_tripped = False
        self._a5_over_count = 0                       # NEW
        await self.Safety_A5Trip.write(0)
        await self.Safety_A5TripTime.write("")
        await self.Safety_A5TripPV.write("0.0")
        LOGGER.warning("A5 safety trip cleared")
    return value
```

## 影响范围总结

| 文件 | 改动行 | 说明 |
|------|--------|------|
| `hiaf_ioc_final.py` :311 | 1 行 | `_a5_max = 10.0` → `50.0` |
| `hiaf_ioc_final.py` :262 | 1 行 | `value=10.0` → `50.0` |
| `hiaf_ioc_final.py` :310 | +1 行 | 新增 `_a5_over_count = 0` |
| `hiaf_ioc_final.py` :640-674 | ~35 行重写 | NaN 告警 + 3 次连续计数 trip |
| `hiaf_ioc_final.py` :983 | +1 行 | clear 时重置 `_a5_over_count` |

## 验证事项

- [ ] 正常压力 (A5 < 50 Pa) 时 `_a5_over_count` 保持 0
- [ ] 单次毛刺 (< 3 次) 触发 WARNING 但不 trip
- [ ] 连续 3 次超阈值 → trip + 关阀 + Meow 通知
- [ ] 超阈值 1~2 次后恢复正常 → counter 归零
- [ ] NaN 出现时仅 WARNING，不影响运行
- [ ] 手动 `Safety:A5Clear=1` 可清除 trip 状态
- [ ] trip 后 `Piezo_Running=1` 被拒绝（已有逻辑不变）

## 风险评估

- **低风险:** 改动仅涉及 `_sensor_poll_loop` 的安全判断分支，不触碰 OPC 读写、PV 定义、其他控制循环。
- **退化回滚:** 将阈值改回 10、删掉计数器、恢复原 NaN 分支即可完全回退当前行为。
- **已知边界:** NaN 不 trip 意味着传感器彻底断线时不会自动停机 — 如果这是不可接受的风险，可在后续追加长时 NaN 看门狗（如 >10 秒持续 NaN 才 trip）。
