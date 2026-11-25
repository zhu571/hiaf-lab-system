# GC 扫描数据自动存档 + 说明标注 —— 设计方案（v0.4）

> 状态：**评审修订稿，先评审不做**
> 取代：`scan-archive-design-v0.3.md`（2026-11-25 第二轮修订）
> 计划位置：`docs/architecture-remediation-plan.md` §16「并行特性轨道：GC 扫描存档」
> 迁移编号：**050（PR1–PR4 已落地）**——049 已被 AR-04 `queue_class` 占用
> 决策记录（2026-11-25）：① 项目级；② 多值标签；③ 内容相同但文件名不同各存一条；④ **单副本**：gascell 存一份；NAS 原始目录只读不动，不归档、不复制、不删除
> 关联：`docs/test-data-curves-design.md`、`docs/api-contract.md`、`docs/permission-audit.md`、`docs/maintenance-strategy.md`、`docs/architecture-remediation-plan.md`

---

## 0. 版本变更摘要（v0.2 → v0.3 → v0.4）

| 主题 | v0.2 | v0.4 |
|---|---|---|
| 数据归属 | 全局 `scans` 表 | `project_id NOT NULL`，复用项目 ACL |
| 标签 | 单值 `tag` | 多值 `tags TEXT[]`，GIN 索引，支持 any/all 过滤 |
| 去重 | 按 sha256 去重 | 唯一键 `(project_id, machine, filename, txt_sha256)`；内容相同但文件名不同各存一条 |
| 文件存储 | 只存 lab-uploads | gascell `lab-uploads` **一份**；NAS 源目录只读，不归档、不复制、不删除 |
| 写接口 | 未提审计/幂等 | 全部 `Idempotency-Key` + 审计；作废不硬删 |
| 自动入库 | `POST /scans`（动态项目路径） | 固定 `POST /api/v1/scans/ingest`，只允许 SERVICE_TOKEN |
| 解析 | 详情页按需解析 | 上传时识别 `format`/`columns`/`row_count`；解析器泛型化，不在 Go 里写死 7 列 |
| 迁移 | 044 | **050**（049 已被 AR-04 占用；PR1 已落） |
| 前端 | 新页面 | 项目工作区 `scans` tab + 列表/详情 + 双 y 轴曲线 + i18n/statusMeta |
| 运维 | 未提 | watcher 心跳/重试、备份/恢复/回滚、存量导入 dry-run |

**v0.4 相对 v0.3 的变更（2026-11-25 第二轮）**：取消 NAS 归档副本、`nas_*` 字段、NAS 同步 worker、server 端 NAS 挂载与 `SCAN_NAS_ARCHIVE_ROOT`。NAS 只作为 watcher 的只读来源，原始目录里的数据不动；系统只维护 gascell 一份副本。

另外修正 v0.2 的几处事实问题：

- `H:\DJ\GC\test\` 是 Windows 盘符，gascell 是 Linux，监听路径必须是可配置的挂载点（如 `/mnt/mtof/DJ/GC/test`）；
- 文件名是**秒级** `hhmmss`，不是毫秒；同一秒同一 tag 会覆盖，解析器要接受这种现实；
- “复用 attachments 的文件存储方式”有歧义：attachments 的 `entity_type` 白名单里没有 `scan`，且跨模块不能直写 attachments 表。v0.4 明确为**复用存储模式，不复用 attachments 表**（见 §5）；
- 内联图/文件读取不强制 `Idempotency-Key`，避免 `<img src>` 无法加载；内联读不逐条写审计，显式下载才审计。

---

## 1. 背景与目标

LabVIEW VI `scan singl rf pA Vpp ui.vi`（`G:\Lv24\mtofg\E11\gascellDC.llb`）每次扫描输出一对文件到 NAS 共享：

- `gc<yymmdd>-<hhmmss>-<tag>.txt`：带表头的 tab 分隔数据；
- 同名 `.png`：该次扫描的图。

目前已积累约 369 个 txt + 367 个 png。现状问题是“文件在、语义丢了”：过几周说不清每个扫描的条件、目的和结论，检索和回溯都靠文件名。

**目标**

1. 扫描结束后自动把 txt + png 收进 lab-system，不需要人工拷贝；
2. 每次扫描可写：标题 + 自由描述（条件/参数/结论）+ 多值标签；
3. 网页可按标签、时间、说明关键词检索，能看图、看原始数据、看解析曲线，随时改说明；
4. 原件长期保存（gascell 一份系统副本 + NAS 原始目录保留），可下载；
5. 存量 369 对历史文件一次性导入，可 dry-run、可重跑。

**非目标（v1 不做）**

- 不做实时数据分析、不做自动结论/告警；
- 不把 txt 拆成逐点结构化大表；
- 不自动生成 `test_data_curves`（只预留未来“从 scan 提取曲线”的入口）；
- 不做多监听目录（配置结构预留，但 v1 只跑一个目录、一个项目）；
- 不做扫描文件版本管理/覆盖历史（NAS 覆盖由源端负责，本系统只记录收到的每一份）。

---

## 2. 总体架构

```text
LabVIEW VI ──写──> NAS 源目录
                   /mnt/mtof/DJ/GC/test/          （源件，本系统不删不改）
                            │
                            │ 轮询（SMB 上不用 inotify）
                            ▼
                 lab-scan-ingest（gascell 宿主机，systemd）
                            │ POST /api/v1/scans/ingest
                            │ Bearer SERVICE_TOKEN + Idempotency-Key
                            ▼
                 Go server（scans 模块）──> PostgreSQL `scans`
                            │
                            ▼
                 gascell lab-uploads/scans
                 （系统唯一副本，随 DB 备份）
                            │
                            ▼
                 Web：项目 tab「扫描存档」
