# 架构评审：现状、缺陷与修复优先级

> 版本：v1.0（2026-11-24）
> 状态：已完成静态评审，待团队确认
> 范围：`go-server/`、`py-agent/`、`web-ui/`、`migrations/`、`deploy/`、`.github/workflows/`
> 方法：代码走读 + 配置/CI 检查 + 关键路径 grep 取证；**未做压力测试与渗透测试**，性能结论均为结构性推断
> 行号基于 2026-11-24 的 `main` 分支；代码变动后以搜索关键词为准

---

## 0. 结论摘要

1. **工程完成度高、架构纪律强**：模块化单体、构造期注入、审计 hash 链、source gate、迁移/备份/更新脚本、测试量（测试代码 ≈ 源码）都明显高于同类实验室项目。
2. **它不是“多人 agent 平台”的架构**：单实例状态、进程内限流/调度/SSE、全局审计锁、单进程 Python 服务，决定了它只能支撑“少量人 + 低频 AI”。
3. **有三处“看起来有、实际是半成品”**：幂等只做重复拒绝不做重放；审计异步写失败只记日志；DB 层 append-only 与模块隔离没有强制。
4. **最该先修的 5 件事**（约 2 周）：幂等语义、HTTP 超时/panic 恢复/请求 ID 透传、备份补全与异地、审计分区与失败告警、配置集中化 + Caddy 对齐。
5. **顺序建议**：先做上述加固，再做多人 Agent 的 S1 会话层（见 `docs/multi-user-agent-upgrade.md`）；否则会话功能会被运行时卡住。

### 评级（相对“10 人实验室 + 低频 AI”的基准）

| 维度 | 评级 | 依据 |
|---|---|---|
| 模块化 | B+ | 目录与分层约定清晰、注入桥接规范；`main.go` 是 843 行上帝文件，例外清单在增长 |
| 权限/安全 | A- | 认证、项目 ACL、source gate、service token 白名单扎实；DB 权限模型未落地、幂等有洞 |
| 数据一致性 | B | 事务、审计链、候选 fence 有设计；审计可丢、幂等非真幂等 |
| 可扩展性 | C | 单实例、进程内状态、全局锁，横向扩不了 |
| 可观测性 | C- | 有 metrics/告警；无 tracing、`request_id` 不过 Python、GET 默认不审计 |
| 可运维性 | B | 迁移/备份/更新/watchdog 齐全；备份不完整、单机单点 |
| 前端 | B+ | 组件分层、i18n 校验、覆盖率门禁；30 视图、类型手写 |
| 测试 | B+ | 测试代码 1.31MB vs 源码 1.27MB；Go/Py 覆盖率不设门禁、E2E 手动 |
| 文档 | A- | 16 册设计文档 + `AGENTS.md`；存在版本漂移 |

---

## 1. 架构速览

```
浏览器/PWA ──HTTPS──> Caddy(VPS) ──frp/EasyTier──> Go server :8000
                                                    ├─ PostgreSQL 16
                                                    ├─ py-agent-interpret :8001（LLM 调用）
                                                    ├─ py-agent worker（队列消费）
                                                    ├─ InfluxDB（传感器/电气）
                                                    ├─ Grafana + Prometheus
                                                    ├─ ntfy
                                                    └─ EPICS IOC / epics-gateway
```

| 指标 | 数值 | 来源 |
|---|---|---|
| Go 业务模块 | 20+ | `go-server/` 目录 |
| 迁移版本 | 44 | `migrations/*.up.sql` |
| `main.go` 行数 / 路由注册调用 | 843 / 202 | 静态统计 |
| Compose 服务 | 10 | `deploy/docker-compose.yml` |
| 前端视图 / 业务 API 模块 | 30 / 20（另有 `client.ts`） | `web-ui/src/views`、`web-ui/src/api` |
| 测试/源码体积比 | 1.03 | `*_test.go` vs 非测试 `.go` |
| 大文件 | `system/updater.go` 48KB、`logs/service.go` 36KB、`ask/service.go` 36KB | 静态统计 |

**评价**：这是“模块化单体 + 旁路 AI 服务 + 单机 Compose”的合理形态。对实验室规模，它比微服务、消息队列、K8s 更合适。问题不在“选型”，而在“为增长预留的扩展点不足”。

---

