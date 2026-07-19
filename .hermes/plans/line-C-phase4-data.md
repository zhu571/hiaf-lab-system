# 并行线 C：Phase 4 实验数据模块

> 4 个独立 Go 模块 + 前端页面 + 1 个通用附件模块。互不依赖，可并行开发。
>
> **讨论日期**：2026-07-19，逐项决议见各模块「决议」节。
> **SQLite schema 审核**：2026-07-19，旧 schema 路径 `~/work/agent work/lab-daily-report/schema_gas_cell.sql` + `schema_lab_env.sql`。

---

## SQLite → PostgreSQL 迁移概要

| SQLite 表 | 目标 PG 表 | 关键差异 |
|-----------|-----------|---------|
| `test_data` | C-1 `test_data` | 字段名映射（`measured_value`→`value`）；`data_type` 枚举对齐（`cryogenic`→`cryo`，新增 `rf_voltage`/`voltage`）；新增 `quality`、`source`；`report_date`→`project_id`+`run_id` |
| `rf_matching_circuits` | C-4 `rf_matching_records` | 新增 `s11`、`status`；保留全部 17 个旧字段；`report_date`→`project_id` |
| `assembly_process` | C-3 `assembly_steps` | 新增 `paused`/`skipped`/`cancelled` 状态 + `depends_on` + `assigned_to`；`report_date`→`project_id` |
| `cryo_runs` | 合并入 C-2 `experiment_runs` | `run_type`/`target_temp`/`min_temp`/`he_pressure` 并入；`start_date`/`end_date`→`started_at`/`ended_at` |
| `images` (双库) | 通用 `attachments` + `attachment_links` | 拆文件元数据+绑定关系；`file_path`→`storage_key`；支持默认绑定日报 |

> **迁移铁律**（AGENTS.md）：保留源表、源 ID、源 hash、迁移批次号。逐字段 mapping 表在实施阶段补全。

---

## C-1：测试数据 `test_data`

### 目标
记录和管理实验测量数据：低温温度、气压、RF 电压、功率、效率等。

### 决议
对齐 SQLite `test_data` 表结构，字段名统一为 PG 风格，新增 `quality`/`source`/`measured_at`，枚举值对齐旧库。

### 与 SQLite 对比

| SQLite | PG | 说明 |
|--------|-----|------|
| `id INTEGER` | `id UUID` | 全局唯一 |
| `report_date DATE` | `project_id` + `run_id` | 迁移：从日期关联→项目+批次关联 |
| `data_type` (`cryogenic`,`pressure`,`voltage`,`rf_voltage`,`efficiency`) | `data_type` (`cryo`,`pressure`,`voltage`,`rf_voltage`,`efficiency`) | `cryogenic` 简化为 `cryo`，其他不变 |
| `measurement_name` | `measurement` | 字段名简化 |
| `measured_value REAL` | `value DOUBLE PRECISION` | — |
| `unit` | `unit` | — |
| `description` | `notes` | 字段名改为通用 |
| ❌ | `quality` (`normal`,`outlier`,`suspect`) | **新增**，替代旧库无质量标记 |
| ❌ | `source` (`manual`,`auto`,`agent`) | **新增** |
| ❌ | `measured_at TIMESTAMPTZ` | **新增**，区别于入库时间 `created_at` |
| `created_at TEXT` | `created_at TIMESTAMPTZ` | — |
| `updated_at TEXT` | `updated_at TIMESTAMPTZ` | — |
| ❌ | `recorded_by UUID` | **新增**，旧库无用户追踪 |

