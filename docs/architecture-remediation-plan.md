# 架构缺陷修复方案（AR-01～AR-12）

> 版本：v1.0（2026-11-24）
> 状态：待评审
> 范围：`docs/architecture-review.md` 中 P1 + P2 共 12 项（AR-01～AR-12）。
> P3 的 6 项（AR-13～AR-18）不在本文展开，文末给处理策略。
> 关联：`docs/architecture-review.md`、`docs/multi-user-agent-upgrade.md`
> 原则：先修“会丢数据/会误判权限”的，再修“扩展性”，最后修“可维护性”；每项独立可回滚。

---

## 0. 总览

### 0.1 修复批次

| 批次 | 内容 | 目标 | 预估 |
|---|---|---|---|
| **Batch 0** | AR-12 配置集中化、AR-07 观测基础、AR-03 幂等 | 打好“可观测 + 可重试 + 可配置”的地基 | 1.5–2 周 |
| **Batch 1** | AR-02 审计、AR-06 备份、AR-05 DB 角色 | 补数据安全与恢复能力 | 2–3 周 |
| **Batch 2** | AR-01 单实例状态、AR-04 Agent 并发 | 让运行时能承载多人 | 2–3 周 |
| **Batch 3** | AR-08 拆 main、AR-09 单一事实源、AR-10 CI、AR-11 前端 | 降长期维护成本 | 2–3 周（可并行） |
| **并行特性** | GC 扫描数据存档（scan-archive v0.4） | 独立功能，不占 AR 修复优先级 | 6–8 周（单人，可分 PR） |
| **P3** | AR-13～AR-18 | 顺手修/待确认 | 3–5 天 |

> 单人总工期约 8–10 周；两人并行约 5–6 周。Batch 0 完成即可开始多人 Agent 的 S1 会话层。

### 0.2 依赖关系

```
AR-12 配置 ─┬─> AR-05 角色（DB 连接参数集中）
            ├─> AR-01 限流（配置开关）
            └─> AR-04 并发（worker 数/信号量）
AR-07 观测 ─┬─> AR-02 审计失败告警
            ├─> AR-04 排障
            └─> AR-10 CI 指标
AR-03 幂等 ─┬─> 多人 Agent 的消息重试
            └─> AR-10 契约测试
AR-06 备份 ───> 上线前必须（与代码无关）
AR-05 角色 ───> AR-02 归档（归档任务需独立角色）
```

### 0.3 迁移编号协调

`multi-user-agent-upgrade.md` 原计划把 045 留给 `assistant_*`。实际落地时 **045 已用于幂等 V2（AR-03）**，后续从 046 起顺延：

- 045 `idempotency_keys` 改造（AR-03）✅ 已落地
- 046 `audit_log` 月分区（AR-02）✅ 已落地
- 047 `rate_limit_counters`（AR-01）✅ 已落地
- 048 `scheduler_claims`（AR-01）✅ 已落地
- 049 `pending_agent_tasks.queue_class`（AR-04）✅ 已落地
- 050 `scans`（scan-archive，并行特性）✅ PR1 后端骨架已落地
- 051 `system_update_requests`（AR-06b）
- 052 `db roles/grants`（AR-05，或放运维脚本）
- 053+ `assistant_*`（多人 Agent 会话层）

**不要修改已发布迁移**；会话层开工前再按当时的最大编号顺延。

### 0.4 实施状态（2026-11-25）

| 项 | 状态 | 已落地内容 | 剩余 |
|---|---|---|---|
| AR-04 Agent 并发 | 🟡 主体 | AskEngine 线程本地 client（去全局锁）、serve 每 loop 信号量、uvicorn 多 worker（factory）、worker 多线程、迁移 049 queue_class + claim 优先 interactive | 取消/超时传播、交互式任务生产者、Go SSE 断线取消 |
| AR-01 单实例状态 | 🟡 主体 | 迁移 047 共享限流 + 048 调度认领；ask/模板/日报 AI 限流切 DB；todos/weekly/ask retention 认领；单测 + DB 验证 | 更新 SSE/会话状态外置；alert 维护去重（幂等，暂可重复） |
| AR-12 配置 | 🟡 Phase 1 | `internal/config`、`common.DBOptions`、main 接线、`DB_PASSWORD_FILE`、单测 | 各模块 `AutoConfigure` 仍读 env；`.env` 自动生成 |
| AR-07 观测 | 🟡 主体 | `Recoverer`、HTTP ReadHeader/Idle 超时、路由级 `Timeout`（ask/AI 解析/周报/经验/仪器 NL）、panic/审计失败指标、`X-Request-Id` Go→py-agent 透传、Python 中间件与日志字段、单测 | OTel、LLM 调用指标 |
| AR-03 幂等 | 🟡 主体 | 迁移 045、V2 作用域/TTL/响应重放/失败释放、清理任务、单测 + DB 集成测试 | 流式/大响应的可重放策略、前端重试复用 key |
| AR-02 审计 | 🟡 Phase 2 | 失败指标 + 60s 限频告警；迁移 046 月分区 + `EnsurePartitions` 每日维护；带存量数据 up/down、跨月路由、链连续已验证 | 分区 detach/归档（需链锚点）、敏感读审计 |
| AR-06 备份 | 🟡 6a | `backup.sh` 补 InfluxDB/附件/图片/清单/保留/异地；`restore-check.sh` 校验附件与 Influx manifest | docker.sock 收窄/宿主更新代理；Influx 实际 restore 演练命令 |
| AR-05 DB 角色 | 🟡 脚本 | `deploy/scripts/init_db_roles.sh`（lab_app + 最小权限 + 默认权限 + 审计禁改删） | 切换 compose `DB_USER=lab_app` + 停机冒烟；CI 以 lab_app 跑一遍 |

