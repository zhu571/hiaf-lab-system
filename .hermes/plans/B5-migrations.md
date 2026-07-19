# B-5：Instrument Migration（3 张新表）

## 产出

### 1. migration SQL 文件

新增 `migrations/015_instruments.up.sql` 和 `migrations/015_instruments.down.sql`。

三张表：

**command_log** — 对齐 instrument-security.md §6：
```sql
CREATE TABLE command_log (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id     TEXT NOT NULL,
    command_name      TEXT NOT NULL,
    risk_level        TEXT NOT NULL,
    params_raw        JSONB,
    params_normalized JSONB,
    user_id           UUID NOT NULL REFERENCES users(id),
    acting_user_id    UUID,
    lease_id          UUID REFERENCES instrument_leases(id),
    approval_id       UUID REFERENCES instrument_approvals(id),
    whitelist_version TEXT NOT NULL,
    before_snapshot   JSONB,
    result_summary    TEXT,
    error_code        TEXT,
    duration_ms       INTEGER,
    request_id        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**instrument_leases** — 对齐 security §3.2：
```sql
CREATE TABLE instrument_leases (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id TEXT NOT NULL,
    user_id       UUID NOT NULL REFERENCES users(id),
    purpose       TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at    TIMESTAMPTZ,
    revoked_by    UUID REFERENCES users(id)
);
```

**instrument_approvals** — 对齐 security §5：
```sql
CREATE TABLE instrument_approvals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lease_id     UUID REFERENCES instrument_leases(id),
    command_name TEXT NOT NULL,
    params_hash  TEXT NOT NULL,
    requested_by UUID NOT NULL REFERENCES users(id),
    approved_by  UUID NOT NULL REFERENCES users(id),
    status       TEXT NOT NULL DEFAULT 'pending',
    approved_at  TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Down: `DROP TABLE IF EXISTS instrument_approvals, instrument_leases, command_log CASCADE;`

### 2. Go model（扩展 model.go）

新增三个 struct：
- `CommandLogEntry` — 对应 command_log 表
- `Lease` — 对应 instrument_leases 表
- `Approval` — 对应 instrument_approvals 表

字段名和 json tag 对齐数据库列名。

## 规则
- 使用已有的 migration 编号方案（当前最大 014，用 015）
- 所有 TIMESTAMPTZ + DEFAULT now()
- 不做 git commit
- go build ./... 验证

## 不包含
- Repository/Service（留给 B-6）
- handler 对接