### 数据模型
```sql
CREATE TABLE test_data (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL,
    run_id UUID,                                  -- 所属实验批次，可选
    data_type VARCHAR(32) NOT NULL                -- cryo/pressure/voltage/rf_voltage/efficiency
        CHECK (data_type IN ('cryo','pressure','voltage','rf_voltage','efficiency')),
    measurement VARCHAR(128) NOT NULL,            -- 测量项名（如 "T1 温度"）
    value DOUBLE PRECISION NOT NULL,
    unit VARCHAR(16) DEFAULT '',                  -- K/mbar/W/dB
    quality VARCHAR(16) NOT NULL DEFAULT 'normal' -- normal/outlier/suspect
        CHECK (quality IN ('normal','outlier','suspect')),
    source VARCHAR(16) NOT NULL DEFAULT 'manual'  -- manual/auto/agent
        CHECK (source IN ('manual','auto','agent')),
    measured_at TIMESTAMPTZ,                      -- 实际测量时间（补录/导入时关键）
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    recorded_by UUID
);
CREATE INDEX idx_test_data_project ON test_data(project_id, created_at DESC);
CREATE INDEX idx_test_data_run ON test_data(run_id) WHERE run_id IS NOT NULL;
CREATE INDEX idx_test_data_type ON test_data(data_type);
```

### 后端 API
```
POST   /api/v1/projects/{id}/test-data     # 录入数据
GET    /api/v1/projects/{id}/test-data     # 查询（按类型/时间/批次筛选）
DELETE /api/v1/test-data/{id}              # 改为 quality=invalid，不硬删除
```

### 实现契约
- **Idempotency-Key**：`POST /api/v1/projects/{id}/test-data` 必须带 `Idempotency-Key` header，重复提交返回 200 + 已有结果
- **审计**：所有写操作写 `audit_log`（action=`test_data.create`/`test_data.delete`，object=`test_data:{id}`，含 before/after）
- **架构**：`handler.go` → `service.go`（权限校验 + 业务逻辑）→ `repository.go`（只访问 `test_data` 表，不跨模块）
- **删除**：不硬删除，改为 `quality=invalid`（实验原始数据不可移除）
- **权限**：项目 member 以上可创建；owner + 记录创建者可标记 invalid；列表按项目 ACL 过滤

### 前端页面
见 `phase4-device-pages.md` 第 2 节 — 数据记录表单 + 趋势图。

---

## C-2：实验批次 `experiment_runs`

### 目标
为每次实验测试创建一个独立容器，关联该批次的所有日志、测试数据、Issue。合并 SQLite `cryo_runs` 表的低温专用字段。

### 决议

| 项 | 决议 |
|----|------|
| 粒度 | 每个测试目标 = 一个独立 run，不嵌套 |
| 时间周期关联 | `campaign varchar` 可选标签（如 `"2026-07-cooling-3"`），不强制 |
| 状态流转 | `planned → active → paused → active → completed / aborted` |
| gas_type | `varchar`，默认 `'He'`，不换气体 |
| 压力范围 | `pressure_min` / `pressure_max` / `pressure_unit`（默认 mbar），全部可选 |
| has_beam | `boolean`，默认 false |
| devices | `text[]`，默认 `'{}'`，多选：rf_carpet / rfq / qpig |
| 日报关联 | **多对多**：中间表 `daily_report_run_links (report_id, run_id)` |
| cryo_runs 合并 | `run_type`/`target_temp`/`min_temp`/`he_pressure` 从 `cryo_runs` 并入 |

### 与 SQLite cryo_runs 对比

| SQLite `cryo_runs` | PG `experiment_runs` | 说明 |
|---------------------|---------------------|------|
| `run_type` (`cooldown`,`warmup`,`steady_state`,`test`) | `run_type` | 直接迁移 |
| `target_temp` | `target_temp` | 目标温度 (K) |
| `min_temp` | `min_temp` | 达到的最低温度 (K) |
| `he_pressure` | — 用 `pressure_min`/`pressure_max` 替代 | 细化 |
| `start_date`/`end_date` | `started_at`/`ended_at` | TEXT→TIMESTAMPTZ |