> 标注说明：🟡 = 代码已落地但未完成全部验收（尤其缺少 `TEST_DATABASE_URL` 下的迁移/集成测试与生产冒烟）。
>
> **验证（2026-11-25）**：`go build ./...`、`go vet ./...` 通过；`internal/config`、`common`、`middleware`、`ask`、`steptemplates`、`logs`、`todos`、`weekly`、`agent` 单测通过；`py-agent` 的 `test_ask` + `test_request_id` 36 项通过（UTF-8 模式）。迁移 045～049 均在临时 PostgreSQL 16 上完成 up/down；幂等 6 项 DB 测试、共享限流/调度认领 DB 测试、`EnsurePartitions` 与 046 带存量数据迁移均通过。全量 `go test ./...` 仅 `system.TestGitLsRemoteUsesDeployKey` 因本机 PATH 无 `git` 失败（环境问题，非本次改动）；DB 全量套件需 CI 的 `postgres:16`（本机临时库缺 `pgcrypto`，审计 hash 触发器只能以 stub 验证分区/链结构）。

### 0.5 统一约定

- 每项都带 feature flag（环境变量），默认关闭或保持旧行为，验证后切默认。
- 每个迁移必须有 down；数据迁移写清回滚步骤。
- 每项至少覆盖“正常 + 一个异常 + 一个并发/越权”测试。
- 改动前先补 `docs/api-contract.md`（涉及 API 的项）。

---

## 1. AR-03 幂等语义修正（Batch 0）

### 目标

- 同一 `(user, method, path, key)` 重试返回**同一响应**，而不是 409。
- 处理失败或 5xx 时释放 key，允许客户端重试。
- key 按用户/端点隔离，TTL 自动清理。

### 方案

**数据模型**（迁移 048）：

```sql
-- 1) 加作用域与响应字段
ALTER TABLE idempotency_keys
    ADD COLUMN user_id UUID,
    ADD COLUMN method VARCHAR(10) NOT NULL DEFAULT '',
    ADD COLUMN path VARCHAR(256) NOT NULL DEFAULT '',
    ADD COLUMN state VARCHAR(16) NOT NULL DEFAULT 'done',  -- in_progress/done
    ADD COLUMN response_status INT,
    ADD COLUMN response_content_type VARCHAR(128),
    ADD COLUMN response_body BYTEA,
    ADD COLUMN locked_at TIMESTAMPTZ,
    ADD COLUMN expires_at TIMESTAMPTZ NOT NULL DEFAULT now() + interval '7 days';

-- 2) 旧主键换成作用域唯一
ALTER TABLE idempotency_keys DROP CONSTRAINT idempotency_keys_pkey;
ALTER TABLE idempotency_keys
    ADD COLUMN scope_key TEXT GENERATED ALWAYS AS (
        COALESCE(user_id::text, '') || '|' || method || '|' || path || '|' || idempotency_key
    ) STORED;
CREATE UNIQUE INDEX uq_idempotency_scope ON idempotency_keys(scope_key);
CREATE INDEX idx_idempotency_expires ON idempotency_keys(expires_at);
```

> 旧数据 `user_id=NULL`、`method/path=''`，会生成 `legacy` 作用域；7 天后自然清掉。

**中间件流程**（改 `middleware/idempotency.go`）：

1. 从 claims 取 `user_id`（无 claims 的登录/注册用 `path` + `client_ip` 兜底）。
2. `INSERT ... state='in_progress' ON CONFLICT (scope_key) DO NOTHING`。
3. 插入成功 → 包装 `ResponseWriter`，捕获 status/body（≤256KB；`text/event-stream` 不捕获）。
4. 请求结束：
   - `2xx/3xx/4xx` 且 body 可捕获 → `UPDATE state='done', response_*`；
   - `5xx` / panic → `DELETE` 该行（允许重试）。
5. 插入冲突 → `SELECT`：
   - `done` → 原样重放（status/body/content-type）+ `X-Idempotent-Replay: true`；
   - `in_progress` 超过 60s → 抢占（更新 `locked_at`）继续执行；
   - 否则 → `409 idempotency_in_progress` + `Retry-After: 2`。
6. 每日清理 `expires_at < now()`；`IDEMPOTENCY_TTL` 可配。

**边界**：

- 附件上传/下载、SSE 不存 body：重复请求返回 `409 duplicate_idempotency_key`（保留旧行为），文档写明。
- 响应 >256KB：只存 status + hash，重复时返回 `409` 并提示“原响应过大，请用 request_id 查询”。
- service token 调用继续跳过（业务幂等由服务端保证）。

### 改动

- `migrations/048_idempotency_scope.up.sql/.down.sql`
- `middleware/idempotency.go` + `idempotency_test.go`
- `middleware/response_writer`（复用 audit 的 `responseWriter`，抽出公共实现）
- `common/cleanup.go` 或 `main.go` 起清理 ticker
- `docs/api-contract.md` 补 409 语义
- 前端 `client.ts`：网络重试时复用同一个 `Idempotency-Key`（当前拦截器每次生成新 key）

### 测试

- 同 key 二次请求 → 同 body/status，`X-Idempotent-Replay: true`。
- 首次 500 → key 删除 → 同 key 重试成功。
- 用户 A/B 同 key → 互不影响。
- 并发同 key 10 个请求 → 1 个执行、9 个重放或 409。
- TTL 清理后同 key 可重新使用。

### 工作量与风险

- 4–5 天。
- 风险：捕获 body 的内存/大响应；限制 256KB + 流式跳过。
- 回滚：`IDEMPOTENCY_V2=false` 走旧逻辑；迁移 down 恢复旧主键（先清空表）。

---

## 2. AR-07 观测基础（Batch 0）

### 目标

- panic 不再断连；慢连接不堆积。
- 一个 `request_id` 串起 Go → py-agent → LLM。
- 审计/LLM/队列有可告警指标。

