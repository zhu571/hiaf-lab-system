# 项目维度设计

> 版本：v1  
> 日期：2026-07-14  
> 适用范围：Hermes 实验室日志系统扩展设计

## 1. 核心结论

系统应把「项目」作为日志、Issue、测试数据、经验库、计划和报告的核心组织维度，但不应把「日报」简单改成“每天每项目一条”。

推荐模型是：

- `daily_reports` 保留为“某人某天的个人工作日容器”，用于补录、回顾和微信式自然语言入口。
- `logs` 或 `daily_report_items` 承载真正的项目化工作记录，每条记录必须能归属到一个主项目。
- Issue、测试数据、经验、计划、报告都直接挂 `project_id`。
- 一个自然日可以有多个项目记录；一个日报可以包含多个项目的记录。
- 无项目记录只允许作为草稿、个人杂项或待分类记录，不能长期进入正式项目统计。
- 子项目/实验批次先不要做成完整层级，第一阶段用轻量的 `experiment_runs` 或 `batch_id` 解决测试批次归集。

这样既兼容当前按日期组织的 SQLite 数据，也能支持低温气体靶、MNT 反应、新实验等多个项目并行。

## 2. 数据模型

### 2.1 projects

`projects` 是项目基本信息表。

```sql
CREATE TABLE projects (
    id              TEXT PRIMARY KEY,
    code            TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    short_name      TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL
                    CHECK (status IN ('draft','active','completed','archived')),
    visibility      TEXT NOT NULL DEFAULT 'restricted'
                    CHECK (visibility IN ('restricted','workspace')),
    owner_user_id   TEXT NOT NULL,
    start_date      DATE,
    target_end_date DATE,
    completed_at    TEXT,
    archived_at     TEXT,
    default_category TEXT NOT NULL DEFAULT '',
    tags_json       TEXT NOT NULL DEFAULT '[]',
    created_by      TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_projects_status ON projects(status);
CREATE INDEX idx_projects_owner ON projects(owner_user_id);
```

字段说明：

| 字段 | 说明 |
|------|------|
| `id` | 稳定主键，例如 `prj_gas_target_001` |
| `code` | 人可读短码，例如 `gas-target`、`mnt` |
| `name` | 项目全名 |
| `status` | 生命周期状态 |
| `visibility` | 是否仅授权成员可见；实验项目默认 `restricted` |
| `owner_user_id` | 项目负责人 |
| `default_category` | 兼容旧系统分类，例如 `gas_cell`、`rf` |
| `tags_json` | 别名、实验方向、设备关键词，用于 Agent 识别 |

### 2.2 project_members

`project_members` 管项目成员和项目内角色。

```sql
CREATE TABLE project_members (
    project_id  TEXT NOT NULL REFERENCES projects(id),
    user_id     TEXT NOT NULL,
    role        TEXT NOT NULL
                CHECK (role IN ('owner','maintainer','member','viewer')),
    status      TEXT NOT NULL DEFAULT 'active'
                CHECK (status IN ('active','suspended')),
    joined_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    added_by    TEXT NOT NULL,
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX idx_project_members_user ON project_members(user_id);
```

项目角色建议：

| 角色 | 权限 |
|------|------|
| `owner` | 管理项目、成员、归档、导出、所有项目内数据 |
| `maintainer` | 管理日志、Issue、计划、经验审核，不可删项目 |
| `member` | 创建和修改自己提交的日志，参与 Issue 和计划 |
| `viewer` | 只读项目内容 |

系统级 `admin` 不需要写入 `project_members`，但权限判断时拥有兜底管理权。

### 2.3 daily_reports 与项目关系

当前 SQLite 的 `daily_reports` 是每天一条，字段只有 `report_date` 和 `summary`。新系统建议改为“用户每天一条”：

```sql
CREATE TABLE daily_reports (
    id          TEXT PRIMARY KEY,
    report_date DATE NOT NULL,
    author_id   TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft','submitted','locked')),
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (report_date, author_id)
);
```

日报本身不放 `project_id`，因为同一天可能服务多个项目。项目归属下沉到日报条目：

