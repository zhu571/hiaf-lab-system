# IOC 通信优化方案

> 链路：asyncua (Python) → OPC UA → Siemens WinCC  
> 前置 I/O：IOC 容器 `lab-ioc` 运行 caproto IOC，通过 `python-opcua/asyncua` 库连接 WinCC OPC UA Server (`opc.tcp://10.51.12.158:4862`)，轮询 27 个传感器节点 + 泵状态节点，并通过 velocity-form PI 控制器写压电阀设定值。  
> 下行链路：caproto CA → EPICS Gateway (`epics-gateway.py:5070`) → Go Server → 前端。  
> 存储：InfluxDB（10s batch）+ SQLite（阈值/30s periodic）。  
> 部署：Docker Compose on Rocky Linux。

---

## 一、可靠性

按性价比（收益 / 改动量）从高到低排列。

### R1. OPC UA 重连指数退避 + 抖动

**现状：** `_ensure_connected()` 检测到断连后立即全量重连，无退避。连续失败时不加延迟，日志刷屏。

**优化：**

- 增加 `_reconnect_backoff` 计时器，每次失败后 `backoff = min(base * 2^n, max_60s) + random(0, jitter_2s)`。
- 重连成功后重置 backoff。
- 避免 "惊群"——多个节点同时断连不会同时重连。

**代价：** ~15 行，改 `_ensure_connected()` 即可。  
**收益：** 消除断连风暴日志，减轻 WinCC 侧压力，提高复杂断连场景下自愈概率。

### R2. IOC 健康端点 + Docker healthcheck

**现状：** IOC 容器无 healthcheck，caproto 进程假死或 OPC UA 死连时 Docker 不知情，无法自动重启。

**优化：**

- 在 IOC 内启动一个极简 HTTP `/health` 端点（stdlib `http.server` 或 aiohttp），返回 200 当且仅当：caproto 事件循环存活 + OPC UA 连接正常。
- docker-compose 加 `healthcheck: test: ["CMD", "python", "-c", "import urllib.request; urllib.request.urlopen('http://localhost:5080/health', timeout=3)"]`。

**代价：** ~30 行，加一个后台协程启动 HTTP server，Dockerfile 暴露新端口。  
**收益：** 进程假死/OPC UA 死连 3×3s=9s 内自动重启，消除运维盲区。

### R3. 写失败本地缓冲 + 断连降级

**现状：** InfluxDB/SQLite 写入失败只打 warning，数据丢弃。OPC UA 断连时 PI 循环直接跳过周期。

**优化：**

- InfluxDB 写入失败时，将 Point 序列化写入内存环形缓冲区（`collections.deque`，上限 600 条 ≈ 100 分钟 @ 10s）。
- 恢复后从 buffer 补写（FIFO，带上原始时间戳）。
- OPC UA 断连时 PI 循环把目标值缓存，恢复后立即追赶（不用从 0 重建积分）。

**代价：** ~40 行，`hiaf_storage.py` 加 deque + `_drain_backlog()` 方法。  
**收益：** 网络抖动/后端短暂不可用时零数据丢失，恢复后数据连续。

### R4. Sensor 读取单点故障隔离

**现状：** 27 个传感器用 `asyncio.gather(*tasks, return_exceptions=True)` 并行读，单个 `read_value()` 超时（2s）可能导致整个 gather 被延迟。更关键的是，某个 node ID 不存在或变更后，`get_node()` 可能抛出异常未在 `_ensure_connected` 中处理。

**优化：**

- 为每个 sensor node 增加独立 error counter，单个 node 连续失败 N 次时打 warn 并跳过该 node 2 个周期（冷却窗口），避免不存在的 tag 反复拖慢全量轮询。
- `_ensure_connected` 中 `get_node()` 加 try/except，对不存在的 node 标记为 `_dead_nodes` 集合并跳过。

**代价：** ~30 行。  
**收益：** WinCC 侧 tag 变更不会导致 IOC 整体性能劣化。

### R5. 监控与告警闭环

**现状：** IOC 进程退出或 OPC UA 断连超过 N 秒无任何外部通知，依赖人工巡检。

**优化：**

- 复用现成 ntfy 通道：`_ensure_connected` 连续失败超过 30s 时，发 ntfy `lab-system` 消息 "OPC UA 断连 >30s"。
- 恢复时发 "OPC UA 已恢复"。
- 传感器轮询失败率 >50% 时发送告警。

**代价：** ~20 行，利用 ntfy 容器（已在同 compose）。  
**收益：** 从被动发现变为主动告警，减少窗口期。

---

## 二、性能

### P1. OPC UA 订阅替代全量轮询（传感器侧）

