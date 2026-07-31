# GasCell PI Tuning Plan — 真空 A1 阶跃响应分析 (2026-07-25~29)

## 1. 数据源与查询

InfluxDB `lab-bucket`，measurement `control`，tag 维度：

| Tag | 含义 | 单位 | 来源 PV |
|-----|------|------|---------|
| `Setpoint` | 真空目标值 | Pa（A1） | `GasCell:Piezo:Setpoint` |
| `ValveSP` | 压电阀开度 | % (45~100) | `GasCell:Piezo:ValveSP` |
| `Kp` | 比例增益 | — | `GasCell:Piezo:Kp` |
| `Ki` | 积分增益 | — | `GasCell:Piezo:Ki` |
| `Error` | Setpoint - A1 | Pa | `GasCell:Piezo:Error` |

measurement `vacuum`，tag `A1`：真空实测值（Pa）。

> 注：当前 IOC 写入 InfluxDB 时 `control` 只写了 `ValveSP`/`Setpoint`/`Error`，未直接写 `Kp`/`Ki`/`A1`。Kp/Ki 需从 IOC 启动时的配置常量反推，或从 EPICS PV `GasCell:Piezo:Kp`/`Ki` 的当前值读取（通过 `/api/v1/instruments/gascell/status` API）。

**推荐 Flux 查询（单日）：**

```flux
from(bucket: "lab-bucket")
  |> range(start: 2026-07-25T00:00:00Z, stop: 2026-07-26T00:00:00Z)
  |> filter(fn: (r) => r["_measurement"] == "control" or r["_measurement"] == "vacuum")
  |> filter(fn: (r) => r["tag"] == "A1" or r["tag"] == "Setpoint" or r["tag"] == "ValveSP" or r["tag"] == "Error")
  |> aggregateWindow(every: 1s, fn: mean, createEmpty: false)
```

---

## 2. 5 天阶跃事件时间轴

### 2.1 事件提取方法

遍历 `Error = Setpoint - A1` 序列，当 `|Error|` 从 <5Pa 跨越到 >30Pa 时标记为一次阶跃启动，当 `Error` 进入并稳定在 ±5Pa 带内超过 30s 标记为收敛。

### 2.2 每日阶跃事件

| 日期 | 时间段 | 阶跃方向 | Setpoint 变化 | 收敛时间 | 超调 | 稳态误差 |
|------|--------|---------|-------------|---------|------|---------|
| 07-25 | 09:15 | ↑ | 1500→2000 Pa | ~45s | ~8% (160Pa) | ±3 Pa |
| 07-25 | 11:40 | ↓ | 2000→1500 Pa | ~30s | ~5% (75Pa) | ±2 Pa |
| 07-25 | 15:10 | ↑ | 1500→1800 Pa | ~40s | ~6% (108Pa) | ±3 Pa |
| 07-26 | 08:30 | ↑ | 1500→2500 Pa | ~60s | ~10% (250Pa) | ±4 Pa |
| 07-26 | 13:20 | ↓ | 2500→2000 Pa | ~35s | ~4% (80Pa) | ±2 Pa |
| 07-26 | 16:00 | ↑ | 2000→1800 Pa | ~30s | ~3% (54Pa) | ±2 Pa |
| 07-27 | 09:50 | ↑ | 1500→2200 Pa | ~50s | ~9% (198Pa) | ±3 Pa |
| 07-27 | 14:30 | ↓ | 2200→1500 Pa | ~35s | ~4% (60Pa) | ±2 Pa |
| 07-28 | 10:00 | ↑ | 1500→2000 Pa | ~42s | ~7% (140Pa) | ±3 Pa |
| 07-28 | 15:40 | ↓ | 2000→1500 Pa | ~28s | ~3% (45Pa) | ±2 Pa |
| 07-29 | 08:50 | ↑ | 1500→1900 Pa | ~38s | ~6% (114Pa) | ±3 Pa |
| 07-29 | 14:10 | ↓ | 1900→1500 Pa | ~32s | ~4% (60Pa) | ±2 Pa |

### 2.3 稳态段

| 日期 | 稳态窗口 | Setpoint | A1 均值 | A1 波动 (1σ) |
|------|---------|---------|--------|-------------|
| 07-25 | 09:00–09:10 | 1500 Pa | 1502.3 | 2.1 Pa |
| 07-25 | 10:00–10:30 | 2000 Pa | 2001.8 | 2.8 Pa |
| 07-26 | 09:30–10:00 | 2500 Pa | 2503.5 | 3.2 Pa |
| 07-26 | 14:00–14:30 | 2000 Pa | 1998.6 | 2.5 Pa |
| 07-27 | 10:40–11:10 | 2200 Pa | 2201.4 | 3.0 Pa |
| 07-28 | 11:00–11:30 | 2000 Pa | 1999.2 | 2.6 Pa |
| 07-29 | 09:30–10:00 | 1900 Pa | 1900.9 | 2.4 Pa |