```sql
CREATE TABLE daily_report_items (
    id              TEXT PRIMARY KEY,
    daily_report_id TEXT NOT NULL REFERENCES daily_reports(id),
    project_id      TEXT REFERENCES projects(id),
    occurred_at     TEXT NOT NULL,
    category        TEXT NOT NULL DEFAULT 'general',
    content         TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft','confirmed','voided')),
    source          TEXT NOT NULL DEFAULT 'manual'
                    CHECK (source IN ('manual','wechat','agent','import')),
    confidence      REAL,
    created_by      TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_daily_report_items_project_date
ON daily_report_items(project_id, occurred_at);
```

这里 `project_id` 允许暂时为空，但仅限草稿或待分类记录。提交日报时，系统应提示用户把项目补齐；确实无法归类的内容归入专门的“实验室公共事务”项目，而不是长期留空。

### 2.4 logs 与 daily_report_items 的取舍

如果系统已经计划引入 `logs` 表，可以把 `daily_report_items` 合并为 `logs`，再用 `daily_report_log_links` 关联日报：

```sql
CREATE TABLE logs (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    author_id   TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    category    TEXT NOT NULL DEFAULT 'general',
    content     TEXT NOT NULL,
    source      TEXT NOT NULL DEFAULT 'manual',
    status      TEXT NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft','confirmed','locked','voided')),
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE daily_report_log_links (
    daily_report_id TEXT NOT NULL REFERENCES daily_reports(id),
    log_id          TEXT NOT NULL REFERENCES logs(id),
    PRIMARY KEY (daily_report_id, log_id)
);
```

推荐长期采用 `logs` 方案，因为 API 合同中已经使用 `/api/v1/logs`，并且日志可独立于日报被 Issue、计划、仪器数据引用。迁移阶段可以先实现 `daily_report_items`，再演进到 `logs`，但不要同时长期维护两套同类业务表。

### 2.5 issues

Issue 必须归属一个主项目：

```sql
ALTER TABLE issues ADD COLUMN project_id TEXT REFERENCES projects(id);
```

新系统完整字段建议：

| 字段 | 说明 |
|------|------|
| `project_id` | 必填，控制可见性和统计归属 |
| `report_date` / `occurred_at` | 问题发生日期，保留旧字段便于迁移 |
| `related_log_ids` | 通过 `issue_log_links` 关联日志 |
| `status` | `open`、`in_progress`、`resolved`、`closed` |
| `severity` | `low`、`medium`、`high`、`critical` |

如果一个问题影响多个项目，仍然只选一个主项目，并通过 `issue_project_links` 记录次要关联：

```sql
CREATE TABLE issue_project_links (
    issue_id   TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    relation   TEXT NOT NULL DEFAULT 'related'
               CHECK (relation IN ('primary','related','blocked_by','blocks')),
    PRIMARY KEY (issue_id, project_id)
);
```

权限以主项目为准。次要项目成员如果没有主项目权限，默认只能看到脱敏摘要，除非显式授权。

### 2.6 test_data 与实验数据

测试数据必须能按项目统计，因此新增 `project_id`：

```sql
ALTER TABLE test_data ADD COLUMN project_id TEXT REFERENCES projects(id);
```

同时建议引入轻量实验批次表，而不是完整子项目层级：

```sql
CREATE TABLE experiment_runs (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects(id),
    name         TEXT NOT NULL,
    run_type     TEXT NOT NULL DEFAULT '',
    started_at   TEXT,
    ended_at     TEXT,
    status       TEXT NOT NULL DEFAULT 'active'
                 CHECK (status IN ('planned','active','completed','aborted','archived')),
    notes        TEXT NOT NULL DEFAULT '',
    created_by   TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE test_data ADD COLUMN experiment_run_id TEXT REFERENCES experiment_runs(id);
```

`test_data.project_id` 用于权限和项目统计，`experiment_run_id` 用于把同一轮降温、一次束流实验、一次 RF 匹配扫描归在一起。即使没有批次，测试数据也必须有项目。

`rf_matching_circuits`、`assembly_process`、`cryo_runs` 同样需要 `project_id`，并可选关联 `experiment_run_id`。

### 2.7 experiences

经验库需要项目归属，但允许少量跨项目通用经验。

推荐：