### 方案

**1. HTTP 健壮性**

```go
srv := &http.Server{
    Addr:              ":" + port,
    Handler:           r,
    ReadHeaderTimeout: 5 * time.Second,
    IdleTimeout:       120 * time.Second,
    MaxHeaderBytes:    1 << 20,
}
```

- 不设全局 `ReadTimeout/WriteTimeout`（附件 100MB、SSE 长连接会被误杀）。
- 普通 API 路由加 `middleware.Timeout(60 * time.Second)`；上传路由 5 分钟；weekly 360s；SSE 路由不加。
- 前提：handler 把 `r.Context()` 传到 DB/HTTP 调用（当前大部分已传，补齐少数直连即可）；否则超时只返回 504、底层调用仍在跑。
- 注册 `middleware.Recoverer`（chi 自带）+ 自定义 `recoverer`：记录 stack、`lab_panics_total`、未写头时返回统一 500 envelope。

**2. Request ID 透传**

- 新增 `common.RequestIDFrom(ctx)`（已有 `GetRequestID`）。
- 所有出站客户端设置 `X-Request-Id`：`logs` AI 解析、`ask`、`steptemplates`、`weekly`、`instruments`。
- py-agent `serve.py` 加 Starlette 中间件：读 `X-Request-Id`，放 `contextvars`，日志与错误响应带上；回调 Go 时继续带。
- Caddy 默认透传；确认 `X-Request-Id` 不在删除列表。

**3. 指标**

| 指标 | 类型 | 用途 |
|---|---|---|
| `lab_panics_total` | counter | panic 告警 |
| `lab_audit_write_failed_total` | counter | 审计失败（AR-02） |
| `lab_llm_requests_total{stage,status}` | counter | 模型调用 |
| `lab_llm_request_duration_seconds{stage}` | histogram | 延迟 |
| `lab_agent_queue_depth` | gauge | 已有，确认接入 |
| `lab_py_agent_upstream_errors_total` | counter | 502 降级 |

**4. OTel（可选，Batch 3）**

- Go 用 `otelhttp` 包一层；py-agent 用已依赖的 `opentelemetry-sdk`；先只做 trace，不接 collector 时 no-op。
- 不做也行：request_id + 结构化日志已能覆盖 80% 排障。

### 改动

- `middleware/recover.go`、`middleware/timeout.go`、`middleware/request_id.go`
- `main.go` server 配置、路由超时
- `logs/service.go`、`ask/service.go`、`steptemplates/service.go`、`weekly/client.go` 出站头
- `py-agent/serve.py` 中间件 + `tools/api.py` 出站头
- `deploy/prometheus/prometheus.yml` 告警规则（panic/audit/5xx）

### 测试

- handler panic → 500 + `lab_panics_total` +1（不崩进程）。
- 慢请求 65s → 超时返回 504/取消（SSE/上传不受影响）。
- 一次 ask：Go 审计、py-agent 日志、LLM 日志同一 request_id。

### 工作量与风险

- 2–3 天。
- 风险：超时误杀长任务；按路由配置，默认 60s，长任务显式放宽。
- 回滚：`HTTP_TIMEOUTS_ENABLED=false`、`RECOVERER_ENABLED=false`。

---

## 3. AR-12 配置集中化（Batch 0）

### 目标

- 所有环境变量在一个结构体里解析、校验、脱敏打印。
- 启动即失败，而不是运行到一半才发现漏配。
- `.env.example` 与代码一致。

### 方案

新增 `go-server/internal/config`：

```go
type Config struct {
    HTTP      HTTP
    DB        DB
    Auth      Auth
    AI        AI
    Influx    Influx
    Update    Update
    Scheduler Scheduler
    Features  Features
}
func Load() (*Config, error) // 读取 env + /run/secrets，校验必填/范围/URL
func (c *Config) LogRedacted()
```

- 必填项缺失 → `fmt.Errorf("config: DB_PASSWORD is required")`，`main` 直接退出。
- duration/int/bool 解析失败 → 报错，不给隐式默认。
- `Features` 放 `IDEMPOTENCY_V2`、`AUDIT_ARCHIVE`、`RATE_LIMIT_DB`、`ASSISTANT_ENABLED` 等开关。
- `common.OpenDB` 接受 `DB.PasswordFile`（当前硬编码 `/run/secrets/db_password`）。
- 各模块 `AutoConfigure()` 改为接收 config 值（或保留但只读 config）。
- `.env.example` 由 `cmd/envdoc` 生成（或测试比对 key 集合）。

### 改动

- `internal/config/config.go`、`config_test.go`
- `main.go`、`common/db.go`、各模块 `AutoConfigure`
- `deploy/.env.example`、`deploy/docker-compose.yml`（显式传参）
- `cmd/envdoc/main.go`

### 测试

- 缺必填 → 启动失败且信息明确。
- 非法 duration → 失败。
- 脱敏日志不含密码/token。
- `.env.example` key 集合与 config 字段一致（测试）。

### 工作量与风险

- 3–4 天。
- 风险：漏改某个 env 导致行为变化；分批替换，先 main.go 再模块。
- 回滚：保留 `os.Getenv` 兜底一个版本。

---

## 4. AR-02 审计加固（Batch 1）

### 目标

- 审计写失败可见、可告警，不再“只 log”。
- 审计表可按月分区，后续可安全归档，不无限膨胀。
- 敏感读可追溯。

### 方案

**Phase 1（1–2 天）：失败可见**

- `insertAuditLog` 失败 → `lab_audit_write_failed_total++` + `ReportAlert("critical","security","审计写入失败", ...)`（限频 1 次/分钟）。
- 重试 1 次（100ms 退避），仍失败才告警。
- 加 `lab_audit_write_duration_seconds`，观察 advisory lock 等待。

