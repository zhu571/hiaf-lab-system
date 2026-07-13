# 仪器控制安全策略

> 版本：v1  
> 适用范围：Keysight E5063A、Hioki IM3536 及后续纳入白名单控制的仪器

## 1. 安全目标

仪器控制默认遵循最小权限、串行占用、参数收窄、可恢复、可追溯原则。系统必须保证：

- 未在白名单中的 SCPI 命令永不执行。
- yellow 命令必须通过参数校验、组合约束、互斥锁、超时控制。
- red 命令默认拒绝，只能人工现场操作。
- Agent 不能绕过租约、人工确认、对象级权限和审计。
- 每次仪器操作都有操作前状态、参数、结果、操作后状态。

## 2. 白名单修复要求

### 2.1 `take_screenshot` 路径白名单

问题：仅限制字符串长度不足以防止路径穿越、覆盖任意文件或写入危险目录。

要求：

- 只允许写入仪器本地截图目录：`D:/screenshots/` 或仪器支持的等价固定目录。
- 文件名只允许 `[A-Za-z0-9._-]`。
- 扩展名只允许 `.png`、`.jpg`、`.bmp`。
- 禁止 `..`、绝对路径切换、反斜杠混淆、控制字符。
- 后端生成文件名优先，用户只能传 `label`，不直接传完整路径。

推荐执行参数：

```yaml
path:
  type: string
  max_len: 128
  allow_prefixes: ["D:/screenshots/"]
  allow_extensions: [".png", ".jpg", ".bmp"]
  deny_patterns: ["..", "\\\\", "\u0000"]
  regex: "^D:/screenshots/[A-Za-z0-9._-]+\\.(png|jpg|bmp)$"
```

### 2.2 `set_sweep_range` 组合约束

单独限制 start/stop 不够，必须校验组合：

- `start_freq < stop_freq`。
- `stop_freq - start_freq <= max_span_hz`。
- `points <= 1601`。
- `if_bandwidth` 不能低到导致超时；默认 `>= 10 Hz`，低 IFBW 需要更长 timeout 或人工确认。
- 估算扫频时间不得超过命令 `timeout_ms`。
- 扫频范围按被测对象类型收窄，例如 RF 匹配默认只允许 1 MHz 到 30 MHz。

### 2.3 `trigger_single` 状态快照与恢复

`trigger_single` 会改变触发源和连续扫描状态，必须：

1. 执行前读取并保存：
   - `TRIG1:SOUR?`
   - `INIT1:CONT?`
   - `SENS1:SWE:POIN?`
   - `SENS1:BWID?`
   - `SENS1:FREQ:STAR?`
   - `SENS1:FREQ:STOP?`
2. 执行命令。
3. 等待完成，超时则中止并进入恢复流程。
4. 读取数据。
5. 按快照恢复触发源和连续扫描状态。
6. 恢复失败时告警并要求人工检查。

### 2.4 DC bias 和 power 参数收窄

不同被测对象允许范围不同：

| 对象类型 | E5063A power | Hioki DC bias | 说明 |
|----------|--------------|---------------|------|
| `rf_matching_network` | -35 到 0 dBm | 禁用 | RF 匹配网络默认低功率 |
| `passive_lc_component` | -45 到 -10 dBm | 0 到 0.5 V | 小信号无源件 |
| `gas_cell_electrode` | -40 到 -20 dBm | 0 到 1.0 V | 防止电极极化或击穿 |
| `unknown` | -45 到 -30 dBm | 禁用 | 未声明对象类型时最保守 |

执行 `set_power`、`set_dc_bias` 时必须带 `object_type`，未带则按 `unknown` 处理。

## 3. 互斥锁与租约

### 3.1 仪器互斥锁

- 每台仪器同一时刻只允许一个写命令运行。
- green 只读命令可并发，但不得与 yellow 写命令同时访问同一 TCP session。
- yellow 命令必须声明 `lock: instrument:<id>:write`。
- 命令超时后锁必须释放，但仪器状态标记为 `needs_check`。

### 3.2 占用租约

执行 yellow 命令前必须持有租约：

```json
{
  "lease_id": "lease_001",
  "instrument_id": "e5063a",
  "holder_user_id": "usr_001",
  "purpose": "RF 匹配扫频",
  "expires_at": "2026-07-14T10:35:00+08:00"
}
```

规则：

- 租约默认 15 分钟，最长 2 小时。
- 续约需说明原因。
- admin 可抢占租约，但必须通知原持有人并写审计。
- Agent 不能抢占租约。

## 4. 紧急停止

### 4.1 触发入口

所有登录成员都可以触发：

```text
POST /api/v1/instruments/{instrument_id}/emergency-stop
```

### 4.2 行为

E5063A：

- 停止当前扫描。
- 关闭连续扫描或保持安全 idle。
- 源功率降到保守值，例如 `-45 dBm`。

Hioki：

- 停止触发或回到空闲测量。
- 关闭 DC bias。
- 电压电平恢复到默认低值。

### 4.3 后续状态

紧急停止后仪器进入 `locked_until_manual_check`，必须由具备 `instrument_operator` 或 admin 权限的人确认恢复。

## 5. 人工确认流程

以下操作必须人工确认：

- red 命令，原则上仍拒绝远程执行。
- 超出默认对象类型范围但仍在物理安全上限内的 yellow 命令。
- 租约抢占。
- 紧急停止后的恢复。
- Agent 发起的任何仪器 yellow 命令。
- 低 IFBW 或大 span 导致预计执行超过 60 秒的扫描。

确认记录：

```json
{
  "approval_id": "apv_001",
  "requested_by": "agent_001",
  "acting_user_id": "usr_001",
  "approved_by": "usr_002",
  "approved_at": "2026-07-14T10:20:00+08:00",
  "command": "set_sweep_range",
  "params_hash": "sha256",
  "expires_at": "2026-07-14T10:25:00+08:00"
}
```

确认 token 只能用于同一命令、同一参数 hash、同一租约，5 分钟过期。

## 6. 操作前后审计

每个仪器 yellow 命令必须记录：

- 操作者、被代表用户、审批人。
- 租约 ID、仪器 ID、对象类型、项目 ID。
- 白名单版本和命令名。
- 原始参数、规范化参数、组合约束校验结果。
- 执行前状态快照。
- 实际发送的 SCPI 模板 ID 和参数 hash。
- 开始时间、结束时间、超时设置。
- 返回值摘要、错误码。
- 执行后状态快照。
- 恢复动作及恢复结果。

审计事件最少两条：

1. `instrument.command.requested`
2. `instrument.command.completed` 或 `instrument.command.failed`

状态恢复失败时追加：

3. `instrument.command.restore_failed`
4. `notification.security_or_safety_alert.sent`

## 7. 实现检查清单

- 白名单 YAML 每次变更都有版本号和审查记录。
- YAML 加载时做 schema 校验，启动失败优于带病运行。
- 后端执行命令时只接受白名单命令名，不接受任意 SCPI 字符串。
- 参数类型、范围、枚举、正则、组合约束全部在服务端校验。
- 前端展示范围只能作为辅助，不能作为安全边界。
- Agent 输出只能生成候选命令，最终仍由仪器服务校验。
- 每台仪器配置独立 TCP 连接池或串行 worker，避免命令交错。