## 2. 做得好的地方（不要改）

1. **模块化单体 + 构造期注入**
   - 跨模块只走 HTTP 或 `main.go` 注入的窄接口；agent 的 candidate/trace 全靠 `SetExecutor`/`SetReportReader`/`SetAuditReader` 桥接，自身不 `SELECT` 他模块表。
   - 例外登记（AGENTS.md §5）有明确白名单和约束，不是“随手 join”。
2. **安全默认**
   - `SourceGate` 状态机区分 Caddy/内网/公网，Caddy 先删后设 XFF；service token 白名单（`middleware/service_token.go`）；agent 委托必须匹配 `processing` 任务行；登录 5 次锁定 + IP 限流；Argon2id；HttpOnly + CSRF。
3. **审计链**
   - 迁移 029 的 hash 链 + 触发器；`insertAuditLog` 把 advisory lock 前置到 INSERT 事务，保证 id 序 == 链序（`middleware/audit.go:145-162`）。
   - 候选 trace 能串到执行产物（迁移 030）。
4. **测试与 CI**
   - 迁移 up/down、Windows 交叉编译、govulncheck/pip-audit/npm audit、前端覆盖率门禁；测试代码量与源码相当。
5. **前端工程化**
   - base/business 分层、i18n key 双向比对、设计令牌、深色模式、窄口径覆盖率门禁。
6. **运维脚本**
   - 备份、恢复演练、watchdog、更新 runner，都有失败告警和 flock 防重入。

---

## 3. 缺陷登记

> 编号用于后续 issue/PR 引用。P1 = 影响多人 Agent 或数据安全；P2 = 拖慢迭代；P3 = 小问题/待确认。

### P1

#### AR-01 单实例状态，无 leader election

- **现象**：限流、SSE 会话、调度器、更新会话都在进程内。起第二个副本会重复跑调度、限流失效、SSE 断流；不起副本则所有长请求挤在一个进程。
- **证据**：
  - 限流：`go-server/ask/service.go:943`（`allowOne`，进程内 map）；`go-server/steptemplates/service.go`、`logs` 的 AI 解析同模式。
  - 更新会话：`go-server/system/service.go:40-41`（`active` + `sessions map`），仅靠磁盘 marker 做重启恢复。
  - 调度器：`go-server/main.go:776`（todos）、`main.go:786`（weekly）、`main.go:792`（ask retention）、`main.go:794`（alert maintenance），均为进程内 goroutine，无 advisory lock / leader 选举。
  - HTTP server：`main.go:796` 只有 `Addr` + `Handler`，无超时。
- **影响**：多人 Agent 的“并发会话 + 配额”直接不可用；副本数只能为 1。
- **建议**：
  - 短期：明确“单副本”约束并写进部署文档；调度器加 `pg_try_advisory_lock` 防重复；限流改 `assistant_usage`/Redis 计数。
  - 中期：把会话/运行状态全放 Postgres（已在 `multi-user-agent-upgrade.md` 设计），进程只做无状态计算；SSE 用“DB + 事件轮询/通知”恢复。
- **验收**：起 2 个 server 副本，调度任务不重复、限流仍生效、SSE 重连不丢最终答案。

#### AR-02 审计：全局串行、异步可丢、无归档、GET 默认不审计

- **现象**：每条审计写在 DB 层取 `pg_advisory_xact_lock(714001)`；响应后才写，5s 超时失败只 `slog.Error`；`audit_log` 无分区/归档；GET 默认不审计。
- **证据**：
  - 全局锁：`middleware/audit.go:162`。
  - 失败只记日志：`middleware/audit.go:118`。
  - GET 跳过：`middleware/audit.go:65`。
  - 无分区：`migrations/` 无 `PARTITION`（仅 043 窗口函数用了 `PARTITION BY`）。
- **影响**：
  - 所有挂 Audit 的写请求在 DB 层串行；实验室规模够用，但“每个工具调用都审计”的多人 Agent 会把它变成吞吐上限。
  - 业务写成功、审计写失败时，审计链出现缺口（合规风险）。
  - 读操作无审计，敏感数据“谁看过”不可追溯。