**Phase 2（已落地：月分区，路线 A）**

- 迁移 046：`audit_log` 按月 `RANGE(created_at)` 分区，PK `(id, created_at)`；
  旧表改名 `audit_log_legacy` 保留，存量行/序列/hash 链原样搬迁。
- `audit.Service.EnsurePartitions`：启动补齐“当前 + 未来 3 个月”分区，之后每日检查；
  `main.go` 起 `StartPartitionMaintenance`。跨月不补分区会导致审计 INSERT 失败。
- 已验证（临时 PG16）：带存量数据 up/down、跨月分区路由、链 `prev_hash` 连续、
  `EnsurePartitions` 创建当月分区。
- **未做：分区 detach/归档**。detach 后父表看不到尾部行，新行 `prev_hash` 会断链；
  必须先做链锚点（路线 B 的 `audit_chain_state`）或只做冷导出，不能直接 detach。
- `audit_log_legacy` 确认后可另行 DROP（不要放进同一迁移，留回滚窗口）。

**Phase 3（后续）：归档/detach**

- 方案：`audit_chain_state(id, hash)` 锚点表 + 触发器 fallback；或保留全部分区只做冷存储导出。
- 未定前不 detach/drop 任何分区；全链 `VerifyChain` 仍以 `audit_log` 父表为准。

**Phase 4：敏感读审计**

- 显式 `SetAuditAction`：日报详情、团队日报详情、附件下载、ask 历史详情。
- 审计端点本身不审计（防递归）。
- 其余 GET 保持不审计，控制写入量。

### 改动

- `middleware/audit.go`、`audit/service.go`、`audit/handler.go`、`main.go`
- `migrations/046_audit_partitioning.*`
- 分区维护：`auditSvc.StartPartitionMaintenance(ctx)`
- `logs/handler.go`、`attachments/handler.go`、`ask/handler.go` 的读审计

### 测试

- 模拟 DB 失败 → 指标/告警（用 sqlmock 或断连）。
- 046：带存量数据 up/down；跨月分区路由；`prev_hash` 连续。
- `EnsurePartitions`：当月分区存在、重复执行幂等。
- 敏感读产生审计行，普通读不产生。

### 工作量与风险

- 4–6 天。
- 风险：046 会 `ACCESS EXCLUSIVE` 锁表并全表复制，必须在维护窗口执行，先做备份；
  大数据量下复制时间与磁盘翻倍（legacy 保留）要预留空间。
- 回滚：046 down 会丢弃分区化后新写入的审计行，仅用于上线前回滚；上线后回滚应改回
  “只停写、不 down”，或先用锚点方案合并数据。

---

## 5. AR-06 备份与部署边界（Batch 1）

### 目标

- DB + InfluxDB + 附件 + 图片可恢复。
- 至少一份异地副本。
- server 不再持有宿主 `docker.sock`（或至少收窄）。

### 方案

**6a 备份补全（先做，3–4 天）**

`deploy/scripts/backup.sh` 扩展：

```bash
# InfluxDB（在线备份）
docker compose exec -T influxdb influx backup "/tmp/influx-$DATE" --token "$(cat secrets/influxdb_token.txt)"
docker cp lab-influxdb:"/tmp/influx-$DATE" "$BACKUP_DIR/"

# 附件 volume
docker run --rm -v lab-uploads:/data:ro -v "$BACKUP_DIR":/backup alpine \
  tar czf "/backup/uploads-$DATE.tar.gz" -C /data .

# images
tar czf "$BACKUP_DIR/images-$DATE.tar.gz" -C "$REPO_ROOT" images

# 异地（可选）
if [ -n "${RCLONE_REMOTE:-}" ]; then rclone sync "$BACKUP_DIR" "$RCLONE_REMOTE"; fi
```

- 每次生成 `manifest-$DATE.json`（文件、大小、sha256）。
- 保留策略按类型：DB 14 天、Influx 7 天、附件 30 天。
- `restore-check.sh` 增加：Influx restore 到临时 bucket、附件 tar 校验、DB 关键行数。
- 异地：rclone（NAS/S3/MinIO）或 rsync 到 VPS；失败告警。

**6b 移除 docker.sock（5–8 天，单独决策）**

目标架构：

```
server ──写 system_update_requests──> Postgres
                                        │
宿主机 systemd timer ──读 pending──────┘
        └─ 执行 .hermes/update.sh（宿主，无容器 socket）
        └─ 回写 status/log
```

- 迁移 050：`system_update_requests(id, requested_by, branch, status, log_path, created_at, updated_at)`。
- server 的 `/api/v1/system/update` 只写请求行；SSE 读状态文件/表。
- 宿主机 `lab-update-agent.sh` 用 service token 拉 pending、执行、回写。
- 过渡：保留 `UPDATE_ENGINE=go` 旧路径一个版本。

短期（1 天）可选：`tecnativa/docker-socket-proxy` 限制 API（`CONTAINERS=1 IMAGES=1 BUILD=1 EXEC=1`），server 连 proxy；但这仍是“能创建特权容器”的权限，只是收窄，不是根治。

### 改动

- `deploy/scripts/backup.sh`、`restore-check.sh`、`deploy/systemd/*`
- `deploy/docker-compose.yml`（secrets/volume/环境变量）
- 6b：`go-server/system/service.go`、`cmd/update-runner`、迁移 050、宿主机脚本

### 测试

- 从零恢复：DB + Influx + 附件 + images，业务可登录、图表有数据、附件可下载。
- 断网/远端不可达 → 本地备份仍成功 + 告警。
- 6b：无 socket 时更新流程仍可完成；权限提升路径消失。

### 工作量与风险

- 6a 3–4 天；6b 5–8 天。
- 风险：Influx backup 需要 token 与磁盘空间；restore 到临时 bucket 需隔离。
- 回滚：6b 保留旧 `UPDATE_ENGINE`；6a 纯增量。