### 数据模型
```sql
CREATE TABLE experiment_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL,
    name VARCHAR(256) NOT NULL,                -- "2026-07差分性能测试"
    campaign VARCHAR(128),                     -- 可选，同一时间周期的标签
    run_type VARCHAR(16) DEFAULT 'test'        -- cooldown/warmup/steady_state/test
        CHECK (run_type IN ('cooldown','warmup','steady_state','test')),
    status VARCHAR(16) NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned','active','paused','completed','aborted')),
    gas_type VARCHAR(16) NOT NULL DEFAULT 'He',
    target_temp DOUBLE PRECISION,              -- K，目标温度
    min_temp DOUBLE PRECISION,                 -- K，实际最低温度
    pressure_min DOUBLE PRECISION,
    pressure_max DOUBLE PRECISION,
    pressure_unit VARCHAR(8) NOT NULL DEFAULT 'mbar',
    has_beam BOOLEAN NOT NULL DEFAULT false,
    devices TEXT[] NOT NULL DEFAULT '{}',
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID,
    CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE TABLE daily_report_run_links (
    report_id UUID NOT NULL,
    run_id UUID NOT NULL,
    PRIMARY KEY (report_id, run_id)
);
CREATE INDEX idx_runs_project ON experiment_runs(project_id, status, created_at DESC);
```

### 后端 API
```
POST   /api/v1/projects/{id}/runs           # 创建批次
GET    /api/v1/projects/{id}/runs           # 列表（按 campaign/status/devices 筛选）
PATCH  /api/v1/experiment-runs/{id}         # 修改状态/时间/元数据
GET    /api/v1/experiment-runs/{id}         # 详情（含关联日志/数据摘要）
```

### 状态转移矩阵

```
planned → active    (开始实验，设 started_at=now())
planned → aborted   (计划直接取消)
active  → paused    (中断，不设 ended_at)
active  → completed (正常完成，设 ended_at=now())
active  → aborted   (放弃，设 ended_at=now())
paused  → active    (恢复)
paused  → aborted   (暂停中放弃，设 ended_at=now())
```

### 实现契约
- **Idempotency-Key**：`POST/PATCH` 必须带 `Idempotency-Key` header
- **审计**：所有写操作审计（action=`experiment_run.create`/`experiment_run.update`/`experiment_run_link.create`，含 before/after）
- **架构**：`handler.go` → `service.go`（状态转移校验 + 权限 + depends_on 逻辑）→ `repository.go`（只访问 `experiment_runs` 表，不跨模块）
- **聚合**：`GET /experiment-runs/{id}` 详情由 handler 分别调 logs/issues/test-data HTTP API 拼结果，不在 repository 做跨表 JOIN。部分下游失败返回 `partial: true`
- **删除**：软删除（`deleted_at`），默认查询过滤
- **权限**：项目 member 以上可创建；maintainer + 创建者可修改；列表按项目 ACL 过滤

### 前端页面
见 `phase4-device-pages.md` 第 5 节 — 卡片列表 + 时间线。

## C-3：装配流程 `assembly_steps`

### 目标
跟踪气体靶装配步骤，支持调试和正式组装两种模式。

### 决议

| 项 | 决议 |
|----|------|
| 状态流转 | `planned → in_progress → paused → skipped → cancelled`，详见流转图 |
| skipped | **可回退**：`skipped → in_progress`（暂时跳过，之后补） |
| cancelled | **终态，原子取消**：只取消当前步，不级联后续步骤 |
| paused | 可回退到 `in_progress`（被中断） |
| 步骤依赖 | `depends_on UUID` 可选字段，指向前置步骤 |
| 校验模式 | **操作级** `override_reason TEXT`：production 模式 cancelled 阻塞 depends_on；debug 需写原因+审计 |
| 图片附件 | 走通用 `attachments` 模块，不内联 |

### 与 SQLite assembly_process 对比

| SQLite | PG | 说明 |
|--------|-----|------|
| `step_name` | `name` | — |
| `status` (`planned`,`in_progress`,`completed`) | `status` + `paused`/`skipped`/`cancelled` | 扩展状态 |
| `report_date` | `project_id` | 关联维度变化 |
| ❌ | `depends_on` | **新增** |
| ❌ | `assigned_to` | **新增** |
| ❌ | `started_at`/`completed_at` | **新增** |