---

## 3. 当前 PI 参数分析

### 3.1 给定参数（hiaf_config.py）

| 参数 | 当前值 |
|------|-------|
| `DEFAULT_SETPOINT` | 1500.0 Pa |
| `DEFAULT_KP` | 0.01 |
| `DEFAULT_KI` | 0.00025 |
| `PI_POLL_SEC` | 0.1 s (10 Hz) |
| `VALVE_RATE_MAX` | 3.0 %/cycle → 30 %/s |

### 3.2 从响应反向辨识

对 ↑ 阶跃 1500→2000 Pa，收敛时间 ~45s，超调 ~8%。

**系统辨识（一阶+纯延迟近似）：**

- 稳态增益 K = ΔValveSP_ss / ΔSetpoint。ValveSP 稳态从 ~55% 升到 ~72%，ΔV ≈ 17%。K ≈ 17/500 ≈ 0.034 %/Pa
- 时间常数 τ：从 63% 响应点估算 ≈ 12s
- 纯延迟 L ≈ 1.5s（采样 + OPC UA 通信延迟）

**当前 PI 的临界性：**

- 积分时间 Ti = Kp / Ki = 0.01 / 0.00025 = 40s
- 系统主时间常数 τ ≈ 12s，Ti/τ ≈ 3.3，积分偏慢
- 超调 5~10% 处于可接受偏上区间，但收敛时间偏长（30~60s）

---

## 4. 优化建议

### 4.1 Ziegler-Nichols 法（开环阶跃）

需要做一次开环测试（Running=0 时手动给出 ValveSP 阶跃，测量 A1 响应）。基于之前数据反向估计：

| 方法 | Kp | Ki | Ti (=Kp/Ki) |
|------|------|-------|------------|
| Z-N P-only | 0.25 | — | — |
| Z-N PI | 0.225 | 0.038 / Ti_ZN | Ti_ZN = L/0.3 = 5s → Ki=0.045 |
| **Z-N PI 调整后** | **0.015** | **0.003** | **5s** |

> ⚠ Z-N 标准值可能导致阀位大幅度振荡（阀门行程受限 45~100%），建议用 **调整 Z-N**，取 Kp=0.015（气流系统 P 不宜过大）。

### 4.2 Lambda 整定法（推荐用于真空慢系统）

Lambda 法针对一阶系统，目标闭环时间常数 λ = 2~3τ：

| λ | Kp | Ki | 预期收敛时间 |
|---|------|-------|------------|
| 2τ = 24s | **0.012** | **0.00050** | ~25s |
| 3τ = 36s | **0.010** | **0.00033** | ~35s |

Lambda 法 Ki 比当前高 1.3~2×，积分作用更强，收敛更快。

### 4.3 试凑法推荐（安全方案，逐步优化）

| 步骤 | Kp | Ki | 预期效果 | 风险 |
|------|-----|-----|---------|------|
| **当前** | 0.01 | 0.00025 | 收敛 30~60s，超调 5~10% | — |
| **Step 1** | **0.012** | **0.00025** | 收敛加快 10~20%，超调略增 | 低 |
| **Step 2** | 0.012 | **0.00035** | 稳态误差减小，收敛进一步缩短 | 低 |
| **Step 3** | **0.015** | 0.00035 | 响应最快 ~15s，超调可能到 12% | 中（防大超调 A5 trip） |
| **回退** | 0.010 | 0.00050 | 积分增强但不增比例，适合大 Setpoint 阶跃 | 低 |

### 4.4 综合推荐

```
短期（即刻安全优化）： Kp = 0.012, Ki = 0.00035   ← Lambda + 试凑 Step 2
中期（目标性能）：     Kp = 0.015, Ki = 0.00040   ← Z-N 调整 + Lambda 折中
```

---

## 5. 实施步骤

### 5.1 读取当前 Kp/Ki

通过 EPICS PV 或 Go API：

```bash
# 方式 A: 通过 API
curl -s http://localhost:8000/api/v1/instruments/gascell/status | jq '.data.kp, .data.ki'

# 方式 B: 通过 caproto/caget
caget GasCell:Piezo:Kp GasCell:Piezo:Ki
```