---

## 6. AR-05 DB 权限模型（Batch 1）

### 目标

- 应用账号不能 DDL，不能改/删审计。
- 新表自动获得应用权限，不靠手工 GRANT。
- `ask_reader` 仍然只读白名单。

### 方案

**角色划分**

| 角色 | 用途 | 权限 |
|---|---|---|
| `lab`（owner） | 迁移/运维 | 全权 |
| `lab_app` | Go server | DML（SELECT/INSERT/UPDATE/DELETE）+ 序列，无 DDL |
| `lab_maint` | 归档/清理任务 | `lab_app` + `audit_log` DELETE |
| `ask_reader` | ask execute | 18 表 SELECT |

**初始化脚本** `deploy/scripts/init_db_roles.sh`（用 `db_password` 以 lab 连接）：

```sql
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='lab_app') THEN
    CREATE ROLE lab_app LOGIN;
  END IF;
END $$;
ALTER ROLE lab_app PASSWORD :'app_password';

GRANT CONNECT ON DATABASE lab TO lab_app;
GRANT USAGE ON SCHEMA public TO lab_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO lab_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO lab_app;
ALTER DEFAULT PRIVILEGES FOR ROLE lab IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO lab_app;
ALTER DEFAULT PRIVILEGES FOR ROLE lab IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO lab_app;
GRANT ask_reader TO lab_app;

REVOKE UPDATE, DELETE, TRUNCATE ON audit_log FROM lab_app;
```

- 新 secret：`db_app_password`；compose `server` 挂载为 `/run/secrets/db_app_password`。
- `common.OpenDB` 支持 `DB_PASSWORD_FILE`（当前硬编码 `db_password`）。
- `server` 环境：`DB_USER=lab_app`、`DB_PASSWORD_FILE=/run/secrets/db_app_password`。
- `migrate` 服务仍用 `lab`（`db_password`）。

**验证**

- 启动后跑一遍全量业务冒烟（登录、写日志、审批、ask、附件）。
- 集成测试以 `lab_app` 连接：
  - `CREATE TABLE` 失败；
  - `UPDATE audit_log` / `DELETE audit_log` 失败；
  - `SET LOCAL ROLE ask_reader` 成功。

**风险与回滚**

- 漏 GRANT 会在运行时 500；用 `ALTER DEFAULT PRIVILEGES` + 启动自检（查询 `has_table_privilege`）降低风险。
- 回滚：切回 `DB_USER=lab`（保留角色不删）。

### 工作量

- 3–5 天（含全量冒烟）。

---

## 7. AR-01 单实例状态（Batch 2）

### 目标

- 多副本时调度不重复、限流共享。
- 单副本仍是默认，但不再“假设只有一个进程”。
- HTTP 超时与优雅退出完善。

### 方案

**1. 调度器 leader lock（2 天）**

新增 `common/leader.go`：

```go
func WithAdvisoryLock(ctx context.Context, db *sql.DB, key int64, fn func(context.Context) error) (bool, error) {
    conn, err := db.Conn(ctx)          // 必须同一连接，会话级锁
    if err != nil { return false, err }
    defer conn.Close()
    var ok bool
    if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&ok); err != nil || !ok {
        return false, err
    }
    defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", key)
    return true, fn(ctx)
}
```

- 锁键：714101 todos、714102 weekly、714103 ask retention、714104 alert maintenance。
- 各 scheduler 在 `runGenerate`/`runBatch` 外包一层；拿不到锁 → debug 日志 + skip。
- 注意：锁连接不能被连接池复用；`db.Conn` 独占直到 Close。

**2. 限流共享（3 天）**

迁移 046：

```sql
CREATE TABLE rate_limit_counters (
    scope        TEXT NOT NULL,
    subject      TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    count        INT NOT NULL DEFAULT 0,
    PRIMARY KEY (scope, subject, window_start)
);
```

- `middleware/ratelimit` 或 `common/ratelimit`：

```sql
INSERT INTO rate_limit_counters(scope, subject, window_start, count)
VALUES ($1, $2, date_trunc('minute', now()), 1)
ON CONFLICT (scope, subject, window_start) DO UPDATE
SET count = rate_limit_counters.count + 1
RETURNING count;
```

- 替换 `ask.allowOne`、`steptemplates` 生成限流、`logs` AI 解析限流。
- 清理：每小时删 `window_start < now() - interval '1 hour'`。
- 开关 `RATE_LIMIT_DB=false` 回退内存实现。

**3. SSE 与更新会话（记录约束，2 天）**

- 更新会话在内存 + 磁盘 marker；单副本可恢复。
- 多副本前：Caddy 不能做 sticky（frp 单上游），需要“状态外置 + 事件重放”。
- 在 `docs/maintenance-strategy.md` 写明“server 副本数=1”，直到 AR-01 Phase 2 完成。
- 多人 Agent 的会话/流式按 `multi-user-agent-upgrade.md` 的 DB 事件模型做，不在这里重复。

**4. HTTP 超时（见 AR-07）**

### 改动

- `common/leader.go`、`common/ratelimit`、迁移 046
- `todos/scheduler.go`、`weekly/scheduler.go`、`ask/service.go`（retention）、`alert/service.go`
- `ask/service.go`、`steptemplates/service.go`、`logs/service.go` 限流调用
- `main.go` 锁键常量、配置
- `docs/maintenance-strategy.md` 单副本说明

### 测试

- 两个进程同时跑 scheduler → 同一分钟只有一个执行。
- 两个进程并发限流 → 总数不超过阈值。
- 锁连接归还后不泄漏（连接数监控）。

### 工作量与风险

