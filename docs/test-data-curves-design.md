# test_data_curves — 测试数据扫描曲线表设计方案

> 状态：已确认（2026-08-28 用户批准）
> 作者：Hermes（按 lab-system-development 流程）
> 关联：RF Carpet 阻抗扫频数据存档需求；复用 testdata 模块现有权限/审计模式

## 1. 背景

RF Carpet 阻抗扫频（Hioki IM3536，1-8MHz，80 点）这类**曲线型测试数据**，现有 `test_data` 表是**单条标量**模型（`value DOUBLE PRECISION`），`data_type` 枚举也只有 `cryo|pressure|voltage|rf_voltage|efficiency`，**装不下扫描曲线序列**。此前误把这类数据塞进 `rf_matching_records`，已被用户纠正（"这是测试数据不是 rf 匹配数据"）。

需求：在 test-data 域下新建一张**专门存储扫描曲线**的表，能装下 80 点曲线，并复用 testdata 模块的权限/审计/CRUD 模式。

## 2. 方案

### 2.1 新表 `test_data_curves`（迁移 044）

```sql
CREATE TABLE test_data_curves (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    run_id      UUID REFERENCES experiment_runs(id) ON DELETE SET NULL,
    name        VARCHAR(128) NOT NULL,
    curve_type  VARCHAR(32) NOT NULL DEFAULT 'impedance_sweep'
                CHECK (curve_type IN ('impedance_sweep','s11_sweep','custom')),
    x_label     VARCHAR(64) NOT NULL DEFAULT '频率 (Hz)',
    y_label     VARCHAR(64) NOT NULL DEFAULT '阻抗 |Z| (Ω)',
    unit        VARCHAR(16) NOT NULL DEFAULT '',
    points      JSONB NOT NULL,
    quality     VARCHAR(16) NOT NULL DEFAULT 'normal'
                CHECK (quality IN ('normal','outlier','suspect','invalid')),
    source      VARCHAR(16) NOT NULL DEFAULT 'import'
                CHECK (source IN ('manual','instrument','import','agent','backfill')),
    notes       TEXT NOT NULL DEFAULT '',
    is_void     BOOLEAN NOT NULL DEFAULT false,
    voided_at   TIMESTAMPTZ,
    voided_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    void_reason TEXT,
    measured_at TIMESTAMPTZ,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_curves_project ON test_data_curves(project_id, created_at DESC);
CREATE INDEX idx_curves_run ON test_data_curves(run_id) WHERE run_id IS NOT NULL;
```

**对齐 rf_matching 先例**：`is_void`/`voided_at`/`voided_by`/`void_reason` 软作废四件套（migration 019）。

### 2.2 `points` JSONB 结构

每点三字段，保留 θ（RF Carpet 阻抗扫频有 θ）：

```json
[
  {"freq_Hz": 1000000, "Z_ohm": 0.957, "theta_deg": -92.75},
  {"freq_Hz": 1088608, "Z_ohm": 0.324, "theta_deg": -86.57}
]
```

Go 端 `CurvePoint{FreqHz, ZOhm, ThetaDeg *float64}`，JSON tag 小写下划线。

### 2.3 Go 模块（放 `go-server/testdata/`，复用现有模式）

复用 testdata 模块的 handler/service/repository 骨架，新增方法：

| 文件 | 新增 |
|------|------|
| `model.go` | `Curve` / `CurvePoint` / `CreateCurveRequest` / `ListCurvesParams` |
| `repository.go` | `CreateCurve` / `GetCurve` / `ListCurves` / `UpdateCurve` / `MarkCurveVoid` |
| `service.go` | `CreateCurve` / `GetCurve` / `ListCurves` / `UpdateCurve` / `VoidCurve`（复用 `requireProject`/`requireAccess`） |
| `handler.go` | 5 个 handler，复用 `projectID()`/`requireIdempotencyKey`/`writeError` |

#### API 端点（复用 testdata 权限模型：项目 member+ 可写）

| 方法 | 路径 | 审计 action |
|------|------|-----------|
| POST | `/api/v1/projects/{project_id}/test-data-curves` | `test_data_curve.create` |
| GET | `/api/v1/projects/{project_id}/test-data-curves` | — |
| GET | `/api/v1/test-data-curves/{id}` | — |
| PATCH | `/api/v1/test-data-curves/{id}` | `test_data_curve.update` |
| DELETE | `/api/v1/test-data-curves/{id}`（软作废） | `test_data_curve.delete` |

#### 校验规则（对齐 testdata）
- `curve_type` ∈ `impedance_sweep|s11_sweep|custom`
- `points` 非空数组，元素需含 `freq_Hz`（数值）+ 至少一个 y 值字段
- `name` 非空 ≤128；`unit` ≤16；`quality`/`source` 枚举
- `run_id` 若有需过 `RunValidator.Exists`
- 写接口必须 `Idempotency-Key` + 审计

### 2.4 CLI 子命令（labctl）

`labctl test-data curve` 子命令组：
- `list`（按 project/curve_type 过滤）
- `entry`（录入一条曲线，name/points 走 JSON 文件或 stdin）
- `get` / `void`

### 2.5 权限与安全

- 复用 testdata 的 `ProjectAccessAdapter`：非项目成员读 403，member+ 可写
- 写接口强制 Idempotency-Key + 审计（中间件）
- `points` JSONB 上限校验（防超大 payload，参考 testdata batch 512KB 纵深）

## 3. 迁移注意事项

- 迁移编号 **044**（下一个版本号，确认当前最高是 043）
- `.up.sql` / `.down.sql` 成对；down 为 `DROP TABLE test_data_curves`
- 只追加新迁移，不改已有迁移

## 4. 验收标准

1. `go test ./...` 通过
2. 迁移 044 up/down 可逆
3. API CRUD 全链路：录入一条 80 点曲线 → list 可见 → get 读回 points → void 后 active 列表隐藏
4. 权限：非成员 403，member+ 写成功；写接口无 Idempotency-Key 400
5. CLI：`labctl test-data curve entry/list/get/void` 可用
6. 审计：create/update/delete 各落一条 `test_data_curve.*`

## 5. 后续（本方案完成后）

用本表重新导入 RF Carpet 7 条阻抗扫频曲线（此前误存 rf_matching_records 的 7 条已作废，数据仍在库里可追溯）。