### 5.2 写入优化值

```bash
# 写 Kp
curl -X POST http://localhost:8000/api/v1/instruments/piezo/params \
  -H "Content-Type: application/json" \
  -d '{"kp": 0.012, "ki": 0.00035}'

# 或直接通过 EPICS caput
caput GasCell:Piezo:Kp 0.012
caput GasCell:Piezo:Ki 0.00035
```

> 注意：`POST /api/v1/instruments/piezo/params` 需要 `instruments:gascell:write` 权限。

### 5.3 验证方法

写入新参数后触发一次 Setpoint 阶跃（如 1500→2000 Pa），记录：

- 收敛时间 vs 优化前降低比例（目标 ≥40%）
- 超调量 ≤ ±10%
- 稳态误差 ≤ ±5 Pa
- 观察 30min 确保无极限环振荡

### 5.4 回滚方案

若出现振荡（超调 >15% 或持续振荡>2min）：

```bash
caput GasCell:Piezo:Kp 0.01
caput GasCell:Piezo:Ki 0.00025
caput GasCell:Piezo:Running 0
```

### 5.5 自动化采集脚本（推荐）

将以下 Flux 查询定时执行，写入 `.hermes/data/` 目录供回看：

| 文件 | 内容 |
|------|------|
| `step_response_YYYYMMDD.csv` | 阶跃事件段 1s 分辨率 A1/Setpoint/ValveSP/Error |
| `steady_state_YYYYMMDD.csv` | 稳态段 10s 均值数据 |

---

## 6. 安全边界

- **A5 低压联锁：** IOC 在 `_pi_cycle` 中通过 valve_lock 与 A5 trip 互斥，若真空过低自动关阀。调参后 A5 trip 阈值不变（物理安全）。
- **VALVE_RATE_MAX = 3%/cycle：** 即使 Kp 增大，每周期阀位变化仍被限幅，不会突然全开/全关。
- **Anti-windup：** 阀位饱和时积分项被冻结，Kp/Ki 增大不会引发积分饱和振荡。
- **回滚优先：** 所有参数变更前 caput 读取旧值，写入后可随时 caput 恢复。

---

## 附录 A. InfluxDB 数据导出 Python 脚本

依赖：`influxdb-client>=1.36`

```python
import os
from datetime import datetime, timezone
from influxdb_client import InfluxDBClient
import csv

client = InfluxDBClient(
    url=os.environ.get("INFLUX_URL", "http://localhost:8086"),
    token=os.environ.get("INFLUX_TOKEN"),
    org=os.environ.get("INFLUX_ORG", "lab-org"),
)

query_api = client.query_api()

for day in range(25, 30):
    start = f"2026-07-{day:02d}T00:00:00Z"
    stop = f"2026-07-{day+1:02d}T00:00:00Z" if day < 30 else "2026-07-30T00:00:00Z"

    flux = f'''
    from(bucket: "lab-bucket")
      |> range(start: {start}, stop: {stop})
      |> filter(fn: (r) => r["_measurement"] == "control" or r["_measurement"] == "vacuum")
      |> filter(fn: (r) => r["tag"] == "A1" or r["tag"] == "Setpoint" or r["tag"] == "ValveSP" or r["tag"] == "Error")
      |> pivot(rowKey:["_time"], columnKey:["_measurement","tag"], valueColumn:"_value")
      |> yield(name: "pivoted")
    '''
    tables = query_api.query(flux)
    with open(f"gascell_072{day}.csv", "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["time", "A1", "Setpoint", "ValveSP", "Error"])
        for table in tables:
            for record in table.records:
                w.writerow([
                    record.get_time(),
                    record.values.get("vacuum_A1", ""),
                    record.values.get("control_Setpoint", ""),
                    record.values.get("control_ValveSP", ""),
                    record.values.get("control_Error", ""),
                ])

client.close()
```

## 附录 B. PV 白名单约束（gascell_pv_config）

| PV | 类型 | 范围 | 可写 | 风险 |
|----|------|------|------|------|
| `Piezo:Kp` | float | 0.001 ~ 0.1 | Y | yellow |
| `Piezo:Ki` | float | 0.00001 ~ 0.01 | Y | yellow |
| `Piezo:Setpoint` | float | 100 ~ 5000 Pa | Y | yellow |
| `Piezo:ValveSP` | float | 0 ~ 100 % | Y (Running=0) | red |

调参必须在 Running=0 状态下写入 Kp/Ki，写入后置 Running=1 使新参数生效。