**现状：** sensor poll loop 以 1Hz 全量读取 27 个 sensor node，无论值变化与否。每轮 27 次 `read_value()` RPC 调用。

**优化：**

- 在 OPC UA 连接建立后，对 sensor nodes 创建 `Subscription`，设置 `publishing_interval=1000ms`、`queue_size=2`。
- 用 `asyncua` 的 `SubHandler` 回调直接更新 `_sensor_values` 缓存和 PV。
- 保留轮询模式作为 fallback（订阅失败时自动降级）。

**代价：** ~50 行，新增 `_setup_subscription()` 方法。  
**收益：** 网络 IO 从 27 RPC/秒 降至仅在值变化时推送，减轻 WinCC 服务器负载和容器网络开销。平均可降 70-90% 带宽占用。

### P2. InfluxDB 异步写入 + 批量提交

**现状：** `write_api=SYNCHRONOUS` 在 `hiaf_storage.py:48`，每次 `maybe_write_influx` 都阻塞 event loop，约 50+ Point 的写入可能耗时 50-200ms。

**优化：**

- 改用 `ASYNCHRONOUS` write mode，批量积累 2-3 个采样周期（20-30s）一次写入。
- 配合 `WritePrecision` 使用实际采集时间而非写入时间。
- 写失败降级到本地 buffer（见 R3）。

**代价：** ~15 行，改 `SYNCHRONOUS` → `ASYNCHRONOUS`，加 `_pending_influx` list。  
**收益：** 消除 sensor poll loop 中的同步阻塞，PI 循环不再被 InfluxDB 延迟拖慢。

### P3. A1 值合并读取 / 避免双重 OPC UA 调用

**现状：** `_sensor_poll_loop` 写入 A1 到 `self._sensor_values["直采数据_A1"]`；`_pi_control_loop` 又单独调用 `_safe_read_a1()` 从 OPC UA 再读一次。在 PI @10Hz 时每 100ms 多一次 `read_value`。

**优化：**

- PI 循环不再直接读 OPC UA 节点，改用共享的 `_a1_from_opc` 缓存，由 sensor poll loop (1Hz) 负责更新。
- A1 是 PI 最关键输入，现 1Hz 更新速率已够用（10Hz 循环内读取同一个 A1 值即可）。
- 代价：PI 循环内的 A1 数据延迟最多 1s（vs 原 0.1s），对真空压力控制（响应在秒级）影响可忽略。

**代价：** ~5 行，删掉 `_safe_read_a1()` 调用，用 `self._a1_from_opc`。  
**收益：** PI 循环减少 10 次/秒 OPC UA RPC，A1 是唯一被双重读取的节点。

### P4. Pump tag 读取复用

**现状：** `_read_pump_tags()` 每次运行时用列表推导 `[k for k in self._pump_nodes if any(s in k ...)]` 做子串匹配来筛选活跃 tag。85 个 pump tags 中只选约 20 个活跃 tag。  
子串匹配每轮重复、可提前计算。

**优化：**

- 在 `_ensure_connected` 构建 pump nodes 时，一次性筛选出 `_active_pump_keys`（已做的事前移到 `__init__` 后的首次连接处，缓存结果）。
- 避免每次 `_read_pump_tags()` 重复做 filter。

**代价：** ~5 行，`_active_pump_keys` 移到类属性。  
**收益：** 消除 85 次 `any(...)` 每轮的无谓开销。

### P5. SQLite 批量写入事务合并

**现状：** `_db.executemany` + `_db.commit()` 在 `maybe_write_sensors` 中被调用，commit 会触发 fsync。

**优化：**

- 阈值触发时仍然写；但 30s 周期 flush 时可以用 `PRAGMA synchronous=NORMAL` 或 `PRAGMA journal_mode=WAL` 降低写延迟。
- 写入频率由阈值驱动（平均远小于 1Hz），实际瓶颈很小，此项标记为"有余力再做"。

**代价：** ~5 行，DB 初始化时执行 PRAGMA。  
**收益：** 小幅降低 SQLite 写入延迟（从 ~10ms 到 ~2ms）。

---

## 三、部署

### D1. Docker 容器资源限制

**现状：** `lab-ioc` 服务在 docker-compose 中未设置 `mem_limit` 和 `cpus`，极端情况下可能 OOM 或被内核杀。

**优化：**

```yaml
ioc:
  deploy:
    resources:
      limits:
        cpus: "1.0"
        memory: "256M"
```

**代价：** 3 行 YAML。  
**收益：** 防止 IOC 进程泄漏拖垮宿主机，OOM 时 Docker 自动重启。

### D2. SQLite 持久化 volume mount