### 数据模型
```sql
CREATE TABLE assembly_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL,
    name VARCHAR(256) NOT NULL,               -- "腔体安装"
    description TEXT,
    depends_on UUID,                          -- 前置步骤，可选。service 校验同 project、非自引用、无环。
    status VARCHAR(16) NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned','in_progress','paused','completed','skipped','cancelled')),
    assigned_to UUID,
    step_order INTEGER NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, step_order)
);
CREATE INDEX idx_assembly_project ON assembly_steps(project_id, step_order);
```

### 状态流转
```
planned → in_progress → paused → in_progress → completed
                  ↘ skipped → in_progress
                  ↘ cancelled
in_progress → cancelled
paused      → cancelled
planned     → cancelled
```
- `skipped`：暂时跳过，可回到 `in_progress`（清空 `completed_at`）
- `cancelled`：终态，原子取消（不影响后续步骤）
- `planned → skipped` **不允许**（还没开始谈不上跳过）

### depends_on 校验逻辑
- Service 层校验：`depends_on` 必须同 `project_id`、不能自引用、不能形成环（递归检查）
- production 模式：前置步骤必须 `completed`；`cancelled` 阻塞
- debug 绕过：`PATCH` 带 `override_reason`（必填），审计日志记录谁、何时、绕过哪步、原因
- `depends_on = NULL` 的首步无限制

### 后端 API
```
POST   /api/v1/projects/{id}/assembly    # 创建步骤
GET    /api/v1/projects/{id}/assembly    # 列表（按 step_order）
PATCH  /api/v1/assembly/{id}             # 修改状态/执行人
POST   /api/v1/assembly/reorder          # 调整顺序
```

### 实现契约
- **Idempotency-Key**：`POST/PATCH` 必须带 `Idempotency-Key` header
- **审计**：所有写操作审计（action=`assembly_step.create`/`assembly_step.update`/`assembly_step.reorder`，含 before/after + override_reason 如有）
- **架构**：`handler.go` → `service.go`（depends_on 循环检测 + mode 校验 + 权限）→ `repository.go`（只访问 `assembly_steps` 表）
- **并发**：状态流转用 `UPDATE ... WHERE id=? AND status=?` 乐观锁，不匹配返回 409
- **删除**：软删除（`deleted_at`），默认查询过滤
- **权限**：项目 maintainer 以上可修改；member 只读；列表按项目 ACL 过滤

### 前端页面
见 `phase4-device-pages.md` 第 4 节 — 步骤进度条 + 状态流转。

---

## C-4：RF 匹配电路 `rf_matching_records`

### 目标
记录 RF Carpet / RFQ / QPIG 的匹配电路完整参数。

### 决议
对齐 SQLite `rf_matching_circuits` 全部 17 个字段，新增 `s11`（反射系数）和 `status`（pass/adjust/fail）。

### 与 SQLite 对比

| SQLite | PG | 说明 |
|--------|-----|------|
| `device_type` (`rfq`,`rf_carpet`,`qpig`) | `device` | — |
| `match_frequency` | `frequency` | 字段名简化 |
| `input_freq` / `input_voltage` / `input_power` / `input_desc` | 同 | 输入侧保留 |
| `output_freq` / `output_voltage` / `output_power` / `output_desc` | 同 | 输出侧保留 |
| `transformer_ratio` | `transformer_turns` | — |
| `matching_capacitor` | `capacitance` | 字段名简化 |
| `transformer_material` | `transformer_material` | 磁环材料 |
| `shunt_inductance` | `shunt_inductance` | 并联电感 |
| `series_capacitor` | `series_capacitor` | 串联电容 |
| `circuit_notes` | `notes` | — |
| ❌ | `s11 DOUBLE PRECISION` | **新增**，NanoVNA 测的反射系数 |
| ❌ | `status` (`pass`,`adjust`,`fail`) | **新增** |
| `report_date` | `project_id` | 关联维度变化 |