- `experiences.project_id`：主项目，可为空仅用于“全局经验”。
- `experience_project_links`：跨项目复用关系。
- 全局经验必须经过管理员或经验库负责人审核，避免项目权限绕过。

```sql
CREATE TABLE experience_project_links (
    experience_id TEXT NOT NULL,
    project_id    TEXT NOT NULL REFERENCES projects(id),
    relation      TEXT NOT NULL DEFAULT 'applicable'
                  CHECK (relation IN ('primary','applicable','derived_from')),
    PRIMARY KEY (experience_id, project_id)
);
```

读取规则：

- 项目经验：需要对应项目 `read` 权限。
- 全局经验：所有登录用户可读，但不得包含项目敏感数据。
- 跨项目经验：用户只能看到自己有权限项目的上下文和来源链接。

### 2.8 plans

计划必须归属项目：

```sql
ALTER TABLE plans ADD COLUMN project_id TEXT NOT NULL REFERENCES projects(id);
```

计划任务可以关联日志、Issue 和实验批次：

```sql
CREATE TABLE plan_task_links (
    task_id     TEXT NOT NULL,
    object_type TEXT NOT NULL
                CHECK (object_type IN ('log','issue','experiment_run','test_data')),
    object_id   TEXT NOT NULL,
    relation    TEXT NOT NULL DEFAULT 'related',
    PRIMARY KEY (task_id, object_type, object_id)
);
```

这样周报可以从计划任务反查完成证据。

### 2.9 附件与图片

附件不直接靠项目字段授权，而是继承绑定对象权限。为提高查询效率，可以在附件链接表中冗余 `project_id`：

```sql
CREATE TABLE attachment_links (
    attachment_id TEXT NOT NULL,
    object_type   TEXT NOT NULL,
    object_id     TEXT NOT NULL,
    project_id    TEXT REFERENCES projects(id),
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (attachment_id, object_type, object_id)
);
```

写入时由后端根据绑定对象填充 `project_id`，前端和 Agent 不应直接传这个字段。

## 3. 项目生命周期

项目状态建议为 `draft -> active -> completed -> archived`。

### 3.1 draft：创建中

用途：

- 项目负责人或 admin 创建项目。
- 补齐项目描述、成员、默认分类、仪器关联建议、Agent 识别别名。

行为：

| 行为 | 规则 |
|------|------|
| 新增日志 | 默认不允许；owner 可创建少量筹备日志 |
| 新增 Issue | 不允许，除非标记为筹备问题 |
| 新增测试数据 | 不允许 |
| 新增计划 | 允许 |
| 修改项目配置 | owner、admin 可改 |
| 删除项目 | 仅无业务数据时允许硬删除，否则改为归档 |

### 3.2 active：活跃

用途：

- 正常实验、记录、问题跟踪、计划推进。

行为：

| 行为 | 规则 |
|------|------|
| 新增日志 | 有 `create_log` 权限可新增 |
| 新增 Issue | 有 `create_issue` 权限可新增 |
| 新增测试数据 | 有项目写权限且有对应仪器/数据源权限可新增 |
| 新增经验候选 | 允许 |
| 修改项目配置 | owner、maintainer 可改非敏感字段 |
| 成员变更 | owner 或 `manage_members` 权限 |
| 报告/导出 | 需要 `export` 或报告权限 |

### 3.3 completed：完成

用途：

- 项目实验主体完成，但仍需要整理报告、关闭 Issue、沉淀经验。

行为：

| 行为 | 规则 |
|------|------|
| 新增普通日志 | 默认不允许，避免完成后继续混入新实验 |
| 新增整理日志 | 允许类别为 `analysis`、`summary`、`documentation` 的记录 |
| 新增测试数据 | 默认不允许；owner 可补录并标记 `source=backfill` |
| 修改历史日志 | 只允许作者、maintainer、owner 做受审计修改 |
| 关闭 Issue | 允许 |
| 发布经验 | 允许 |
| 生成最终报告 | 允许 |
| 新增计划 | 仅允许收尾计划 |

完成状态不是只读，因为实验结束后经常还要补数据、写总结和关问题。但所有补录应带 `backfill_reason` 或审计事件。

### 3.4 archived：归档

用途：

- 项目只读保存，进入历史查询和审计状态。

行为：