- **建议**：
  - 审计写失败 → 告警 + `lab_audit_write_failed_total` 指标，不能只 log。
  - `audit_log` 按月分区 + 归档（`pg_partman` 或迁移脚本 + 定时任务）；`archive` 保留 hash 链校验。
  - 敏感读端点（日报详情、附件下载、ask）显式 `SetAuditAction`；其余 GET 保持不审计以控制量。
  - 高并发阶段再考虑“审计 outbox + 单写者”，不要现在上消息队列。
- **验收**：并发 50 写请求时 p95 不因审计锁显著恶化；审计失败有告警；分区表可滚动归档。

#### AR-03 “幂等”是重复拒绝，不是幂等重放

- **现象**：`idempotency_keys` 只存 `key + request_id`；迁移 023 的 `response_status/response_body` 从未写入；key 全局唯一、不按 user/path 作用域、无 TTL；请求失败后 key 已被消费。
- **证据**：
  - 只 INSERT key：`middleware/idempotency.go:33`。
  - 重复返回 409 + 他人 `existing_request_id`：`middleware/idempotency.go:55-56`。
  - 迁移字段闲置：`migrations/023_idempotency_keys.up.sql:4-5`，全仓库无 `response_status` 引用。
- **影响**：
  - 网络失败后客户端用同 key 重试 → 409，无法恢复（与“幂等”语义相反）。
  - 不同用户/端点撞 key 会拿到别人的 `request_id`（低概率但设计上不该发生）。
  - 表无限增长。
- **建议**：
  - 作用域改为 `(user_id, method, path, key)` 唯一。
  - 成功后写 `response_status/response_body`，重复请求直接重放；失败则删除 key 允许重试。
  - 增加 TTL 清理任务（如 7 天），并加索引。
- **验收**：同 key 重试返回同一响应；失败后重试可成功；跨用户同 key 不互相影响；表行数稳定。

#### AR-04 Agent 运行时是“单进程批处理 ETL”

- **现象**：worker 一次一个任务；`serve.py` 单 uvicorn；`AskEngine._client_lock` 串行化所有 `/ask/execute`；LLM 同步调用占线程 60–360s；模型侧 `tools=[]`，没有工具循环。
- **证据**：
  - worker 串行：`py-agent/worker.py:135-136`（`run_once` 单次 claim）、`:226`（`run_forever`）。
  - 单进程：`py-agent/serve.py:360`（`uvicorn.run` 无 workers）。
  - 全局锁：`py-agent/tools/ask.py:80`、`:215`（`with self._client_lock`）。
  - 无工具循环：`py-agent/tools/parse.py:52`（`LightAgent(tools=[])`）、`:59`（清空 `ToolRegistry`）。
- **影响**：多人同时提问会排队；一个 60s 的 ask 会阻塞其他人的 execute；这不是模型慢，是架构只允许一个请求在跑。
- **建议**：
  - 短期：按用户/会话加信号量；`AskEngine` 改每线程 client（或连接池），去掉全局锁。
  - 中期：交互式与批处理分队列/分 worker；会话存 Postgres；工具循环按 `spike.py` 的 guardrail 模式生产化。
  - 详见 `docs/multi-user-agent-upgrade.md` §7（S2/S4）。
- **验收**：10 个并发 ask 的 p95 不随人数线性恶化；一个用户的长任务不阻塞另一个用户。

#### AR-05 DB 权限模型未落地

- **现象**：应用用 `lab`（迁移执行者/owner）连库，拥有全库 DDL/DML；只有 `ask_reader` 一个受限角色；“模块不跨表”靠 code review；`audit_log` 的 append-only 没有 `REVOKE UPDATE/DELETE`；033 的 GRANT 白名单手工维护。
- **证据**：
  - 连接使用 `lab`：`go-server/common/db.go`（`DB_USER` 默认 `lab`）。
  - 唯一受限角色：`migrations/033_ask_history.up.sql:5-7`。
  - 白名单漂移只 warn：`go-server/ask/service.go:159-169`（`StartupCheck` 打日志）。
  - 无 `REVOKE`：`migrations/` 中无针对 `audit_log` 的权限回收。
- **影响**：任何 SQL 注入/越权代码都能读写全库；审计“不可篡改”只是约定；新表忘记 GRANT 会让 ask 功能静默降级。
- **建议**：
  - 拆角色：`lab_app`（DML only，无 DDL）、`lab_migrate`（DDL）、`ask_reader`（只读白名单）。
  - `REVOKE UPDATE, DELETE ON audit_log FROM lab_app`；触发器再兜底。
  - 把 `ask_reader` 白名单做成迁移模板/CI 检查，漏 GRANT 直接失败。