- 4–5 天。
- 风险：会话级锁忘记释放 → 连接泄漏；用 defer + 超时。
- 回滚：`SCHEDULER_LOCK=false`、`RATE_LIMIT_DB=false`。

---

## 8. AR-04 Agent 并发（Batch 2）

### 目标

- 多个用户同时提问不互相阻塞。
- 批处理与交互式任务不互相饿死。
- 为工具循环/流式做好运行时准备。

### 方案

**1. 去掉 AskEngine 全局锁（1 天）**

```python
self._local = threading.local()

def _client(self):
    client = getattr(self._local, "client", None)
    if client is None:
        client = httpx.Client(timeout=self.EXECUTE_TIMEOUT)
        self._local.client = client
    return client
```

- 删除 `self._client_lock` 与 `with`。
- `close()` 时关闭各线程 client（或依赖进程退出）。

**2. 全局限流（1 天）**

- `serve.py` 增加 `asyncio.Semaphore(int(os.getenv("PY_AGENT_MAX_CONCURRENCY", "8")))`。
- 每个 LLM 端点 `async with sem:`；等待超时返回 `503 busy`（前端提示重试）。
- 不要用线程池大小隐式限流（`asyncio.to_thread` 默认池不可控）。

**3. 多 worker（1–2 天）**

- `serve.py` 模块级 `app = create_app(...)`，`uvicorn.run("serve:app", workers=N)`。
- `UVICORN_WORKERS` 默认 2（单机 CPU 核数一半）；每个 worker 独立 AskEngine。
- 健康检查不受影响。

**4. Worker 并发 + 队列分级（2 天）**

- `worker.py` 支持 `WORKER_CONCURRENCY`（默认 2），每个线程一个 `Worker` + 独立 `GoAPI`（claim 用 `SKIP LOCKED` 已保证不重）。
- 迁移 049：`pending_agent_tasks.queue_class VARCHAR(16) DEFAULT 'batch'`。
- claim 增加过滤：交互式优先（`ORDER BY queue_class='interactive' DESC, created_at`）。
- 现有日报解析写 `batch`；未来的 assistant 写 `interactive`。

**5. 取消与超时（2 天，可与多人 Agent 一起）**

- Go `POST /api/v1/assistant/runs/{id}/cancel` 置 `cancelled`。
- py-agent 每阶段前查 run 状态（回调 Go 或本地 cancel 文件）；至少保证“不再产生新 token”。
- 单次 run 总 deadline（如 120s）+ 每阶段预算。

### 改动

- `py-agent/tools/ask.py`、`py-agent/serve.py`、`py-agent/worker.py`
- `deploy/docker-compose.yml`（`UVICORN_WORKERS`、`WORKER_CONCURRENCY`、`PY_AGENT_MAX_CONCURRENCY`）
- 迁移 049 + `agent/repository.go` claim/queue
- 测试：并发 ask、双 worker claim、限流 503

### 工作量与风险

- 4–6 天。
- 风险：DeepSeek 侧 429；用信号量 + 退避，不做无限重试。
- 回滚：`PY_AGENT_MAX_CONCURRENCY=1`、`UVICORN_WORKERS=1`、`WORKER_CONCURRENCY=1`。

---

## 9. AR-08 拆 main.go（Batch 3）

### 目标

- `main.go` 只做“读配置 → 建依赖 → 起服务”。
- 路由按域分文件，新增模块不动 800 行大文件。

### 方案

分三步，每步独立 PR：

1. **抽 deps**：`type deps struct { DB *sql.DB; Cfg *config.Config; Auth *auth.Service; ... }`；`buildDeps()` 放 `wiring.go`。
2. **拆路由**：`router.go` 里 `mountPublic/mountAPIv1`；按域拆 `routes_auth.go`、`routes_logs.go`、`routes_ai.go`、`routes_instruments.go`、`routes_admin.go`。**不改任何路径/中间件顺序**。
3. **拆大文件**：
   - `system/updater.go` → `updater_prepare.go` / `updater_run.go` / `updater_verify.go` / `updater_rollback.go`
   - `logs/service.go` → `service_reports.go` / `service_logs.go` / `service_translation.go` / `service_ai.go`
   - `ask/service.go` → `service_chat.go` / `service_execute.go` / `service_history.go` / `sqlguard.go`

**防回归**：加 `router_test.go`，遍历 `r.Routes()` 与 golden 列表比对（method+pattern），防止拆分时漏路由。

### 工作量与风险

- 4–6 天。
- 风险：机械搬运易漏；golden 测试 + 全量 `go test`。
- 回滚：纯文件移动，git revert 即可。

---

## 10. AR-09 单一事实源（Batch 3）

### 目标

- 项目表清单只维护一处。
- 前端类型与 Go 结构体不再手抄。
- 为后续 OpenAPI 打底。

### 方案

**1. 项目表清单（1 天）**

- Go `chatPayload` 增加 `"project_tables": ask.ProjectTables()`（从 `projectFilterColumns` 导出）。
- py-agent `_plan` 用 payload 里的列表，删除 `PROJECT_TABLES` 常量（保留 fallback 一个版本）。
- 测试：Go 单测断言列表；Python 测试缺字段时回退。

**2. TS 类型生成（3–4 天）**

- 引入 `tygo`（Go struct → TS），配置 `tygo.yaml`，生成 `web-ui/src/api/types.gen.ts`。
- 先覆盖高频模块：`ask`、`agent`、`logs`、`issues`、`projects`。
- `web-ui/src/api/*.ts` 改为 `import type { ... } from './types.gen'`。
- CI：`tygo generate && git diff --exit-code`。
- 后续再考虑完整 OpenAPI（`openapi-typescript`）。

**3. GRANT 一致性（1 天）**

- Go 集成测试：对 `mainTables` 每张表查 `has_table_privilege('ask_reader', $1, 'SELECT')`，失败即红。
- 迁移模板加注释：新增主表必须同步 GRANT。