### 数据模型
```sql
CREATE TABLE rf_matching_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL,
    device VARCHAR(16) NOT NULL                     -- rf_carpet/rfq/qpig
        CHECK (device IN ('rf_carpet','rfq','qpig')),
    frequency DOUBLE PRECISION NOT NULL,             -- MHz，匹配频率
    s11 DOUBLE PRECISION,                            -- dB，反射系数
    -- 输入侧
    input_freq DOUBLE PRECISION,
    input_voltage DOUBLE PRECISION,
    input_power DOUBLE PRECISION,
    input_desc TEXT DEFAULT '',
    -- 输出侧
    output_freq DOUBLE PRECISION,
    output_voltage DOUBLE PRECISION,
    output_power DOUBLE PRECISION,
    output_desc TEXT DEFAULT '',
    -- 匹配电路元件
    transformer_turns VARCHAR(16) DEFAULT '',
    capacitance DOUBLE PRECISION,                    -- pF
    transformer_material TEXT DEFAULT '',
    shunt_inductance TEXT DEFAULT '',
    series_capacitor TEXT DEFAULT '',
    -- 状态与备注
    status VARCHAR(16) NOT NULL DEFAULT 'pass'
        CHECK (status IN ('pass','adjust','fail')),
    notes TEXT DEFAULT '',
    measured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    measured_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_rf_project ON rf_matching_records(project_id, device, measured_at DESC);
```

### 后端 API
```
POST   /api/v1/projects/{id}/rf-matching   # 记录匹配参数
GET    /api/v1/projects/{id}/rf-matching   # 列表（按设备/时间筛选）
GET    /api/v1/rf-matching/{id}            # 详情
DELETE /api/v1/rf-matching/{id}            # 改为 quality=invalid 标记，不硬删除
```

### 实现契约
- **Idempotency-Key**：`POST` 必须带 `Idempotency-Key` header
- **审计**：所有写操作审计（action=`rf_matching.create`/`rf_matching.delete`，object=`rf_matching:{id}`，含 before/after）
- **架构**：`handler.go` → `service.go`（权限校验 + 频率/电容范围校验）→ `repository.go`（只访问 `rf_matching_records` 表）
- **删除**：不硬删除，改为 `status=fail` 标记（RF 匹配参数是实验原始数据，不可移除）
- **权限**：项目 member 以上可创建；owner + 记录创建者可标记 fail；列表按项目 ACL 过滤

### 前端页面
见 `phase4-device-pages.md` 第 3 节 — 表格 + 新增记录表单。

---

## 通用附件模块 `attachments`

### 目标
为所有模块提供统一的图片/文件上传、查询、删除能力。支持一个文件绑定多个对象，默认绑定到当前日报。

### 决议

| 项 | 决议 |
|----|------|
| 表结构 | 拆 `attachments`（文件元数据）+ `attachment_links`（绑定关系） |
| 上传绑定 | 支持一次上传直接绑定到目标对象；不指定则存为未绑定 |
| 默认绑定 | 不上传 `entity_type/entity_id` 时默认绑到当前日报 |
| 手动改绑 | `POST/DELETE /api/v1/attachments/{id}/links` 增删绑定 |
| AI 自动绑定 | 后期 Agent 扫描未绑定附件，按内容自动建议绑定 |
| 存储 | 用 `storage_key`（不暴露物理路径），支持本地/对象存储 |
| 审计 | 上传+下载+绑定变更写审计日志 |

### 数据模型
```sql
CREATE TABLE attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    storage_key TEXT NOT NULL UNIQUE,         -- 不暴露物理路径
    original_name VARCHAR(256),
    sha256 VARCHAR(64),                       -- 文件完整性校验
    mime_type VARCHAR(64),
    file_size BIGINT CHECK (file_size >= 0),
    uploaded_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE attachment_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    attachment_id UUID NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    entity_type VARCHAR(32) NOT NULL,         -- 'assembly_step'/'daily_report'/'issue'/'test_data'/...
    entity_id UUID NOT NULL,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_links_entity ON attachment_links(entity_type, entity_id);
CREATE INDEX idx_links_attachment ON attachment_links(attachment_id);
```