- **验收**：应用账号无法 `DROP/ALTER`；无法改删审计行；新表迁移 CI 自动校验 GRANT。

#### AR-06 部署单点与备份不完整

- **现象**：10 个容器一台机器；备份只有 `pg_dump` 到本地 `/opt/lab-backups`，保留 14 天；InfluxDB、附件 volume、`images/` 未备份；无异地；`server` 挂载 `docker.sock`。
- **证据**：
  - 仅 pg_dump：`deploy/scripts/backup.sh:128-130`；保留 14 天：`:54`、`:140`。
  - 无 Influx/附件：脚本中无相关命令（grep `influx|upload|images` 无命中）。
  - docker.sock：`deploy/docker-compose.yml:263`。
- **影响**：磁盘/主机故障 = 全损；传感器历史、附件不可恢复；server 被攻破可控制宿主 Docker。
- **建议**：
  - 备份补 InfluxDB（`influx backup`）、`lab-uploads` volume、`images/`；用 restic/rclone 推异地。
  - 恢复演练覆盖 Influx + 附件。
  - 更新 runner 与 server 解耦：server 只写“更新请求”表，由独立 runner（不挂 server 容器）消费；或使用 docker socket proxy 限制 API。
- **验收**：从零恢复演练通过（DB + Influx + 附件）；异地副本可拉取；server 容器无 `docker.sock`。

#### AR-07 观测断链

- **现象**：`request_id` 不传给 py-agent；无 OTel 埋点；无统一 panic recovery；`http.Server` 无超时；GET 默认不审计（见 AR-02）。
- **证据**：
  - Go 出站请求未带 `X-Request-Id`（全仓库仅 CORS 白名单与测试使用）。
  - 无 `middleware.Recoverer`：`main.go` 未注册；仅有 `instruments` 内部两处局部 `recover()`。
  - 无超时：`main.go:796`。
- **影响**：一次问答在 Python 侧无法关联到 Go 的 request_id；handler panic 表现为连接断开、无统一 500；慢连接占用资源。
- **建议**：
  - Go → py-agent 所有调用带 `X-Request-Id`，py-agent 日志输出该字段。
  - 注册 `middleware.Recoverer`（chi 自带）+ 统一 500 envelope + 指标。
  - `http.Server` 设 `ReadHeaderTimeout: 5s`、`IdleTimeout: 120s`；SSE 路由单独处理 `WriteTimeout`。
  - 关键链路加 OTel span（Go HTTP → py-agent → LLM），至少先做日志关联。
- **验收**：任一问答可用一个 ID 串起 Go 审计、Python 日志、模型调用；panic 返回 500 而非断连；慢连接不堆积。

### P2

#### AR-08 上帝文件与模块装配耦合

- **现象**：`main.go` 843 行、202 处路由/挂载；`system/updater.go` 48KB；`logs/service.go`、`ask/service.go` 各 36KB。
- **影响**：加模块要改 `main.go` 多处，冲突面大；单文件认知负担高，review 容易漏。
- **建议**：把 `main.go` 拆成 `wiring.go`（构造/注入）+ `routes_*.go`（按域注册）+ `schedulers.go`；每个模块提供 `Register(r chi.Router, deps)`。大 service 按用例拆文件（不改包边界）。

#### AR-09 重复事实源

- **现象**：项目表清单在 Go `projectFilterColumns`（`ask/service.go:50`）与 Python `PROJECT_TABLES`（`ask.py:48`）两处；`ask_reader` GRANT 与 `mainTables` 手工同步；Go 响应与 20 个业务 API 模块全手写，无 OpenAPI。
- **影响**：新增表/字段要改 2–3 处，漏改导致 LLM 生成错误 SQL 或类型漂移。
- **建议**：Go 暴露 schema 元数据端点，py-agent 启动时拉取；从 Go 结构体生成 OpenAPI，再生成 TS 类型；CI 加“文档/代码一致性”检查。

#### AR-10 CI 门禁偏软、测试隔离弱