**现状（已知问题）：** SQLite 文件在容器内 `/root` 目录，无 volume mount，重启即丢。（`.hermes/merge-influxdb-grafana-ioc.md` 已记录）  
InfluxDB 是主存储，SQLite 是辅助，但仍有历史查询价值。

**优化：**

```yaml
ioc:
  environment:
    SENSOR_DB_PATH: /data/sensor_history.db
  volumes:
    - /opt/lab-monitor/sqlite:/data
```

`hiaf_config.py` 已支持 `SENSOR_DB_PATH` 环境变量（line 170），无需改代码。

**代价：** 2 行 YAML + host 建目录。  
**收益：** 容器重启后 SQLite 数据保留，消除数据丢失。

### D3. OPC_URL 环境变量化

**现状：** `hiaf_config.py:5` `OPC_URL = "opc.tcp://10.51.12.158:4862"` 硬编码。docker-compose 中虽然传了 `OPC_URL` 环境变量，但 `hiaf_config.py` 未读取。

**优化：**

```python
OPC_URL = os.getenv("OPC_URL", "opc.tcp://10.51.12.158:4862")
```

**代价：** 1 行。  
**收益：** 不改代码即可切换 WinCC 地址（备用服务器、IP 变更等）。

### D4. 结构化日志 + 日志轮转

**现状：** 使用 stdlib `logging` + `basicConfig`，所有日志输出到 stdout，无级别筛选、无轮转。

**优化：**

- 改用 `logging.config.dictConfig` 配置 INFO 到 stdout + WARNING+ 到 stderr。
- Docker logging driver 保留 `json-file` 配合 `max-size=10m max-file=3`。
- 可选：加 Prometheus `/metrics` 端点导出连接状态、读写延迟、错误计数。

**代价：** ~20 行（logging 配置）+ 5 行（可选 metrics）。  
**收益：** 排查问题时可按级别过滤，容器日志不撑爆磁盘。

### D5. IOC 优雅关闭

**现状：** `Piezo_Running.shutdown` 在 `@Piezo_Running.shutdown` 中执行 `_opc.disconnect()` + `_storage.close()`。但 caproto `run()` 的 shutdown 钩子是否被正确触发取决于 Docker 发 SIGTERM 时的事件循环状态。若 `asyncio.create_task(self._sensor_poll_loop())` 未被 cancel，关闭时可能卡住。

**优化：**

- 在 `__init__` 中保存 `self._tasks: list[asyncio.Task] = []`。
- `startup` 中 `task = asyncio.create_task(self._sensor_poll_loop()); self._tasks.append(task)`。
- `shutdown` 中 `for t in self._tasks: t.cancel()`，然后 `await asyncio.gather(*self._tasks, return_exceptions=True)`。

**代价：** ~15 行。  
**收益：** Docker stop 不超时卡死，关闭流程可控。

---

## 实施优先级汇总

| 优先级 | 编号 | 类别 | 代价 | 说明 |
|--------|------|------|------|------|
| 🔴 P0 | R2 | 可靠性 | 小 | IOC healthcheck，消除盲区 |
| 🔴 P0 | D3 | 部署 | 微小 | OPC_URL 环境变量化 |
| 🔴 P0 | D1 | 部署 | 微小 | 容器资源限制 |
| 🟡 P1 | R1 | 可靠性 | 小 | OPC UA 重连退避 |
| 🟡 P1 | P3 | 性能 | 微小 | 消除 A1 双重读取 |
| 🟡 P1 | P2 | 性能 | 小 | InfluxDB 异步写入 |
| 🟡 P1 | D2 | 部署 | 小 | SQLite volume mount |
| 🟢 P2 | R3 | 可靠性 | 中 | 写失败本地缓冲 |
| 🟢 P2 | R4 | 可靠性 | 小 | 单 sensor 故障隔离 |
| 🟢 P2 | P1 | 性能 | 中 | OPC UA 订阅替代轮询 |
| 🟢 P2 | D5 | 部署 | 小 | 优雅关闭 |
| ⚪ P3 | R5 | 可靠性 | 小 | 告警闭环 |
| ⚪ P3 | P4 | 性能 | 微小 | Pump tag 复用 |
| ⚪ P3 | P5 | 性能 | 微小 | SQLite PRAGMA |
| ⚪ P3 | D4 | 部署 | 小 | 结构化日志 |

**建议分两批实施：**

- **第一批（本周）：** P0 + P1 共 7 项，改动量合计 <100 行 + 少量 YAML，可一次性提交。预计消除 90% 已知稳定性风险。
- **第二批（后续）：** P2 + P3 共 8 项，其中 OPC UA 订阅（P1）改动最大（~50 行）但收益也最高，建议独立 PR 加观察期。