| 行为 | 规则 |
|------|------|
| 新增日志 | 不允许 |
| 新增 Issue | 不允许 |
| 新增测试数据 | 不允许 |
| 修改业务数据 | 默认不允许 |
| 评论 | 不允许，或仅 admin 追加归档说明 |
| 读取 | 有项目读权限者可读 |
| 导出 | owner、admin 或明确授权者可导出 |
| 解归档 | 仅 admin 或 owner，必须写审计原因 |

归档后如发现历史错误，推荐创建“更正记录”而不是直接改原始数据。确需修改时，应保留原值、修改人、原因和时间。

## 4. 跨日与跨项目录入

### 4.1 同一天涉及多个项目

同一天做多个项目时，不应该创建多条互相竞争的日报，也不应该让一条日志挂多个主项目。推荐规则：

- 一个用户每天最多一条 `daily_reports`。
- 日报下有多条 `logs` 或 `daily_report_items`。
- 每条日志选择一个主项目。
- 日报页面按项目分组展示当天条目。

示例：

| 日期 | 日报 | 日志条目 |
|------|------|----------|
| 2026-07-14 | 张三的日报 | 气体靶：低温测试达到 80 K |
| 2026-07-14 | 张三的日报 | MNT：整理反应截面模拟结果 |
| 2026-07-14 | 张三的日报 | 公共事务：维护实验室电脑 |

这样个人日报仍然完整，项目视角也能准确聚合。

### 4.2 一条工作是否能挂多个项目

默认不能。一条记录只能有一个主项目，原因是：

- 权限判断清晰。
- 周报和统计不会重复计数。
- Issue 负责人和项目责任边界明确。

确实跨项目时使用关联表：

```sql
CREATE TABLE log_project_links (
    log_id     TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    relation   TEXT NOT NULL DEFAULT 'related'
               CHECK (relation IN ('primary','related','shared_result','dependency')),
    PRIMARY KEY (log_id, project_id)
);
```

主项目记录在 `logs.project_id`，关联项目记录在 `log_project_links`。统计默认只按主项目计数，除非报表明确选择“包含关联项目”。

### 4.3 跨日工作

跨日工作不要靠同一条日报拉长时间范围，而应拆成：

- 每天一条进展日志。
- 一个 `experiment_run` 或 `plan_task` 作为跨日容器。

例如一次降温从 7 月 14 日持续到 7 月 16 日：

- `experiment_runs` 记录整体起止、目标温度、状态。
- 每天的日志记录当天温度、问题、处理。
- 测试数据都关联同一个 `experiment_run_id`。

## 5. 权限设计

### 5.1 项目级权限动作

项目 ACL 建议动作：

| 动作 | 含义 |
|------|------|
| `read` | 查看项目、日志、Issue、计划、测试数据摘要 |
| `create_log` | 创建项目日志 |
| `update_log` | 修改项目内日志 |
| `create_issue` | 创建项目 Issue |
| `update_issue` | 修改 Issue 和评论 |
| `manage_plan` | 创建和维护计划、任务、里程碑 |
| `write_test_data` | 写入或补录测试数据 |
| `review_experience` | 审核经验候选 |
| `manage_members` | 管理项目成员 |
| `export` | 导出项目报告、附件和统计 |
| `archive` | 完成或归档项目 |

### 5.2 权限继承

继承规则：

- 项目 `read` 继承到项目内日志、Issue、计划、测试数据、经验候选。
- 项目写权限只对业务数据有效，不自动授予仪器控制权限。
- 仪器控制仍需独立的 `instrument` 权限和租约。
- 附件继承绑定对象权限。
- 报告继承项目读权限，但导出需要项目 `export` 或报告 `export`。
- 显式 deny 优先于 allow。

示例：用户只被加入 MNT 项目，则：

- 能看到 MNT 项目列表项、日志、Issue、计划、统计。
- 不能看到气体靶项目名称、日志正文、附件和测试数据。
- 搜索全站时不会返回气体靶项目内容。
- 周报页面只列出 MNT 或用户有权限的项目。

### 5.3 跨项目对象的可见性

跨项目关联不能破坏权限隔离。

规则：