```

- server 与 watcher 都在 gascell（10.144.144.12）上，watcher 走 `127.0.0.1:8000`；
- gascell 副本是**系统唯一副本**：网页读取、缩略图、下载都走它，不依赖 NAS 可用；
- NAS 源目录是**只读源**：watcher 只读，不写、不复制回、不归档、不删除；源件由 LabVIEW/源端自行管理；
- DB 只在 gascell（每日 `pg_dump` 备份）；NAS 不存说明文字，说明的持久性依赖 DB 备份。

---

## 3. 数据模型（迁移 050）

```sql
CREATE TABLE scans (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    machine           VARCHAR(64) NOT NULL DEFAULT '',
    filename          VARCHAR(256) NOT NULL,
    source_tag        VARCHAR(32) NOT NULL DEFAULT '',
    tags              TEXT[] NOT NULL DEFAULT '{}'
                      CHECK (cardinality(tags) <= 10),
    scan_at           TIMESTAMPTZ,
    scan_at_source    VARCHAR(16) NOT NULL DEFAULT 'filename'
                      CHECK (scan_at_source IN ('filename','mtime','upload')),
    format            VARCHAR(16) NOT NULL DEFAULT 'unknown'
                      CHECK (format IN ('legacy3','v7','v11','v12','unknown')),
    columns           JSONB NOT NULL DEFAULT '[]'::jsonb
                      CHECK (jsonb_typeof(columns) = 'array'),
    row_count         INTEGER NOT NULL DEFAULT 0 CHECK (row_count >= 0),
    bad_rows          INTEGER NOT NULL DEFAULT 0 CHECK (bad_rows >= 0),
    parse_warning     TEXT NOT NULL DEFAULT '',
    txt_sha256        VARCHAR(64) NOT NULL,
    png_sha256        VARCHAR(64),
    txt_size          BIGINT NOT NULL CHECK (txt_size > 0),
    png_size          BIGINT CHECK (png_size IS NULL OR png_size > 0),
    txt_path          TEXT NOT NULL,
    png_path          TEXT,
    thumb_path        TEXT,
    source_path       TEXT NOT NULL DEFAULT '',
    title             VARCHAR(200) NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    annotated_at      TIMESTAMPTZ,
    ingest_source     VARCHAR(16) NOT NULL DEFAULT 'watcher'
                      CHECK (ingest_source IN ('watcher','manual','backfill')),
    is_void           BOOLEAN NOT NULL DEFAULT FALSE,
    voided_at         TIMESTAMPTZ,
    voided_by         UUID REFERENCES users(id) ON DELETE SET NULL,
    void_reason       TEXT,
    uploaded_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT scans_unique_file
        UNIQUE (project_id, machine, filename, txt_sha256)
);

CREATE INDEX idx_scans_project_time
    ON scans (project_id, scan_at DESC NULLS LAST);
CREATE INDEX idx_scans_project_created
    ON scans (project_id, created_at DESC);
CREATE INDEX idx_scans_tags
    ON scans USING GIN (tags);
CREATE INDEX idx_scans_pending
    ON scans (project_id, created_at DESC)
    WHERE annotated_at IS NULL AND is_void = FALSE;