### 工作量与风险

- 4–6 天。
- 风险：生成类型命名冲突；先小范围，逐步替换。
- 回滚：生成文件可回退，手写类型保留。

---

## 11. AR-10 CI 门禁（Batch 3）

### 目标

- 覆盖率下降会红；E2E 不再只靠手动；镜像漏洞可见；测试可并行。

### 方案

1. **Go 覆盖率门禁**：`scripts/coverage-gate.sh` 解析 `go tool cover -func` 的 total，低于阈值退出；先设 50%，每季度 +5%。
2. **Python 覆盖率**：`coverage report --fail-under=60`。
3. **E2E**：`schedule: cron '0 18 * * *'` 每晚跑全量；PR 跑 3 个核心 spec（登录/日志/AI 问答）。
4. **镜像扫描**：构建 server 镜像后 `aquasecurity/trivy-action`，先 report-only，再 gate HIGH/CRITICAL。
5. **测试隔离**：`testutil.SeedUUID(pkg, n)` 生成包级唯一 ID，替换固定 UUID；逐步把 `-p 1` 提到 `-p 4`。
6. **`go test -shuffle=on`**。

### 工作量与风险

- 3–4 天。
- 风险：并行测试暴露隐藏耦合；先修种子，再提并行度。

---

## 12. AR-11 前端减负（Batch 3）

### 目标

- 新列表页不再复制加载/空/错误/分页逻辑。
- 类型来自生成，不手抄。
- i18n 文件可维护。

### 方案

1. **`useListPage`**（2 天）：封装 `items/loading/error/page/perPage/total/refresh`，支持 abort；先迁移 `IssuesView`、`ProjectLogsView`、`ExperiencesView`、`TestDataView`、`AuditView`。
2. **生成类型**（依赖 AR-09）：替换 5 个高频 API 模块的 interface。
3. **i18n 拆分**（2 天）：`src/i18n/zh/{common,logs,issues,agent,...}.ts` + `index.ts` 聚合；`en` 同构；keys 测试遍历。
4. **业务组件测试**：AskDialog/候选卡/通知中心补 3–5 个断言（现有 E2E 兜底）。

### 工作量与风险

- 5–8 天（增量）。
- 风险：大 diff；按视图逐个 PR。

---

## 13. P3 处理策略（不展开）

| 编号 | 处理 | 时机 |
|---|---|---|
| AR-13 Caddy 32MB vs 100MiB | Caddy 改 100MB，或对 `/api/v1/attachments` 单独放宽；实测 >32MB 上传 | Batch 1 |
| AR-14 安全头 | Caddy 加 CSP/HSTS/X-Frame-Options/Referrer-Policy | Batch 1 |
| AR-15 Influx retention | `influx bucket list` 确认；设 180 天 + 降采样 | Batch 1 |
| AR-16 依赖面 | 评估 mem0ai/qdrant/boto3 是否实际使用；拆 requirements | Batch 3 |
| AR-17 候选 reconciler | 定时扫 `approved` 超 10 分钟未执行 → 重试/告警 | Batch 2 |
| AR-18 文档漂移 | 更新 AGENTS 的 25/38 为 30/44；加 CI 统计 | 随手 |

---

## 14. 验收清单（整体）

- [ ] 同 key 重试返回同响应；失败可重试（AR-03）。
- [ ] 一次问答可用一个 request_id 串起 Go/Python/LLM 日志；panic 返回 500（AR-07）。
- [ ] `config.Load()` 缺必填即失败，日志脱敏（AR-12）。
- [ ] 审计写失败有指标/告警；归档可验证；敏感读有审计（AR-02）。
- [ ] 从零恢复 DB + Influx + 附件 + 图片（AR-06）。
- [ ] 应用账号不能 DDL、不能改删审计（AR-05）。
- [ ] 两副本调度不重复、限流共享（AR-01）。
- [ ] 10 并发 ask p95 不线性恶化；双 worker 不重复消费（AR-04）。
- [ ] `main.go` < 200 行；路由 golden 测试通过（AR-08）。
- [ ] TS 类型由 Go 生成且 CI 无 diff（AR-09）。
- [ ] Go/Py 覆盖率未下降；E2E 每晚跑（AR-10）。
- [ ] 5 个列表页用 `useListPage`；i18n 按域拆分（AR-11）。

---

## 15. 待决策

1. **迁移编号**：045～049 已占用；050 起由 scan-archive 与 AR-06b/AR-05 按实际开工顺序分配，不预留固定号。
2. **DB 角色切换窗口**：需要一次停机窗口（切 `DB_USER` + 全量冒烟），是否接受。
3. **docker.sock**：走“宿主更新代理”（彻底）还是“socket proxy”（快但收窄）。
4. **审计归档**：12 个月阈值是否合适；归档数据保留多久。
5. **前端类型生成**：tygo（轻）还是 OpenAPI（重）先上。

---

## 16. 并行特性轨道：GC 扫描数据存档（scan-archive v0.4）

> 定位：独立功能，不是 AR 缺陷修复；不改变 AR-01～AR-12 的优先级，但复用迁移号、备份、审计、限流等基础设施。
> 来源：`docs/scan-archive-design-v0.4.md`（评审修订稿，**先评审不做**）。
> 本节只登记计划位置、编号、依赖与拆分；详细设计以原文档为准。

### 16.1 范围

- LabVIEW 扫描输出 `txt + png` 自动入库（gascell 宿主机 watcher 轮询 NAS 只读源）；
- 项目级 `scans` 表：多值标签、标题/描述、软作废；
- gascell `lab-uploads/scans` **单副本**；NAS 源目录只读，不归档、不复制、不删除；
- 存量约 369 对文件 backfill（先 dry-run 出 manifest）；
- 前端项目工作区 `scans` tab：列表/详情/双 y 轴曲线/说明编辑。