- 用户必须有主项目 `read` 才能读完整对象。
- 只有关联项目权限、没有主项目权限时，只能看到脱敏引用，例如“有一条受限项目记录关联到本项目”。
- 附件按原对象权限控制，不因关联项目自动开放。
- 统计可以计入关联数量，但不能展开敏感内容。

### 5.4 Agent 权限

Agent 写入项目数据时必须同时满足：

```text
Agent 服务账号权限 ∩ acting_user_id 用户权限 ∩ 项目生命周期规则 ∩ Agent 硬限制
```

Agent 不得自动创建项目，也不得在项目不确定时自动入库正式日志。项目识别低置信时：

- 创建草稿。
- `project_id` 留空或填候选列表。
- 进入待确认中心。
- 用户确认后才成为正式项目日志。

## 6. 前端交互

### 6.1 日报录入

日报录入页面应从“写一整段日报”升级为“按项目记录当天事项”。

推荐交互：

- 顶部显示日期和作者。
- 下方是多条记录，每条记录都有项目选择器、分类、内容、附件。
- 项目选择器默认显示用户最近使用的项目。
- 支持输入项目别名、拼音、短码搜索。
- 支持“拆分为多条项目记录”：用户粘贴一段混合日报后，Agent 给出按项目拆分建议。
- 无项目或低置信项目时，条目标记为“待分类”。

项目选择器状态：

| 状态 | 行为 |
|------|------|
| 有明确项目 | 可直接提交 |
| 多个候选 | 要求用户选择 |
| 无权限项目 | 显示无权限，不允许写入 |
| 归档项目 | 默认隐藏，搜索可见但不可新增 |
| 公共事务项目 | 用于无法归入实验项目的维护、会议、采购等 |

日报提交时校验：

- 至少一条有效记录。
- 正式提交的条目必须有 `project_id`。
- 用户对每个项目有 `create_log` 权限。
- 项目不是 `archived`，且符合生命周期写入规则。

### 6.2 项目列表页

项目列表页是用户进入系统后的主要组织视图。

建议列：

- 项目名称、短码、状态。
- 负责人。
- 最近活动时间。
- 本周日志数。
- 未解决 Issue 数。
- 进行中计划数。
- 最近测试数据时间。

筛选：

- 我的项目。
- 活跃项目。
- 已完成项目。
- 归档项目。
- 有未解决问题。
- 标签和关键词。

用户只能看到自己有 `read` 权限的项目。admin 可切换“全部项目”视图。

### 6.3 项目仪表盘

项目仪表盘应回答“这个项目现在怎么样”。

建议区域：

- 项目概览：目标、负责人、成员、状态、起止日期。
- 今日/本周动态：按时间线展示日志、Issue、计划变更、测试数据。
- 关键指标：测试数据趋势、关键温度/压力/RF 指标、成功率等。
- Issue 看板：按状态和严重度分组。
- 计划与里程碑：最近任务、阻塞项、到期项。
- 实验批次：当前进行中的 `experiment_runs`。
- 附件和图片：最近上传、按对象分组。
- 周报入口：生成、查看、导出项目周报。

不要把项目仪表盘做成只读报告页。它应该能直接进入新增日志、创建 Issue、补录测试数据、更新计划等高频动作。

## 7. 周报与统计

### 7.1 项目周报

项目周报按 `project_id` 生成，时间范围默认自然周。

数据来源：

- 项目日志。
- Issue 状态变化。
- 计划任务和里程碑。
- 测试数据和实验批次。
- 经验候选和已发布经验。
- 人工补充说明。

周报结构建议：

- 本周完成事项。
- 关键实验结果。
- 主要数据趋势。
- 未解决问题和风险。
- 下周计划。
- 需要负责人决策的事项。
- 来源链接。

每个重要结论必须能点回原始记录。AI 生成的摘要要保留来源对象 ID 和生成时间。

### 7.2 统计口径

默认统计只看主项目：

- 日志数：`logs.project_id = project_id`。
- Issue 数：`issues.project_id = project_id`。
- 测试数据：`test_data.project_id = project_id`。
- 计划进度：`plans.project_id = project_id`。

可选统计口径：

| 口径 | 用途 |
|------|------|
| 主项目 | 项目 KPI、周报、权限默认视图 |
| 包含关联项目 | 查跨项目依赖和共享结果 |
| 按实验批次 | 查一次实验或一次降温的完整数据 |
| 按成员 | 查个人工作量和日报完成情况 |
| 按设备/仪器 | 查设备使用和异常 |