```

字段约定：

- `machine`：扫描台/来源机标识（配置项 `SCAN_MACHINE`，如 `gascell` 或 `gc-labview`），用于多机预留和唯一键；
- `filename`：原始文件名（只展示/下载用，**不参与落盘路径**）；
- `source_tag`：从文件名解析出的原始标签，保留不改；`tags` 是用户可编辑的集合，入库时用 `source_tag` 初始化；
- `tags`：去空白、去重、ASCII 转小写（中文保留），单值 1–32 字符，最多 10 个；过滤支持 `&&`（any）和 `@>`（all）；
- `scan_at`：优先文件名，其次文件 mtime（watcher 上报），最后上传时间；`scan_at_source` 记录来源；
- `format`：`legacy3`（3 列无表头）、`v7`、`v11`、`v12`、`unknown`；历史 3 列只存档，不作图；
- `columns`：表头原文数组（trim 后），字段顺序保留；legacy/unknown 为空数组；
- `row_count`/`bad_rows`/`parse_warning`：上传时轻量解析的结果，详情页不必重新发现“这文件坏了”；
- **不存逐点数据**：详情页按需解析（§7），避免大表；
- `txt_path`/`png_path`/`thumb_path`：相对 uploads 根目录的路径；
- `source_path`：NAS 源文件路径（watcher 上报，人工上传为空），便于回溯；系统不写该路径；
- `ingest_source`：`watcher` / `manual` / `backfill`，用于区分“系统收的”和“人补的”；
- `uploaded_by`：人工上传时记用户；系统入库为 NULL，审计 actor 落 `system`；
- `annotated_at`：说明状态，不靠 `description` 是否为空判断；保存标题/描述时置位，二者都被清空时置 NULL（重新进入“待说明”）；
- 软作废四件套对齐 `rf_matching` / `test_data_curves`，**不硬删**；
- 业务表必含 `id`/`created_at`/`updated_at`；无跨模块 JOIN（列表不显示上传人姓名，显示“系统/手动”即可）。

**唯一键语义**（对应决策③）：

1. 完全相同的 `(project, machine, filename, txt_sha256)` → 重复上传，返回已有记录（HTTP 200 + `duplicate=true`），watcher 视为成功；
2. 同名但内容不同 → 允许两条（文件名相同、sha 不同），如实保留“同一秒被覆盖/重扫”的两份；
3. 内容相同但文件名不同 → 两条都存（唯一键含 filename）；
4. 同名同内容但 png 不同 → 保留已有 png，不覆盖；若已有记录缺 png 而本次带了 png，则补挂 png（审计 `scan.attach_png`），解决“png 迟到”。

---

## 4. API

### 4.1 端点

| 方法 | 路径 | 用途 | 权限 |
|---|---|---|---|
| POST | `/api/v1/projects/{project_id}/scans` | 人工/网页上传 | 项目 member+ |
| POST | `/api/v1/scans/ingest` | watcher 自动入库（固定路径） | 仅 SERVICE_TOKEN |
| GET | `/api/v1/projects/{project_id}/scans` | 列表/过滤/分页 | 项目 viewer+ |
| GET | `/api/v1/projects/{project_id}/scans/tags` | 标签候选（含计数） | 项目 viewer+ |
| GET | `/api/v1/scans/{id}` | 详情（元数据） | 项目 viewer+ |
| PATCH | `/api/v1/scans/{id}` | 改标题/描述/标签 | 项目 member+ |
| POST | `/api/v1/scans/{id}/void` | 软作废（必填原因） | 项目 member+（见 §4.10） |
| GET | `/api/v1/scans/{id}/data` | 按需解析的 `columns/rows` | 项目 viewer+ |
| GET | `/api/v1/scans/{id}/file/{kind}` | `kind=txt\|png`，默认内联 | 项目 viewer+ |
| GET | `/api/v1/scans/{id}/thumb` | 320px 缩略图 | 项目 viewer+ |

**为什么 watcher 用固定 `/scans/ingest` 而不是项目路径**：`middleware/service_token.go` 的白名单是**精确路径相等**匹配（现有 4 条），项目路径是动态的，改成前缀/正则匹配会扩大 token 的授权面。固定端口 + `project_id` 表单字段，白名单只加一条，风险最小。

路由注册注意：chi 下 `/api/v1/scans/ingest` 必须在 `/api/v1/scans/{id}` 之前注册。

### 4.2 上传（人工与 ingest 同一套 service）

`multipart/form-data`：

| 字段 | 必填 | 说明 |
|---|---|---|
| `txt` | 是 | 数据文件，≤5 MiB，tab 分隔 |
| `png` | 否 | 图，≤20 MiB；缺 png 允许 |
| `project_id` | ingest 必填 | 人工上传从路径取，忽略表单 |
| `machine` | 否 | 默认服务端配置；watcher 必传 |
| `scan_at` | 否 | RFC3339；缺省从文件名解析，再退 mtime |
| `tags` | 否 | 逗号分隔或重复字段；缺省用文件名 `source_tag` |
| `title`/`description` | 否 | 补录时可直接带说明 |
| `source_path` | 否 | watcher 上报 NAS 源路径 |
| `ingest_source` | 否 | `manual`（人工）/`backfill`；ingest 端点固定 `watcher` |

限制：整个 multipart ≤50 MiB；只接受 `.txt` + `.png`（按内容校验，png 校验 magic）；`txt` 非法 UTF-8 / 空文件 → 400；超限 → 413。

成功 201：

```json
{
  "data": {
    "id": "0c9f...",
    "duplicate": false,
    "project_id": "3f2a...",
    "filename": "gc261125-101500-qpig.txt",
    "machine": "gascell",
    "scan_at": "2026-11-25T10:15:00+08:00",
    "format": "v7",
    "row_count": 80,
    "bad_rows": 0,
    "tags": ["qpig"],
    "has_png": true,
    "annotated": false
  },
  "request_id": "req_..."
}
```

重复 200：同上，`duplicate: true`，`id` 为已有记录；若本次补挂了 png，`has_png` 为 true。

### 4.3 列表

`GET /api/v1/projects/{project_id}/scans?tags=qpig,rfc&tag_mode=any&from=2026-11-01&to=2026-11-30&q=下降&annotated=false&format=v7&has_png=true&ingest_source=watcher&page=1&per_page=20`

- `tags`：逗号分隔；`tag_mode=any`（默认，`tags && $n`）/`all`（`tags @> $n`）；
- `from`/`to`：日期或 RFC3339，按 `COALESCE(scan_at, created_at)` 比较，闭区间；
- `q`：`filename/title/description` 的 `ILIKE`，转义 `%`/`_`，长度 ≤100；
- `annotated`：`true/false`，对应 `annotated_at` 是否为空；
- `format`/`has_png`/`ingest_source`：直接过滤；
- `include_void`：默认 false；只有 admin 可传 true；
- `page`/`per_page`：默认 1/20，上限 100。

返回 `items` 仅含列表字段（id、filename、title 摘要、tags、scan_at、format、row_count、has_png、annotated、thumb_url、is_void、created_at），不含 description 全文和 sha。

### 4.4 详情 / 说明

`GET /api/v1/scans/{id}` 返回完整元数据：description、sha256、sizes、source_path、file_urls、thumb_url、void 信息。

`PATCH /api/v1/scans/{id}`：

```json
{ "title": "QPIG ref 0–0.25V 扫", "description": "条件：...\n结论：...", "tags": ["qpig", "rfq"] }
```

- 至少一个字段；`title` ≤200，`description` ≤20000，`tags` 规则同上；
- 请求里出现 `title` 或 `description` 时按最终值维护 `annotated_at`：非空 → now()，二者都空 → NULL；
- 只改 `tags` 不改说明状态；
- 每次写审计 `scan.update`（detail 只记变更字段名、标签新旧、标题新值，不复制整段 description）。

### 4.5 作废

`POST /api/v1/scans/{id}/void`，body `{"reason": "..."}`（必填，≤500）。软作废后：默认列表隐藏、详情显示作废横幅、下载禁用；文件仍保留（长期保存）。不做 unvoid，确需恢复走 admin SQL。

### 4.6 数据解析

`GET /api/v1/scans/{id}/data`：

```json
{
  "data": {
    "format": "v7",
    "columns": ["ref","pA","sigma","Vpp1","Vpp2","Vpp3","Vpp4"],
    "rows": [[0.05,-12.929,3.341,2.480,1.045,1.851,2.165]],
    "row_count": 80,
    "bad_rows": 0,
    "warnings": [],
    "truncated": false
  },
  "request_id": "req_..."
}
```

- 行是数组，缺失值用 `null`；最多返回 50000 行，超出截断并 `truncated=true`；
- `legacy3`/`unknown` 返回 422 `legacy_format` / `unparsed_format`，前端只展示原件和 png；
- 解析失败不 500，返回 422 + 明确 code。

### 4.7 文件与缩略图

- `GET /api/v1/scans/{id}/file/png`：`image/png`，`Content-Disposition: inline`，支持 Range/`ServeContent`；
- `GET /api/v1/scans/{id}/file/txt`：`text/plain; charset=utf-8`，默认 inline；`?download=1` 强制 attachment；
- `Cache-Control: private, max-age=3600`；`ETag` 用 sha256；`X-Content-Type-Options: nosniff`；
- 内联读**不要求** `Idempotency-Key`、不写审计；`download=1` 要求 key 并写 `scan.download`；
- `thumb` 不存在返回 404，前端回退原图或占位图。

### 4.8 错误码

| HTTP | code | 场景 |
|---|---|---|
| 400 | `bad_request` | 参数/表单非法 |
| 400 | `missing_idempotency_key` | 写接口缺 key |
| 403 | `permission_denied` | 非项目成员 / 非 SERVICE_TOKEN 调 ingest |
| 404 | `scan_not_found` | id 不存在 |
| 404 | `scan_file_not_found` | png 缺失或文件丢失 |
| 413 | `scan_too_large` | txt/png/总包超限 |
| 415 | `unsupported_file_type` | 扩展名或 magic 不符 |
| 422 | `legacy_format` / `unparsed_format` | 不可作图 |
| 500 | `internal_error` | 其他 |

### 4.9 幂等、去重与审计

- 人工写接口：前端 `client.ts` 自动生成 `Idempotency-Key`，中间件 `RequireIdempotencyKey` 强制；
- service ingest：中间件对 service call 跳过幂等（现有行为），但 handler 仍要求 header；watcher 固定用 `scan-<txt_sha256>`；
- 去重按 §3 唯一键；服务端先查后插，冲突时删掉本次新写目录并返回已有记录；
- 审计 action：`scan.create`（含 `scan.attach_png`）、`scan.update`、`scan.void`、`scan.download`；action 长度均 <64；
- `scan.create` 的 detail：project、filename、machine、format、row_count、txt/png sha、size、source、ingest_source、duplicate；
- GET 列表/详情/内联图不审计（理由：审计表已分区但仍不应被缩略图刷屏；显式下载才审计）。

### 4.10 权限矩阵

| 操作 | viewer | member | owner | admin | agent | SERVICE_TOKEN |
|---|---|---|---|---|---|---|
| 列表/详情/看图/解析 | ✅ | ✅ | ✅ | ✅ | ❌ | 仅 ingest |
| 上传（人工） | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ |
| 改说明/标签 | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ |
| 作废 | ❌ | ✅ | ✅ | ✅ | ❌ | ❌ |
| 自动入库 | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |

权限判断走 `ProjectAccessAdapter`（与 testdata/test_data_curves 同款），service 层集中做，handler 不散落。

---

## 5. 文件存储（gascell 单副本）

### 5.1 gascell 副本

- 根目录：`SCAN_STORAGE_DIR`，默认 `/app/uploads/scans`（`lab-uploads` 卷内，随备份）；
- 布局（路径按 UUID，不用客户端文件名）：

```text
scans/<project_id>/<yyyy>/<scan_id>/scan.txt
scans/<project_id>/<yyyy>/<scan_id>/scan.png
scans/<project_id>/<yyyy>/<scan_id>/thumb.png
```

- `<yyyy>` 取 `scan_at` 年份，缺失退 `created_at`；`scan_id` 由服务端生成 UUID；
- 下载时用 DB 里的 `filename` 作为 `Content-Disposition` 文件名，落盘名固定，彻底避免路径穿越；
- 写入顺序：临时文件 → 算 sha → rename 到最终目录 → 写 DB；DB 失败删除目录；目录内文件缺失视为 `scan_file_not_found`；
- 并发同文件：唯一键冲突时删除本次新目录，返回已有记录；
- 缩略图：png 320px 宽（`golang.org/x/image/draw`），生成失败不阻塞入库（`thumb_path` 为空）；
- 清理：孤儿目录由启动时/每日的轻量清理任务扫描（目录存在但 DB 无行且 mtime > 24h），只记录不自动删；提供 `--cleanup-orphans` 手动开关，避免误删。

### 5.2 NAS 源目录（只读，不动）

- watcher 只读 `SCAN_WATCH_DIR`（如 `/mnt/mtof/DJ/GC/test`）；
- 本系统**不在 NAS 上创建归档目录、不复制回写、不移动、不删除**任何文件；
- 源文件被 LabVIEW 覆盖/清理的风险由源端承担；文件一旦入库，gascell 副本独立存在，不再依赖源文件；
- 需要从源目录重建时，用 watcher 的 backfill 重新导入：命中唯一键会返回已有记录，不会重复入库。

### 5.3 存储边界与恢复

| 位置 | 内容 | 谁写 | 生命周期 |
|---|---|---|---|
| NAS 源目录 | VI 输出原件 | LabVIEW | 源端自行管理，本系统只读 |
| gascell `lab-uploads/scans` | 系统唯一副本 | scans 模块 | 长期，随 DB 每日备份打包 |

- 说明文字只在 DB；DB 每日备份（保留 14 天）。
- 恢复路径：
  - gascell 卷丢 → 恢复 uploads 备份 + DB dump；若超出备份保留期且 NAS 源件仍在，可重新导入文件（说明文字依赖 DB 备份）；
  - NAS 源件丢 → 不影响系统使用，gascell 副本仍在；
  - DB 丢 → 恢复 dump，文件仍在，可用 `filename + sha256` 对账。
- **风险登记**：单副本意味着 gascell 磁盘是唯一系统副本。必须确认 `backup.sh` 的 uploads 打包与 `restore-check.sh` 演练覆盖 `scans/`；磁盘使用率告警见 §13。后续若要求第二副本，再启用 v0.3 的 NAS 归档方案，不影响本版表结构（届时不一定要 `nas_*` 字段，可另加迁移）。

---

## 6. 自动导入器（lab-scan-ingest）

### 6.1 部署形态

- 独立 Python 3.11 脚本（`deploy/scan-ingest/scan_ingest.py` + `requirements.txt`），gascell 宿主机 systemd：
  - `lab-scan-ingest.service`（`Type=simple`，`Restart=always`，`ProtectSystem=strict`、`NoNewPrivileges`、`ReadWritePaths=/var/lib/lab-scan-ingest`）；
  - 运行用户 `labscan`，对 NAS 挂载点只读，对 state 目录读写；
  - 服务 token 放 `/etc/lab-scan-ingest/service_token`（0400，`LoadCredential` 或 `EnvironmentFile`），不读仓库路径；
- 配置（`/etc/lab-scan-ingest/config.env`，0600）：

```ini
SCAN_WATCH_DIR=/mnt/mtof/DJ/GC/test
SCAN_PROJECT_ID=<gc 项目 UUID>
SCAN_MACHINE=gascell
LAB_API_URL=http://127.0.0.1:8000
SERVICE_TOKEN_FILE=/etc/lab-scan-ingest/service_token
STATE_FILE=/var/lib/lab-scan-ingest/state.json
POLL_INTERVAL=15
STABLE_AGE_SECONDS=5
PNG_GRACE_SECONDS=300
DRY_RUN=0
```

v1 只允许一个目录/一个项目；配置结构可扩展为 `SCAN_WATCH_DIRS` 多行（目录|项目|machine），但代码先按单条实现，避免过度设计。

### 6.2 完成判定与状态机

SMB 上 `inotify` 不可靠，**用轮询**：

1. 每 15s 扫 `gc*.txt`，记录 `(size, mtime_ns)`；
2. 连续两次采样 `(size, mtime)` 不变，且 `mtime` 距今 ≥5s → txt 稳定；
3. 同名 png：存在则同样要求稳定；不存在则等 `PNG_GRACE_SECONDS`（默认 300s）后按“无图”入库；
4. 稳定后算 txt sha256，查本地 state；已成功则跳过；
5. POST `multipart` 到 `/api/v1/scans/ingest`，`Idempotency-Key: scan-<sha256>`；
6. 201/200 → 记成功；4xx（除超时）→ 记失败并告警一次；5xx/网络错误 → 指数退避重试（下次轮询继续，最多 5 次后告警）；
7. png 迟到：后续轮询发现“已有记录 + 本次有 png + 记录无 png”时，重新 POST 同一 txt+png，服务端按 §3 规则补挂 png。

state 文件（原子写 `os.replace`）：

```json
{
  "version": 1,
  "files": {
    "gc261125-101500-qpig.txt": {
      "size": 2048,
      "mtime_ns": 1764036900000000000,
      "sha256": "...",
      "status": "success",
      "attempts": 0,
      "last_error": "",
      "updated_at": "2026-11-25T10:16:00+08:00"
    }
  }
}
```

state 条目保留 90 天；文件被同名覆盖（size/mtime 变化）视为新任务，重新入库（唯一键含 sha，允许两条）。

### 6.3 文件名解析

```text
^gc(\d{6})-(\d{6})-(.+)\.(txt|png)$
```

- `yymmdd` → `20yy-mm-dd`，时间按 `Asia/Shanghai`；
- tag 允许空（`gc261125-101500-.txt` → `untagged`），只保留 `[A-Za-z0-9._-]`，ASCII 转小写；
- 解析失败不拒绝：正常入库，`scan_at` 用 mtime、`source_tag` 空、`parse_warning=filename_unparsed`；
- 文件名长度 ≤256，拒绝含路径分隔符。

### 6.4 存量导入（backfill）

- `lab-scan-ingest --backfill --dry-run`：只扫描、解析、输出 manifest（文件名、大小、格式、sha、是否已有、缺 png、解析警告），不改库；
- 人工过 manifest 后 `--backfill`：按 mtime 升序，限速 5 req/s，逐条 POST；4xx 记入失败清单，可重跑；
- 缺 png 的历史文件按“无图”入库；legacy3 只存档；
- 导入完成后对账：NAS 文件数 / DB 行数 / 缺 png 数 / 失败清单；
- 导入不写说明，用户后续补。

### 6.5 心跳与告警

- 每轮写 `/var/lib/lab-scan-ingest/heartbeat`（时间戳、pending、失败数）；
- 扩展 `deploy/scripts/watchdog.sh`：心跳超过 5 分钟 → 告警中心（只告警不重启）；
- 连续失败、NAS 不可读、state 文件损坏、磁盘不足 → `POST /api/v1/alerts/report`（SERVICE_TOKEN 已在白名单）；
- 日志 JSON lines 到 journald，失败保留 last_error 便于排查。

### 6.6 安全

- watcher 只读 NAS、只写 state；不碰源目录；
- token 不落仓库、不写日志；
- 入库端点只认 SERVICE_TOKEN，JWT 用户调用一律 403；
- 上传限速可由现有 `047_rate_limit_counters` 机制扩展（可选，v1 不做）。

---

## 7. 数据解析

### 7.1 格式识别

| 形态 | 判定 | format |
|---|---|---|
| 7 列 + 表头（当前） | 首行非数值且 7 列 | `v7` |
| 11 列 + 表头 | 首行非数值且 11 列 | `v11` |
| 12 列 + 表头 | 首行非数值且 12 列 | `v12` |
| 3 列无表头 | 首行 3 个数值 | `legacy3` |
| 其他 | 无法判定 | `unknown` |

- 以表头为准按列名映射，**不按固定顺序**；表头名 trim、去 BOM、大小写归一；
- 已知列名：`ref`、`pA`（兼容 `pa`）、`sigma`、`Vpp1..4`、`TVpp1..4`；未知列保留原样；
- 无表头但 7/11/12 列 → `unknown` + 警告，仍存档，不作图；
- 编码：UTF-8（去 BOM）；解码失败 → `unknown`，只存档。

### 7.2 行解析

- tab 分隔；CRLF/空行跳过；
- 数值用 `float64`；空单元格 → `null`；非数值 → 该行计入 `bad_rows` 并跳过；
- 每行必须能确定 x（优先 `ref`，否则第一列）且至少一个 y；
- `row_count` 为有效行；`bad_rows` 为跳过行；
- 上限：50000 行返回，超出截断；文件大小上限天然限制。

### 7.3 作图语义

- x = `ref`（无则第一列）；
- series = 除 x 外所有数值列；
- 左轴：`pA`、`sigma`；右轴：`Vpp*`/`TVpp*`；未知列默认左轴，前端可切换；
- 图例可开关 series；legacy/unknown 不显示曲线，只显示表格/原件提示。

---

## 8. 权限、审计与安全

- 权限矩阵见 §4.10；项目访问统一走 `ProjectAccessAdapter`；
- 审计：`scan.create`/`scan.attach_png`/`scan.update`/`scan.void`/`scan.download`；GET 内联读不审计；
- SERVICE_TOKEN 白名单只加 `POST /api/v1/scans/ingest`，handler 内 `middleware.IsServiceCall` 强校验；
- CSRF：人工写接口走现有 CSRF；service call 豁免（现有机制）；
- 上传安全：固定落盘名、magic 校验、大小限制、`nosniff`、不按客户端文件名拼路径；
- 禁止硬编码 token/NAS 凭据；NAS 挂载凭据在 `/etc`，不进仓库；
- 说明字段按纯文本/markdown 存储，前端渲染需转义，禁止 `v-html` 直出。

---

## 9. 前端

### 9.1 路由与导航

- 项目工作区 tab：`ProjectLayout.vue` 的 `tabs` 增加 `{ label: t('project.tabs.scans'), name: 'scans', path: 'scans' }`；
- 路由：
  - `/projects/:id/scans` → `ScanArchiveView.vue`（列表）
  - `/projects/:id/scans/:scanId` → `ScanDetailView.vue`（详情）
  - `meta: { titleKey: 'nav.scans' }`，无 `admin`/`reviewer`；
- 全局导航不需要新增项（项目内 tab 已可达）；若要在主导航露出，可加 `/scans` 重定向到项目选择，v1 不做。

### 9.2 列表页

- 过滤条：标签多选（来自 `/tags`）、日期区间、关键词、`annotated` 开关、format 下拉、清空；
- 卡片/表格：缩略图（懒加载，`thumb`，失败占位）、标题（空则文件名）、标签 chips、扫描时间、format 徽标、行数、`待说明` 徽标；
- 分页；空/加载/错误三态（`StateBlock`/`ListPage`）；移动端卡片化；
- URL query 同步过滤条件，便于分享/返回。

### 9.3 详情页

- 顶部：文件名、machine、scan_at、format、行数/坏行、sha 短码、项目、作废状态；
- 左：png 大图（点击放大/下载）+ 曲线（`ScanCurveChart`）；legacy 显示“仅存档”；
- 右：说明编辑（标题、描述 textarea、标签 `el-select allow-create multiple`）、保存、下载原件、源路径只读；
- 保存成功显示 `updated_at`；失败显示 `request_id` 和错误信息；
- 移动端上下堆叠。

### 9.4 图表组件

- `src/components/business/ScanCurveChart.vue`；
- 数据来自 `GET /scans/{id}/data`；x=ref，左轴 pA/sigma，右轴 Vpp/TVpp；
- 使用 `chartTheme.ts`（`setupChartDefaults`/`chartPalette`/`buildChartGroups`）与 Chart.js；不引第二套图表库；
- `watch(useTheme().state)` → `refreshDefaults()` + destroy/重建；禁止组件内 `Chart.register`；
- 无数据/legacy 显示空态；系列开关；tooltip 显示列名+数值。

### 9.5 i18n、状态与约定

- `zh.ts` 完整新增 `scanArchive.*`、`project.tabs.scans`、`nav.scans`；`en.ts` 对齐；
- `statusMeta` 新增 `scanNote`：`pending`（warning）/`annotated`（success），label 用显式 key；
- 视图内不写死中文、不写 hex/rgba，时间用 `datetime.ts`；
- API 模块 `src/api/scans.ts`：upload/list/tags/get/patch/void/data/fileUrl/thumbUrl；
- 权限按钮隐藏只是 UX，后端仍强校验。

---

## 10. 与 `test_data_curves` 的关系

- `scans` 是**原始文件档案**（txt+png+说明），保留仪器原始输出和可追溯路径；
- `test_data_curves` 是**人工/工具整理后的结构化曲线**（points JSONB，项目/run 关联，可作图）；
- v1 不自动互写；未来可加“从 scan 提取曲线 → 创建 test_data_curve”，在 `test_data_curves` 增加 `scan_id` 溯源列（新迁移），并复用本方案的 `ScanCurveChart`；
- 两者都项目级、都软作废、都走项目 ACL，但表/模块分开，避免把“原件+说明”塞进曲线表。

---

## 11. 迁移、文档与部署

- 迁移 **050_scans**（当前最高 050；PR1 已落）；`.up.sql`/`.down.sql` 成对，down 为 `DROP TABLE scans`（文件不删，文档注明）；
- `go-server/scans/`：`model.go`/`repository.go`/`service.go`/`handler.go` + 测试；repository 只访问 `scans` 表；
- `main.go`：构造 repo/service/handler，注入 `ProjectAccessAdapter` 和存储根；不需要 NAS 挂载，也不启动同步 worker；
- `docs/api-contract.md`：新增本章端点/错误码/枚举（`format`、`ingest_source`、审计 action）；
- `docs/permission-audit.md`：登记项目级资源、SERVICE_TOKEN 白名单新增项、下载审计策略；
- `docs/maintenance-strategy.md`：watcher systemd（宿主机只读 NAS 挂载）、备份/恢复步骤；
- `deploy/docker-compose.yml`：**不需要改**；server 不挂载 NAS，watcher 在宿主机运行；
- `AGENTS.md` §5/§7：模块树、脚本表、迁移数在实施 PR 中更新；
- 回滚：停 watcher → 关 ingest 路由（或 `SCAN_INGEST_ENABLED=false`）→ 代码回滚；表可保留，不影响其他模块；确认不再使用后再执行 down。

---

## 12. 测试计划

**Go**

- parser 表驱动：v7 乱序列名、BOM/CRLF、坏行、空值、11/12 列、legacy3、unknown、超大行数；
- service：去重（同键/同名不同 sha/同名不同内容/内容相同不同名）、png 迟到补挂、tags 校验、ACL 403、作废、软删除、大小限制；
- handler：multipart 缺文件/缺 key/413/415、ingest 非 service 403、404、422；
- repository/db_test：唯一键冲突、列表过滤（any/all/时间/q）、软作废隐藏；
- `go test ./...`、`go vet ./...`。

**Watcher**

- 文件名解析、稳定判定（注入时钟）、state 断点续跑、重复 POST 返回 duplicate、png 迟到、退避重试、dry-run/backfill manifest、状态文件损坏恢复；
- 用临时目录 + fake HTTP server 跑，不依赖真实 NAS。

**前端**

- `src/api/scans.ts`、`statusMeta` 新域、过滤参数构造、图表 option 纯函数；
- 列表/详情组件测试断言关键文案与状态；`npm test` + `npm run test:coverage`；
- E2E：seed 一条 scan（service token 或测试夹具）→ 列表过滤 → 详情看图/曲线 → 写说明 → “待说明”消失 → 下载。

**迁移/运维**

- `scripts/test-all.sh` 覆盖 050 up/down；
- `restore-check.sh` 增加 scans 行数校验（可选）；
- 手工演练：NAS 断连时 watcher 行为、gascell 卷恢复。

---

## 13. 运维

- **备份**：DB `pg_dump` 含 `scans`；`lab-uploads` 卷随全量备份打包；NAS 源目录不备份（只读源，系统不依赖）；
- **恢复**：
  - DB 丢 → 恢复 dump，文件仍在；
  - gascell 卷丢 → 恢复 uploads 备份 + DB dump；备份期外且源件仍在时，用 backfill 重新导入；
  - NAS 源件丢 → 不影响系统使用，gascell 副本仍在；
- **监控**：watcher 心跳（watchdog）、磁盘使用率、ingest 失败日志；
- **容量**：369 对约几十 MB；按当前频率年增 <1GB；`lab-uploads` 磁盘 ≥80% 告警；
- **特性开关**：`SCAN_INGEST_ENABLED`（server 端 ingest 路由开关）、watcher `DRY_RUN`；
- **保留**：不自动删业务数据；作废保留文件；备份保留期按现有策略。

---

## 14. 实施拆分

1. **后端骨架**：迁移 050、scans 模块、parser、项目 ACL、单元测试；
2. **文件/接口**：存储、缩略图、上传/list/detail/patch/void/data/file、审计/幂等、handler 测试；
3. **ingest/watcher**：`/scans/ingest`、SERVICE_TOKEN 白名单、Python watcher、systemd、state、dry-run；
4. **监控/运维**：watcher 心跳、告警、磁盘与备份；
5. **前端**：tab/路由、列表、详情、图表、说明编辑、i18n/statusMeta；
6. **存量导入/上线**：dry-run manifest → 正式导入 369 对 → 对账 → E2E → 文档收口。

依赖顺序：1 → 2 →（3/5 可并行）→ 4 → 6。

---

## 15. 验收标准

- [ ] watcher 在扫描完成后 30s 内自动入库 txt+png；重复轮询不产生重复行；
- [ ] 同名不同内容、内容相同不同文件名均按决策③各存一条；
- [ ] gascell 副本可下载/看图；NAS 源目录未被修改（只读校验：文件名/数量/mtime 不变）；
- [ ] 项目 viewer 只读，member+ 可写说明/作废；非成员 403；
- [ ] ingest 仅 SERVICE_TOKEN 可调，JWT 调用 403；
- [ ] 写接口有 `Idempotency-Key` 和审计；内联图不刷审计；
- [ ] 列表可按标签（any/all）、时间、关键词、待说明过滤；详情可看图/曲线/原始数据；
- [ ] legacy3/unknown 只存档不作图，提示明确；
- [ ] 369 对存量导入对账一致（缺 png 的 2 条有明确记录）；
- [ ] `go test ./...`、`go vet`、前端测试/覆盖率、E2E 全绿；
- [ ] 050 up/down 可逆；回滚步骤演练通过。

---

## 16. 已确认项与剩余部署参数

已确认（2026-11-25）：

1. **GC 项目**：项目级；`SCAN_PROJECT_ID` 在部署时填入 GC 项目 UUID（不写死在代码里）；
2. **作废权限**：member+，与 testdata 一致；admin 可覆盖；
3. **NAS**：只读源，`SCAN_WATCH_DIR=/mnt/mtof/DJ/GC/test`；不归档、不复制、不删除；
4. **说明边车**：不做；说明文字只存 DB，随每日备份；
5. **machine**：默认 `gascell`（可配置，用于唯一键和展示）。

剩余只需部署时确认：GC 项目 UUID、NAS 挂载点实际路径、watcher 运行用户对源目录的只读权限。

---

**下一步**：决策已确认，可按 §14 拆成 6 个 PR 实施；本稿只评审，未动代码。