### 16.2 迁移编号

- 049 已被 AR-04 `pending_agent_tasks.queue_class` 占用；scans 已使用 **050**（PR1 后端骨架已落地）。
- 后续按实际落地顺序：
  - 051 `system_update_requests`（AR-06b）
  - 052 `db roles/grants`（AR-05）
  - 053+ `assistant_*`（多人 Agent 会话层）
- 实施时同步更新 `scan-archive-design-v0.4.md` 的迁移号。

### 16.3 与 AR 修复的耦合

| 依赖 | 说明 |
|---|---|
| AR-06 备份 | `backup.sh` 已整卷打包 `lab-uploads`，天然覆盖 `scans/`；`restore-check.sh` 需补 scans 行数/文件抽查 |
| AR-02 审计 | `scan.create/update/void/download` 走现有分区表；内联图不审计，显式下载才审计 |
| AR-05 角色 | 若切 `lab_app`，scans 只需 DML；ingest 仍只认 SERVICE_TOKEN |
| AR-01 限流 | 可选复用 `047_rate_limit_counters`；v1 不做 |
| AR-04 队列 | 不占用 `pending_agent_tasks`；watcher 直接 HTTP |
| SERVICE_TOKEN | `middleware/service_token.go` 白名单新增固定路径 `POST /api/v1/scans/ingest`（不做前缀匹配） |
| 前端 | 项目 tab 可达，不新增全局导航项 |

### 16.4 实施拆分（对齐设计 §14）

| 步骤 | 内容 | 预估 |
|---|---|---|
| 1 | 后端骨架：迁移 0XX、`go-server/scans/`、parser、项目 ACL、单测 | 1.5–2 周 |
| 2 | 文件/接口：存储、缩略图、上传/list/detail/patch/void/data/file、审计/幂等、handler 测试 | 1.5–2 周 |
| 3 | ingest/watcher：固定端点、SERVICE_TOKEN 白名单、Python watcher、systemd、state、dry-run | 1–1.5 周 |
| 4 | 监控/运维：心跳、告警、磁盘、备份/恢复演练 | 3–5 天 |
| 5 | 前端：tab/路由、列表/详情、曲线、i18n/statusMeta | 1.5–2 周 |
| 6 | 存量导入/上线：dry-run → 369 对导入 → 对账 → E2E → 文档收口 | 3–5 天 |

依赖顺序：1 → 2 →（3/5 可并行）→ 4 → 6；总约 6–8 周（单人）。

### 16.5 排期建议

- 不插队当前 Batch 1/2；建议在 **AR-04 完成后**启动 PR1–2（后端+接口），watcher/前端并行。
- 若 AR-05 切 `lab_app` 与 scan 同期，**先切角色再跑 scan 冒烟**，避免双重变量。
- AR-06b（docker.sock）与 scan 无依赖，可另行决策。

### 16.6 风险

- **单副本**：gascell 磁盘是唯一系统副本；必须确认 AR-06 备份打包 `lab-uploads/scans` 且异地可用。
- **NAS 只读**：watcher 不写源目录；部署时用只读挂载验证。
- **迁移号冲突**：050 可能被 AR-06b 先用；按实际最大号分配，不预留。
- **SERVICE_TOKEN 授权面**：只加固定路径，不做前缀/正则匹配。
- **XSS**：说明字段前端渲染必须转义，禁止 `v-html`。
- **存量 backfill**：先 dry-run 对账，再正式导入；缺 png/legacy3 单独记录。

### 16.7 验收

直接采用 `scan-archive-design-v0.4.md` §15 的 11 条验收；上线前额外确认：

- `restore-check.sh` 覆盖 `scans` 行数与文件抽查；
- `scripts/test-all.sh` 覆盖 050 up/down；
- 单副本/多副本行为与 AR-01 一致（scan 本身不依赖多副本）。

### 16.8 实施状态（2026-11-25）

| 步骤 | 状态 | 已落地 | 剩余 |
|---|---|---|---|
| PR1 后端骨架 | ✅ | 迁移 050 `scans`；`go-server/scans/`（parser、repository、service、handler）；列表/详情/tags/说明/作废路由；parser + service 单测；050 up/down 已验证 | — |
| PR2 上传/文件 | ✅ | `POST /projects/{id}/scans` multipart；txt/png 大小与 magic 校验；sha/去重/迟到 png 补挂；`lab-uploads/scans` 单副本存储 + 320px 缩略图；`file/txt|png`、`thumb` 下载（ETag/Range/内联/下载审计） | 孤儿目录清理任务（设计 §5.1 的可选清理） |
| PR3 ingest/watcher | ✅ | `POST /api/v1/scans/ingest`（仅 SERVICE_TOKEN，白名单精确路径）；`Service.Ingest` 无用户 ACL；`deploy/scan-ingest/` watcher（轮询/稳定判定/png grace/state/心跳/退避/告警）、systemd unit、dry-run/backfill | — |
| PR4 监控/运维 | ✅ | watcher 心跳 + 坏文件告警；`watchdog.sh` 接入 scan-ingest 心跳/磁盘 80% 告警；`restore-check.sh` 校验 scans 行数与附件包 `scans/` 条目 | 实际生产部署后按 369 对导入再跑一次恢复演练 |
| PR5 前端 | ⏳ | — | 项目 tab、列表/详情、曲线、i18n/statusMeta |
| PR6 存量导入 | 🟡 | `--backfill --dry-run` + 正式 backfill 已实现（限速 5 req/s） | 实际导入 369 对、对账、缺 png/legacy3 清单 |

> 说明：PR2 已完成上传与文件读取；`/scans/ingest`、watcher、前端、存量导入仍待后续 PR。`scan.create/download` 审计已接入；内联图不审计。