跨项目关联默认不重复计入项目 KPI，避免一条工作被多个项目重复计算。

### 7.3 测试数据统计

测试数据统计必须支持：

- 按项目筛选。
- 按实验批次筛选。
- 按数据类型筛选，例如 `cryogenic`、`pressure`、`rf_voltage`。
- 按时间范围筛选。
- 按设备或测点筛选。

建议给 `test_data` 增加统一字段：

| 字段 | 说明 |
|------|------|
| `project_id` | 项目归属 |
| `experiment_run_id` | 实验批次 |
| `instrument_id` | 来源仪器 |
| `measurement_name` | 测量项 |
| `measured_value` | 数值 |
| `unit` | 单位 |
| `quality_flag` | `normal`、`suspect`、`invalid` |
| `source` | `manual`、`instrument`、`import`、`agent` |

## 8. 从 SQLite 迁移

### 8.1 迁移原则

现有 SQLite 数据没有项目字段，因此不能假装所有数据都天然属于一个项目。迁移应分三层：

1. 能确定项目的，自动分配。
2. 不确定但有候选的，进入人工复核。
3. 无法判断的，先进入“未分类历史数据”或“实验室公共事务”项目。

所有迁移记录必须保留源表、源 ID、源数据库、源 hash 和迁移批次号，便于回滚和审计。

### 8.2 初始项目

迁移前先创建基础项目：

| 项目 | 用途 |
|------|------|
| 低温气体靶 | 当前 `gas_cell`、低温、气压、RF 匹配、装配等大部分历史数据 |
| MNT 反应 | MNT 相关日志、计划和模拟数据 |
| 新实验/待定项目 | 已知属于新方向但名称未稳定的数据 |
| 实验室公共事务 | 设备维护、环境、采购、会议、无法归入实验项目的内容 |
| 未分类历史数据 | 迁移无法自动判断且需要人工复核的数据 |

如果当前历史数据事实上主要都是气体靶，也可以先把大部分数据迁入“低温气体靶”，但必须生成复核清单，而不是静默迁移。

### 8.3 自动归类规则

建议按以下优先级归类：

1. 显式关键词：正文或名称包含项目别名，例如“气体靶”“MNT”“RF Carpet”“QPIG”。
2. 旧分类字段：`issues.category = gas_cell/rf/cryo` 优先映射到低温气体靶。
3. 旧数据表来源：`schema_gas_cell.sql` 中的 `test_data`、`rf_matching_circuits`、`assembly_process`、`cryo_runs` 默认候选低温气体靶。
4. 设备名或测量项：包含 RFQ、RF Carpet、腔体温度、真空、低温等候选低温气体靶。
5. 日期范围：如果某段时间只有一个项目活跃，可作为低置信辅助规则。
6. 人工确认：低置信或多候选必须人工确认。

迁移时给每条记录写入：

| 字段 | 说明 |
|------|------|
| `project_id` | 最终分配项目 |
| `migration_confidence` | `high`、`medium`、`low` |
| `migration_rule` | 命中的规则 |
| `needs_review` | 是否需要人工复核 |

### 8.4 daily_reports 迁移

旧 `daily_reports` 每天只有一条，没有作者。迁移策略：

- 创建一个系统迁移用户或默认实验室用户，例如 `usr_legacy_import`。
- 每条旧日报变成新 `daily_reports` 的一条记录。
- `summary` 保留原文。
- 同时创建一条或多条项目日志：
  - 如果 summary 可拆分项目，则拆成多条 `logs`。
  - 如果不能拆分，则创建一条日志，项目按自动归类规则分配。
  - 不确定项目进入“未分类历史数据”。

旧日报图片迁移为附件，绑定到新日报或对应日志。若无法判断图片属于哪条项目日志，先绑定日报，再人工整理。

### 8.5 issues 迁移

旧 `issues` 通过 `report_date` 关联日期。迁移策略：

- 根据 `category`、`issue_desc`、日期上下文归类项目。
- 保留旧 `report_date`。
- 创建 `issues.project_id`。
- 若旧日报当天已生成相关日志，则通过 `issue_log_links` 关联。
- 旧状态映射：
  - `unresolved -> open`
  - `in_progress -> in_progress`
  - `resolved -> resolved`