- **现象**：Go/Python 覆盖率只报告不阻塞；E2E 仅 `workflow_dispatch`；依赖扫描 report-only；无镜像扫描/SBOM；测试需 `-p 1` 串行（固定 UUID 种子撞主键）。
- **影响**：回归靠人盯；覆盖率下降无感知；CI 慢；测试间耦合。
- **建议**：Go/Py 设最低覆盖率阈值；E2E 每晚跑；Trivy 扫镜像；测试用独立 schema/事务回滚替代全局种子。

#### AR-11 前端规模压力

- **现象**：30 视图、20 个业务 API 模块、i18n 各 60KB+；业务状态全在页面本地，跨页复用靠复制。
- **影响**：新增页面重复实现列表/加载/错误态；类型与后端漂移。
- **建议**：生成 API 类型；抽 `useListPage` 组合式函数；i18n 按域拆文件（保持 key 校验）；路由级分包已具备。

#### AR-12 配置散读与默认值漂移

- **现象**：全仓库 100+ 处 `os.Getenv`（含测试）、`main.go` 83 处装配调用；无集中 config struct 与启动校验；SOURCE_GATE 默认值曾出现代码与 compose 不一致（compose 注释自述）。
- **影响**：漏配环境变量导致静默降级或安全开关失效。
- **建议**：`internal/config` 集中解析 + 校验 + 打印生效值（脱敏）；`.env.example` 由代码生成；启动时 fail-fast。

### P3（小问题/待确认）

| 编号 | 问题 | 证据 | 建议 |
|---|---|---|---|
| AR-13 | Caddy `request_body 32MB` vs 附件 100MiB | `deploy/caddy/Caddyfile:19`；`go-server/attachments/handler.go:16` | 统一为 100MB 或按路径放宽；实测公网大附件 |
| AR-14 | 缺安全响应头 | Caddyfile 无 `header` 指令 | 加 CSP/HSTS/X-Frame-Options/Referrer-Policy |
| AR-15 | InfluxDB retention 未声明（待确认） | `deploy/docker-compose.yml:74` 仅 setup，无 retention | 确认 bucket 保留策略；设 90–180 天 + 降采样 |
| AR-16 | Python 依赖面过大 | `py-agent/requirements.txt` 含 mem0ai/qdrant/boto3/langfuse/posthog | 评估实际使用；拆 requirements 分层，减小镜像与供应链风险 |
| AR-17 | 候选执行 fence 无 reconciler | `go-server/agent/service.go:151` 注释自述 | 加定时对账：`approved` 超时未执行 → 重试/告警 |
| AR-18 | 文档漂移 | `AGENTS.md:35`（25 视图）、`:37`（38 迁移）vs 实际 30/44 | 更新 AGENTS.md；或加 CI 检查 |

---

## 4. 合理取舍（不要为了“架构洁癖”改）

| 取舍 | 判断 | 理由 |
|---|---|---|
| 模块化单体，不上微服务 | ✅ 正确 | 10 人实验室，单机 Compose 足够；微服务只会增加运维成本 |
| DB 队列，不上 Kafka/RabbitMQ | ✅ 正确 | 任务量低；5s 轮询延迟可接受；`SKIP LOCKED` 已正确使用 |
| 审计 advisory lock 串行 | ✅ 低量下正确 | 简单、保证链序；问题只是没给高并发留路，不是“错” |
| 同步 LLM 调用 | ✅ 批处理正确 | `worker` 场景简单可靠；交互式才需要流式/异步 |
| 本地备份 | ✅ 有比没有好 | 缺的是异地与全量，不是“该上云” |
| 手写 SQL 解析器（ask） | ⚠️ 可接受但需盯 | 有 DB 角色 + 单表限制 + 测试；属登记过的全库只读例外，新增能力要重新评估 |

---

## 5. 修复路线

### Sprint 0（约 2 周，优先做）

| 顺序 | 事项 | 对应 | 产出 |
|---|---|---|---|
| 1 | 幂等语义修正（作用域/TTL/响应重放） | AR-03 | 迁移 + middleware 改造 + 测试 |
| 2 | HTTP 超时 + Recoverer + `X-Request-Id` 透传 | AR-07 | 中间件 + py-agent 日志字段 |
| 3 | 备份补全（Influx/附件/异地）+ 恢复演练 | AR-06 | 脚本 + 演练记录 |
| 4 | 审计失败告警 + 分区方案 | AR-02 | 指标/告警 + 迁移草案 |
| 5 | Caddy 上传限制对齐 + 安全头 + 配置集中化 | AR-12/13/14 | Caddyfile + config 包 |

