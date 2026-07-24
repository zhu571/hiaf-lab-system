# 10Hz 改进方案：OPC UA 订阅架构升级

## 背景

目前 `_setup_subscription` 以 1000ms publishing interval + `queuesize=2` 运行，`_SensorSubHandler` 内 50ms 限速丢弃。目标：稳定 10Hz（100ms 推送），无数据丢失。

## 方案

### 1. 订阅回调异步化

`datachange_notification` 是 asyncua 同步回调，当前通过 `run_coroutine_threadsafe` 写 caproto PV。改为将数据推入 `asyncio.Queue`，由消费者协程处理 PV 写入 + 存储，解耦回调线程与业务协程。

```python
class _SensorSubHandler(SubHandler):
    def __init__(self, queue: asyncio.Queue):
        self._queue = queue

    def datachange_notification(self, node, val, data):
        tag = self._nodeid_to_tag.get(node.nodeid, None)
        if tag is None:
            return
        self._queue.put_nowait((tag, float(val)))
```

主循环启动消费者协程：

```python
async def _consume_sub_queue(self):
    while True:
        tag, val = await self._queue.get()
        self._sensor_values[tag] = val
        await self._sensor_pvs[tag].write(val)
```

### 2. 协程背压控制

`asyncio.Queue(maxsize=1000)` 天然提供背压：当消费者慢于 10Hz 生产者时，队列满后 `put_nowait` 抛出 `asyncio.QueueFull`。采用降级策略：

- 队列水位 < 80%：正常处理
- 水位 80-95%：合并同类项（只保留每个 tag 最新值），丢弃旧数据。`_drop_stale` 用 tag→index 哈希表 O(1) 定位，不遍历全队列。
- 水位 > 95%：记录 `data_loss` 计数到 Prometheus/日志，触发 ntfy 告警

```python
QUEUE_HIGH_WATERMARK = 800  # 80%
QUEUE_CRITICAL_WATERMARK = 950  # 95%

async def _consume_sub_queue(self):
    while True:
        tag, val = await self._queue.get()
        qsize = self._queue.qsize()
        if qsize > QUEUE_HIGH_WATERMARK:
            # 合并：只保留 tag 最新值，丢弃旧数据帧
            self._drop_stale(tag)
        if qsize > QUEUE_CRITICAL_WATERMARK:
            self._data_loss_cnt += 1
            await self._maybe_alert_data_loss()
        self._sensor_values[tag] = val
        await self._sensor_pvs[tag].write(val)
```

### 3. queuesize 与数据丢失处理

- 订阅 `queuesize=1000`：asyncua 服务器端队列（OPC UA 协议层），对应 OPC UA 规范中 Subscription 的 maxNotificationsPerPublish。1000 可容纳 10Hz × 100s 的缓冲区。
- 消费者队列 `asyncio.Queue(maxsize=1000)`：应用层背压。
- 数据丢失分级：
  - L1：单 tag 丢失（consumer 落后时合并丢弃旧值）→ 日志记录，无需告警
  - L2：批量丢失（队列满，`put_nowait` 异常）→ 递增 `_data_loss_cnt`，累计 > 10 次/分钟时 ntfy 告警
  - L3：订阅断流（30s 无任何回调）→ 心跳检测，触发 poll fallback，30s 后定期尝试恢复订阅（每60s重试一次，成功则切回订阅模式）。

```python
async def _heartbeat_check(self):
    last_ts = 0
    while True:
        await asyncio.sleep(1)
        now = self._last_callback_ts
        if now > 0 and (time.monotonic() - now) > 30:
            self._logger.warning("订阅断流30s，触发poll fallback")
            self._subscription = None  # 强制切 poll 模式
```

### 4. PI 循环 10Hz 与订阅数据源同步

当前 PI 循环（PID 控制逻辑）读取 `_sensor_values` 做运算，订阅/ poll 都写入同一 dict，天然共享。10Hz 下需要：

- PI 循环由定时器驱动（`asyncio.create_task` 内 `while True: await asyncio.sleep(0.1)`），粒度为 100ms
- 订阅数据到达频率 ≥ 10Hz（OPC UA 发布间隔 100ms），PI 每次循环读到的是最新缓存值
- 若订阅因背压合并丢弃了历史值，对 PI 无影响（PI 只需最新值，不要历史序列）
- 时序数据（InfluxDB 写入）继续保持 10s 批量，不从订阅回调实时写入，避免存储 IO 干扰 10Hz 控制

```python
async def _pi_control_10hz(self):
    while True:
        await asyncio.sleep(0.1)
        pv = self._sensor_values.get("PI_PV", 0)
        sp = self._sensor_values.get("PI_SP", 0)
        output = self._pid_compute(pv, sp, dt=0.1)
        await self._write_ao(output)
```

### 5. 时序图

```
OPC UA Server (10Hz publish)
    │
    ▼
asyncua subscription queue (queuesize=1000)
    │ datachange_notification (sync callback)
    ▼
asyncio.Queue (maxsize=1000) ← 背压边界
    │ consumer 协程异步消费
    ▼
┌─────────────────────────────────────┐
│ _consume_sub_queue                  │
│  ├─ 水位 < 80%: 正常写入            │
│  ├─ 80-95%: 合并同类项不告警        │
│  └─ >95%: data_loss 计数 + 告警     │
│  └─ _sensor_values[tag] = val       │
│  └─ await pv.write(val) (caproto)   │
└─────────────────────────────────────┘
    │
    ├──→ _pi_control_10hz (100ms 周期，读 _sensor_values)
    └──→ _sensor_poll_loop (1Hz，只写存储，不读 OPC UA)
```

## 迁移步骤

1. 重构 `_SensorSubHandler` 为纯入队回调，不再直接操作 PV
2. 新增 `_consume_sub_queue` 消费者协程，处理 PV 写入 + 水位监控
3. `_setup_subscription` 改为 `queuesize=1000`，`publishing=100`
4. 新增 `_heartbeat_check` 协程，30s 无回调自动 fallback
5. PI 循环改为 `asyncio.sleep(0.1)` 固定 10Hz
6. 增加 Prometheus 指标：`sensor_data_loss_total`、`sensor_queue_depth`
7. 先灰度部署观察 24h，对比 influxdb 写入频率与数据完整性