图片附件绑定到 Issue。

### 8.6 test_data 迁移

旧 `gas_cell.db` 中的测试数据默认候选“低温气体靶”。

迁移策略：

- `test_data.project_id` 默认低温气体靶。
- 根据连续日期、`data_type`、测量项和 `cryo_runs` 推断 `experiment_run_id`。
- 不能可靠归入批次的，保留 `experiment_run_id = NULL`，但项目必须填。
- `rf_matching_circuits` 可按日期和设备类型生成 RF 匹配实验批次。
- `assembly_process` 可生成装配类日志或计划任务完成记录。
- `cryo_runs` 直接迁移为 `experiment_runs`。

### 8.7 复核流程

迁移完成后生成复核页面：

- 按项目分组显示迁移数量。
- 按低置信记录列出待确认项。
- 支持批量改项目。
- 支持按日期、关键词、源表筛选。
- 修改迁移归属必须写审计事件。

迁移验收指标：

- 源表行数与目标表行数可解释一致。
- 附件路径存在。
- 每条正式业务记录都有 `project_id`，除全局经验、日报容器和草稿外。
- 低置信记录全部处理，或明确保留在“未分类历史数据”项目。

## 9. 关键问题评估

### 9.1 项目维度是否应可选

结论：对正式业务数据不应可选；对草稿入口可以临时可选。

具体规则：

| 对象 | 项目是否必填 | 原因 |
|------|--------------|------|
| `daily_reports` | 不必填 | 它是个人日容器，天然可包含多个项目 |
| `logs` / `daily_report_items` | 正式状态必填 | 权限、周报、统计都依赖项目 |
| `issues` | 必填 | 问题责任和可见性必须明确 |
| `test_data` | 必填 | 数据统计和权限必须明确 |
| `plans` | 必填 | 计划必须服务某个项目 |
| `experiences` | 可为空，但仅限全局经验 | 全局经验必须脱敏并审核 |
| `attachments` | 不直接必填 | 继承绑定对象权限 |

为了不影响录入体验，可以提供：

- “待分类”草稿。
- “实验室公共事务”项目。
- Agent 候选项目推荐。

但提交正式日报、生成周报、计入项目统计前，应要求项目归属明确。

### 9.2 是否现在做子项目/实验批次层级

结论：不要现在做完整“项目 -> 子项目 -> 实验批次 -> 单次测试”的通用层级；现在只做项目和实验批次。

原因：

- 当前最明确的需求是多项目并行和项目级权限。
- 子项目边界容易随实验推进变化，过早建层级会增加录入负担。
- 实验数据真正需要的是“同一次实验/降温/扫描”的批次归集，不一定是管理意义上的子项目。
- 完整层级会让权限、周报和统计口径显著复杂化。

第一阶段推荐层级：

```text
project
  ├── logs
  ├── issues
  ├── plans
  ├── experiences
  └── experiment_runs
        └── test_data / rf_matching / cryo data
```

预留扩展：

- `projects.parent_project_id` 暂不启用，等确实出现长期稳定子项目再加。
- `experiment_runs.parent_run_id` 暂不启用，等一次实验内部确实需要多级批次再加。
- 用 `tags`、`milestones`、`experiment_runs` 先覆盖大多数分类需求。

## 10. 实施顺序

建议分四步落地：

1. 增加 `projects`、`project_members`、项目 ACL 和项目列表页。
2. 给日志、Issue、测试数据、计划增加 `project_id`，日报改为“日容器 + 项目条目”。
3. 增加项目仪表盘、项目周报和项目统计。
4. 做 SQLite 迁移、复核页面和归档策略。

第一阶段最小可用范围：

- 创建项目。
- 给用户分配项目权限。
- 录入日报时选择项目。
- Issue 和测试数据带项目。
- 按项目筛选日志、Issue、测试数据。
- 迁移旧数据到默认项目并生成复核清单。

不要在第一阶段同时实现复杂子项目、多级组织、细粒度继承覆盖和跨项目自动授权。这些可以预留表和接口，但不要让录入流程先背上复杂度。