### 与多人 Agent 的依赖关系

```
Sprint 0（加固）
   ├─ AR-03 幂等 ──────────┐
   ├─ AR-07 观测 ──────────┤
   ├─ AR-02 审计 ──────────┼─> S1 会话层（multi-user-agent-upgrade.md）
   └─ AR-06 备份 ──────────┘
S2 工具层依赖 AR-05（工具调用要按角色授权，DB 角色是纵深）
S4 运行层依赖 AR-01/AR-04（多副本、队列分池）
```

### 不建议现在做的

- 不要为了 AR-01 立刻上 K8s/消息队列；先做“单副本约束 + 调度锁 + 状态外置”。
- 不要为了 AR-02 上 Kafka outbox；先做分区 + 失败告警。
- 不要为了 AR-09 重写 API 层；先生成 OpenAPI/TS 类型。

---

## 6. 待确认（评审中无法静态判定）

1. **InfluxDB retention**：compose 未声明，可能由 UI/CLI 另设；需 `influx bucket list` 确认。
2. **Caddy 是否另有全局 header 配置**：仓库内 Caddyfile 未见，VPS 上可能有额外片段。
3. **>32MB 附件经公网是否确实被拒**：需实测 Caddy `request_body` 行为。
4. **多副本是否在路线图内**：决定 AR-01 是 P1 还是“记录在案”。
5. **`lab` 是否即表 owner**：需 `\du lab` 与 `\dt` 属主确认，再决定拆角色方案。

---

## 7. 证据索引

| 结论 | 证据位置 |
|---|---|
| 单进程限流 | `go-server/ask/service.go:943` |
| 更新会话在内存 | `go-server/system/service.go:40-41` |
| 进程内调度器 | `go-server/main.go:776,786,792,794` |
| HTTP server 无超时 | `go-server/main.go:796` |
| 审计全局锁 | `go-server/middleware/audit.go:162` |
| 审计失败只 log | `go-server/middleware/audit.go:118` |
| GET 默认不审计 | `go-server/middleware/audit.go:65` |
| 幂等只插 key | `go-server/middleware/idempotency.go:33` |
| 幂等 409 + 他人 request_id | `go-server/middleware/idempotency.go:55-56` |
| 幂等响应字段闲置 | `migrations/023_idempotency_keys.up.sql:4-5` |
| worker 串行 | `py-agent/worker.py:135-136,226` |
| uvicorn 单进程 | `py-agent/serve.py:360` |
| AskEngine 全局锁 | `py-agent/tools/ask.py:80,215` |
| 模型无工具循环 | `py-agent/tools/parse.py:52,59` |
| 应用账号 lab | `go-server/common/db.go`（`DB_USER` 默认 lab） |
| 唯一受限角色 | `migrations/033_ask_history.up.sql:5-7` |
| 白名单漂移只 warn | `go-server/ask/service.go:159-169` |
| 备份仅 pg_dump | `deploy/scripts/backup.sh:128-130` |
| 备份本地 14 天 | `deploy/scripts/backup.sh:48,54,140` |
| docker.sock | `deploy/docker-compose.yml:263` |
| 附件 100MiB | `go-server/attachments/handler.go:16` |
| Caddy 32MB | `deploy/caddy/Caddyfile:19` |
| 文档漂移 | `AGENTS.md:35,37` |
| 测试/源码比 | 1.31MB / 1.27MB（静态统计） |
| 路由/行数 | `main.go` 843 行、202 处注册 |

---

## 8. 评审边界

- 未执行：压力测试、渗透测试、故障注入、真实多副本验证。
- 未覆盖：`deploy/` 下 frp/EasyTier 的实际网络配置、VPS 侧 Caddy 全局配置、生产数据库实际权限与索引使用率。
- 评级是“相对实验室规模”的判断，不是绝对质量分；同一架构放到 100 人/高并发场景会显著更低。
- 下一步：Sprint 0 的 5 项可拆成独立 PR；多人 Agent 的会话/工具/配额设计见 `docs/multi-user-agent-upgrade.md`。