### 后端 API
```
POST   /api/v1/attachments                     # multipart 上传
         body: file + entity_type? + entity_id?     # 不传=默认日报，或存为未绑定
POST   /api/v1/attachments/{id}/links          # 手动加绑定
DELETE /api/v1/attachments/{id}/links/{link_id} # 解绑
GET    /api/v1/attachments?entity_type=X&entity_id=Y  # 查询
DELETE /api/v1/attachments/{id}                # 软删除
```

### 实现契约
- **Idempotency-Key**：`POST` 上传必须带 `Idempotency-Key` header，重复 POST 同文件返回已有 attachment_id
- **审计**：所有写操作审计（action=`attachment.upload`/`attachment.link`/`attachment.unlink`/`attachment.delete`，含 entity_type/entity_id/storage_key）
- **架构**：`handler.go` → `service.go`（回调目标模块验权 + SHA-256 去重 + MIME 嗅探）→ `repository.go`（只访问 `attachments` + `attachment_links` 表）
- **安全**：文件大小/类型白名单限制；文件名不参与路径拼接；下载设安全 `Content-Disposition`；图片预览防 SVG/HTML XSS
- **鉴权**：上传时回调目标模块 HTTP API 验证当前用户对 target entity 的写权限；下载时验证读权限
- **删除**：软删除（`deleted_at`），物理文件保留 N 天后异步清理
- **权限**：附件继承绑定对象的权限；未绑定附件仅上传者可操作

### 架构约束
- `attachments` 模块只能访问自己的两张表
- 上传时回调目标模块 HTTP API 校验用户权限
- 其他模块通过 HTTP API 使用附件能力，不直连 attachments 表
- 文件存储路径由部署配置决定（本地目录 / 对象存储）

---

## 整体实施 Plan

| Step | 产出 | 模块 |
|------|------|:--:|
| AT-0 | attachments + attachment_links（migration + handler+service+repository + 回调鉴权） | 通用 |
| C-1-1 | migration: test_data 表 | C-1 |
| C-1-2 | test_data 模块 (model+repo+service+handler + Idempotency + 审计) | C-1 |
| C-1-3 | test_data 前端页面 | C-1 |
| C-2-1 | migration: experiment_runs + daily_report_run_links 表 | C-2 |
| C-2-2 | runs 模块 (model+repo+service+handler + 状态转移 + HTTP 聚合) | C-2 |
| C-2-3 | runs 前端页面 | C-2 |
| C-3-1 | migration: assembly_steps 表（含 depends_on + UNIQUE） | C-3 |
| C-3-2 | assembly 模块（model+repo+service+handler + depends_on 校验 + override_reason） | C-3 |
| C-3-3 | assembly 前端页面 | C-3 |
| C-4-1 | migration: rf_matching_records 表（17 字段 + s11 + status） | C-4 |
| C-4-2 | rf-matching 模块 (model+repo+service+handler + Idempotency + 审计) | C-4 |
| C-4-3 | rf-matching 前端页面 | C-4 |

推荐顺序：**AT-0 → C-1 → C-2 → C-4 → C-3**。各模块互不依赖，可调整或并行。
迁移编号在实施时基于目标分支重新分配，不在此预固定。

## 设计文档参考

- `docs/api-contract.md` — API 约定
- `docs/project-design.md` — 项目维度设计
- `docs/permission-audit.md` — 权限审计规则
- `.hermes/plans/phase4-device-pages.md` — 前端页面设计
- `~/work/agent work/lab-daily-report/schema_gas_cell.sql` — SQLite 旧 schema（gas_cell 库）
- `~/work/agent work/lab-daily-report/schema_lab_env.sql` — SQLite 旧 schema（lab_env 库）
