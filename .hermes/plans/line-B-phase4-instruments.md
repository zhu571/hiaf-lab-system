# 并行线 B：Phase 4 仪器控制模块

> Go 后端 + Vue 前端，独立模块。SCPI 白名单安全策略，对话式交互。
> 
> **裁决日期**：2026-07-19，Kimi Code v0.27.0 审查通过。8 项设计矛盾已裁决，文档已对齐 `instrument-security.md` / `api-contract.md` / `仪器白名单.yaml`。

## 目标

实现实验室核心仪器的程序化控制和数据采集：
1. **E5063A 矢量网络分析仪** — 扫 S 参数（S11/S21）、测量带宽
2. **Hioki IM3536 LCR 表** — 阻抗/电容/电感测量
3. **压电控制器** — GasCell 压电阀控制（后端已有，待加固）

## 仪器信息

| 仪器 | 地址 | 协议 | 白名单文件 |
|------|------|------|------|
| E5063A | 10.51.12.157:5025 | SCPI over TCP | `仪器白名单.yaml` |
| Hioki | 10.51.12.101:3500 | SCPI over TCP | `仪器白名单.yaml` |
| GasCell Piezo | EPICS Gateway localhost:5070 | HTTP PV | 待纳入白名单框架 |

## 安全机制

### SCPI 白名单三级

**以 `仪器白名单.yaml` 为唯一权威来源。** 红/黄/绿分级和命令名均以 yaml 为准。

| 级别 | E5063A 示例 | Hioki 示例 | 说明 |
|------|------|------|------|
| 🟢 安全读 | `*IDN?` `SENS1:FREQ:DATA?` `CALC1:MARK1:Y?` | `*IDN?` `MEASure?` `FREQuency?` | 所有角色可执行 |
| 🟡 安全写 | `set_sweep_range` `set_power` `trigger_single` | `set_frequency` `set_voltage_level` `set_dc_bias` | 需租约 + 人工确认（Agent 发起时） |
| 🔴 拒绝 | `*RST` `SYST:PRES` 校准类 | `*RST` 补偿类（OPEN/SHORT/LOAD） | **永不远程执行，只能人工现场操作** |

### 角色映射矩阵

| 角色 | 🟢 安全读 | 🟡 安全写 | 🔴 拒绝 |
|------|:--:|:--:|:--:|
| viewer | ✅ | ❌ | ❌ |
| member | ✅ | ❌ | ❌ |
| maintainer | ✅ | ✅（需租约） | ❌ |
| admin | ✅ | ✅（需租约） | ❌ |
| agent | ✅ | ✅（需人工确认） | ❌ |

> 代码角色体系（`auth/model.go`）：admin / maintainer / member / viewer / agent。无 owner 角色，无 instrument_operator 角色。maintainer 即涵盖仪器操作权限。

### 防护规则

- 每台仪器独立互斥锁（VNA 和 LCR 互不干扰）
- 每次命令前检查白名单 + 角色权限
- **后端强制限流**（非前端防抖）：单仪器 >1次/秒持续 >10 秒自动阻止
- 所有命令写入 `command_log` 表（对齐 `instrument-security.md` §6）
- 写命令后置 `SYST:ERR?` 校验

## 后端 API 端点

**统一到 `api-contract.md` 形状（`/instruments/{id}/commands`）：**

```
POST   /api/v1/instruments/{id}/commands          # 发送白名单命令（结构化输入）
GET    /api/v1/instruments/{id}/status            # 连接状态
POST   /api/v1/instruments/{id}/leases            # 申请租约
DELETE /api/v1/instruments/{id}/leases/{lease_id} # 释放租约
POST   /api/v1/instruments/{id}/emergency-stop    # 紧急停止（所有登录成员可用）
GET    /api/v1/instruments/whitelist              # 查看白名单（只读，go:embed 版本）
GET    /api/v1/instruments/command-log            # 命令历史
```

## 项目结构

```
go-server/instruments/
├── model.go           # 仪器连接模型、SCPI 命令/响应、租约/Approval 模型
├── whitelist.go       # go:embed 加载仪器白名单.yaml → 启动时 schema 校验
├── repository.go      # DB 操作（command_log、leases、approvals）
├── service.go         # 核心逻辑：命令白名单校验 + 串行 worker 队列 + 租约管理
├── handler.go         # HTTP handler
├── handler_test.go
└── service_test.go
```

## NL→SCPI 解析策略

分两期：

**MVP（B-3）**：结构化输入。前端输入框支持 `/命令名 参数=值` 格式（如 `/set_sweep_range start=3e6 stop=5e6 object_type=rf_matching_network`）。后端零 LLM 依赖。

**二期（py-agent）**：自然语言解析。用户输入"扫 Carpet S11 3-5M" → py-agent（LightAgent + DeepSeek V4）产出候选命令 → 仪器服务校验 → yellow 命令走 confirm_token 人工确认。

> 前端输入框不支持裸 SCPI 字符串直输。所有输入必须映射到白名单命令名 + 结构化参数。

## 前端：仪器对话页

设计见 `phase4-device-pages.md` 第 1 节。关键约束：
- 每仪器独立锁状态显示
- 紧急停止按钮全局可见
- 租约申请/状态 UI
- Agent yellow 命令的人工确认界面
- 内联出图（log-mag 转换在后端做）

## 实施 Plan

| Step | 产出 |
|------|------|
| B-1 | 写 `go-server/instruments/` 白名单加载 + SCPI 驱动（TCP socket 逐行发送，分号拆分） |
| B-2 | 写安全中间件（角色校验 + 后端限流 + 每仪器串行 worker 队列） |
| B-3 | 写 command handler（结构化输入 → 白名单命令执行；`*OPC?` 轮询 / `SYST:ERR?` 校验） |
| B-4 | Piezo 最小加固（setpoint 物理范围 + RequireRole(maintainer, admin)） |
| B-5 | migration：`command_log` + `leases` + `approvals` 表 |
| B-6 | 前后端联调：对话页 + 紧急停止 + 租约 UI |
| B-7 | 补测试 + 审计事件 |

## 压电控制（后端已有，需加固）

`go-server/instruments/` 已有 piezo 相关 handler/service：
- `GET /api/v1/instruments/piezo/status`
- `POST /api/v1/instruments/piezo/start`
- `POST /api/v1/instruments/piezo/stop`
- `POST /api/v1/instruments/piezo/setpoint`

**加固任务（B-4）**：
- setpoint 加物理范围常量校验
- start/stop/setpoint 加 `RequireRole(maintainer, admin)`
- 是否纳入完整白名单/租约体系后续再议

## 设计文档参考

- `docs/instrument-security.md` — 安全策略 + 白名单规则（权威）
- `docs/仪器白名单.yaml` — 所有命令及参数范围（权威）
- `docs/api-contract.md` — API 约定（权威）
- `docs/permission-audit.md` — 权限审计
- `phase4-device-pages.md` — 前端页面设计

## Kimi Code 审查记录

2026-07-19，Kimi Code v0.27.0 审查 4 份方案文档 + Piezo 实现代码。发现 8 项 🔴 矛盾、16 项 🟡 缺失、8 项 🔵 建议。全部裁决已整合入本文档。完整审查输出见 `kimi -r session_a2946cec-faab-4049-947a-5f6069c843b7`。
