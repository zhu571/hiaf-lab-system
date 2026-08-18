# 用户与项目成员管理前端实现方案

> 状态：待评审  
> 范围：仅前端设计，不新增或修改后端 API  
> 依据：当前仓库代码、`docs/api-contract.md`、`docs/permission-audit.md`、`docs/project-design.md`

## 1. 结论

采用最小改动方案：不新增路由、Pinia store、依赖或通用组件。

- 在现有 `ProjectDashboard.vue` 的“项目成员”卡片内补齐添加、改角色、移除操作。
- 在现有 `AdminUsersView.vue` 增加概览、筛选、资料编辑和“创建后加入项目”，把账号创建与项目授权串成一个流程。
- 复用 `FormDialog`、`ResponsiveTable`、`StateBlock`、`showApiError`、`requestWithMeta`、当前项目 store 和既有设计令牌。
- 保留现有 AI 助手入口、导航结构、项目切换机制和移动端布局。
- 所有成员写操作完成后只重新加载成员列表；用户写操作完成后只重新加载用户列表，不做乐观更新，避免权限或 `last_owner` 校验失败时回滚本地状态。

建议交付范围为 6 个源码文件和 3 个既有测试文件的增量修改，不新建业务组件。若 `ProjectDashboard.vue` 后续继续膨胀，再按实际复用需求拆出成员组件，本次不预建抽象。

## 2. 需求分析

### 2.1 用户任务

| 用户 | 任务 | 结果 |
|---|---|---|
| admin | 创建账号并交付临时密码 | 新用户可登录并被强制改密 |
| admin | 创建账号后立即加入当前或指定项目 | 一次流程完成“账号 + 项目角色”配置 |
| 项目成员管理员 | 将已有账号加入当前项目 | 成员列表立即刷新 |
| 项目成员管理员 | 修改项目角色 | 新角色立即生效并显示 |
| 项目成员管理员 | 移除成员 | 成员从当前项目移除，账号本身保留 |
| 普通成员/viewer | 查看项目成员 | 只能查看，无管理入口 |
| 开放注册用户 | 自助注册并登录 | 注册成功后等待项目 owner/admin 加入项目 |

系统角色与项目角色必须分开表达：

- 系统角色：`admin / maintainer / member / viewer`，决定全局后台能力。
- 项目角色：`owner / maintainer / member / viewer`，只作用于一个项目。
- 用户列表中的“维护者”不是某项目的“维护者”；界面必须分别标为“系统角色”和“项目角色”，避免误解。

### 2.2 当前能力与缺口

| 项目 | 当前状态 | 本方案 |
|---|---|---|
| 用户创建、列表、更新、重置密码 | API 与页面均已有 | 精修交互，不改契约 |
| 自助注册 | 登录页已有；后端由 `ALLOW_REGISTER` 控制 | 保留入口，细化关闭时提示 |
| 项目成员列表 | 已展示，但只有只读行 | 增加完整管理操作 |
| 成员写 API | 已有，`projects.ts` 尚未封装 | 增加 3 个 API 函数 |
| 当前用户的项目角色 | 无独立字段 | 用成员列表中 `user_id === auth.user.id` 推导 |
| 可选用户目录 | 只有 admin 可调用 `GET /admin/users` | admin 使用可搜索选择器；非 admin 使用精确用户 ID 基线 |
| 成员姓名 | 当前成员响应不含用户名/显示名 | 基线展示 `user_id`；admin 可用用户列表在前端映射姓名 |
| maintainer 管成员 | 需求说明允许；当前 Go 权限表未授予 | 见 §10.1 一致性风险 |

### 2.3 非目标

- 不提供删除账号、批量导入、邀请邮件、邀请码、LDAP、项目权限细粒度覆盖或成员状态管理。
- 不修改注册开关的部署配置，也不新增“读取注册开关”接口。
- 不增加独立“项目成员管理”路由；成员管理属于当前项目上下文。
- 不把用户列表或成员列表放入全局 store；它们仍是页面本地状态。
- 不复制或展示 access token。当前 API 只返回临时密码，没有“复制 token”契约。

## 3. 页面设计

### 3.1 项目仪表盘：成员卡片

保留仪表盘现有布局，将“项目成员”卡片升级为可管理区。

#### 结构

```text
项目成员（6）                         [添加成员]
搜索成员……                 [全部角色 ▼]
------------------------------------------------
张三 / usr_xxx       成员          [改角色] [移除]
李四 / usr_yyy       维护者        [改角色] [移除]
王五 / usr_zzz       负责人        [改角色] [移除]
```

- 标题显示当前成员数。
- 管理按钮仅在 `canManageMembers` 为真时出现。
- 成员少时直接列表展示；搜索和角色筛选均为本地计算，不新增请求或分页。
- desktop 行内显示角色和操作；mobile 使用现有卡片宽度，操作按钮换行，不增加抽屉或新的响应式组件。
- 成员名展示优先级：`display_name` → `username` → `user_id`。前两个字段仅在 admin 通过用户列表映射成功时存在，其他情况始终保留完整 `user_id`，并提供复制按钮。
- 角色使用 `StatusBadge`/现有 Element Plus tag 色系；颜色只引用既有 token，不写 hex/rgba。
- 加载、空、错误分别显示 skeleton/`el-empty`/卡片内 `el-alert + 重试`，成员请求失败不阻塞日志和 Issue 区块。

#### 添加成员

点击“添加成员”打开 `FormDialog`：

1. 用户字段：
   - admin：调用既有 `listUsers()`，使用可搜索 `el-select`；候选排除 `agent`、已停用用户和当前项目已有成员。
   - 非 admin 的 owner/maintainer：当前无用户目录 API，显示“用户 ID”输入框，支持粘贴精确 ID。
2. 项目角色：`owner / maintainer / member / viewer`，默认 `member`。
3. 角色选项下方显示简短权限说明，不用 tooltip 承载关键含义。
4. 前端校验用户非空、角色属于固定枚举；提交期间禁用关闭和重复提交。
5. 调用 `POST /projects/{id}/members`。成功后关闭弹窗、刷新成员列表，并显示成功消息和 `request_id`。
6. `user_not_found`、403、网络错误等统一通过 `showApiError` 展示，弹窗保留输入以便修正。

如果 admin 在选择器中找不到用户，弹窗提供“前往用户管理”链接；不把创建账号表单复制到项目仪表盘。

#### 修改角色

- 点击“改角色”打开 `FormDialog`，显示目标用户和当前角色。
- 新角色与当前角色相同时禁用提交。
- 若目标是当前唯一 owner，前端禁用降级选项并显示原因；后端 `last_owner` 仍是最终安全边界。
- 提交前用 `ElMessageBox.confirm` 明确“项目角色变更会立即影响访问权限”。
- 调用 `PATCH /projects/{id}/members/{userID}`，成功后刷新并显示 `request_id`。

#### 移除成员

- 使用危险样式按钮和二次确认，确认文案同时包含用户标识与项目名。
- 当前唯一 owner 的移除按钮禁用；其他 owner 仍可移除，后端再次校验。
- 移除当前登录用户时额外提示“操作成功后你将失去当前项目访问权限”，成功后调用 `projectStore.load()` 并跳转 `/projects`。
- 调用 `DELETE /projects/{id}/members/{userID}`，成功后刷新成员列表并显示 `request_id`。

### 3.2 AdminUsersView：精细化用户管理

仍使用现有 `/admin/users` 路由和 `ListPage`，不改导航。

#### 页面结构

```text
用户管理                                  [刷新] [新建用户]
[总用户 18] [活跃 16] [待改密 3] [已停用 2]

搜索用户名或显示名…  [系统角色 ▼] [账号状态 ▼]
----------------------------------------------------------------
用户       系统角色   状态    密码状态   创建时间       操作
张三       成员       活跃    待首次改密  2026-08-01   [编辑] […]
```

- 四个概览数均由已加载列表本地计算，不增加统计 API。
- 筛选包含关键词、系统角色、账号状态；提供“清空筛选”。
- 列表增加“密码状态”，显示 `must_change_password`；日期继续复用 `formatDate`。
- 行主操作为“编辑”，次操作收进 `el-dropdown`：重置密码、加入项目、停用/启用。移动端继续复用 `ResponsiveTable` 的 card slot。
- 当前账户保留只读提示，不展示会被后端 `cannot_modify_self` 拒绝的操作。
- agent 账号继续从页面排除，且不可创建或分配。

#### 新建/编辑用户

“新建用户”继续使用 `FormDialog`，补充以下细节：

- 用户名 trim 后必填，长度 2–32；显示名 trim；系统角色固定枚举；初始密码留空则由后端生成，填写时至少 8 位。
- 增加“创建后加入项目”开关。开启后显示项目选择器和项目角色；默认项目为 `projectStore.current`，默认项目角色为 `member`。
- 先调用创建用户 API；成功后使用返回的 `user.id` 调用添加成员 API。两个请求分别有独立幂等键和 `request_id`。
- 若账号创建成功而加项目失败，不回滚或重复创建账号。结果弹窗明确显示“账号已创建，加入项目失败”，保留用户 ID、临时密码、创建请求 ID，并提供“重试加入项目”。重试只调用成员 API。
- 临时密码只在成功响应弹窗展示一次，继续支持 HTTPS Clipboard API 与现有 HTTP fallback。关闭弹窗前若未复制，二次提醒。
- 编辑弹窗合并显示名、系统角色、启用状态；只提交发生变化的字段，成功后刷新列表。

#### 将已有用户加入项目

用户行的“加入项目”打开同一页面内的轻量弹窗：

- 用户固定为当前行。
- 项目从 `projectStore.projects` 选择，默认当前项目。
- 项目角色默认 `member`。
- 提交调用现有成员添加 API。后端的 upsert 行为会把已存在成员改为新角色，因此当目标已在项目中时，前端先根据已知数据提示；无法预知时仍以服务端结果为准。

不设计批量勾选：当前团队规模和接口均适合逐个配置，批量操作会增加部分成功与回滚语义。

### 3.3 注册到入项目的完整流程

提供两条路径：

1. 管理员建号（默认、推荐）
   - admin 打开用户管理 → 新建用户 → 勾选“创建后加入项目” → 选择项目和角色 → 创建。
   - 页面依次创建账号、加入项目 → 展示临时密码和两个请求 ID → admin 安全地交付临时密码。
   - 新用户登录 → 按现有守卫强制修改密码 → 可访问已加入项目。
2. 自助注册（部署开启 `ALLOW_REGISTER=true` 时）
   - 用户在登录页注册并自动登录。
   - 新账号没有项目时显示现有空项目态，并提示“请联系项目负责人或管理员加入项目”。
   - owner/admin 在成员卡片通过用户 ID添加；admin 也可在用户管理页搜索该账号并加入项目。

后端未提供注册开关读取接口，因此前端不能可靠地预先隐藏注册入口。关闭注册时保留现有入口，收到 `registration_disabled` 后显示“当前未开放自助注册，请联系管理员创建账号”，这是无新 API 前提下的唯一可靠行为。

## 4. 组件与状态设计

### 4.1 复用组件

| 能力 | 复用项 |
|---|---|
| 页面骨架与错误重试 | `ListPage`、`StateBlock` |
| desktop/mobile 列表 | `ResponsiveTable` |
| 新建、编辑、加成员、改角色 | `FormDialog` |
| 确认 | `ElMessageBox.confirm` |
| 操作反馈与 request_id | `showApiError`、`ElMessage`、`requestWithMeta` |
| 日期 | `formatDate` / `formatDateTime` |
| 当前身份与项目 | `useAuthStore`、`useProjectStore` |

### 4.2 页面本地状态

`ProjectDashboard.vue` 增加：

- `memberLoading / memberError`：从仪表盘其他区块的加载状态拆开。
- `memberKeyword / memberRoleFilter`。
- `addMemberVisible / editMemberVisible / memberSaving / memberTarget`。
- `adminUsers`：仅 admin 打开添加弹窗时按需加载；不进入 Pinia。
- `currentProjectMember`、`ownerCount`、`canManageMembers` 计算属性。

`AdminUsersView.vue` 增加：

- `roleFilter / statusFilter` 和本地统计计算属性。
- 统一的 `editTarget / editDraft`，替代仅改角色弹窗。
- `joinProjectVisible / joinTarget / joinDraft`。
- `createResult`：保存账号、临时密码、创建请求 ID、加入项目请求 ID 和加入状态，支持仅重试第二步。

所有弹窗关闭时重置草稿与字段错误；项目切换时关闭成员弹窗，避免把旧项目表单提交到新项目。

## 5. API 对接

写请求继续由 `client.ts` 自动添加 `Idempotency-Key` 和 CSRF header；组件不自行生成 token。为满足写操作展示 `request_id` 的项目约定，本功能涉及的写方法使用 `requestWithMeta`。

### 5.1 项目成员 API

在 `src/api/projects.ts` 增加：

| 前端函数 | 方法与路径 | 入参 | 返回 data |
|---|---|---|---|
| `addMember(projectId, data)` | `POST /projects/{id}/members` | `{user_id, role}` | `ProjectMember` |
| `updateMemberRole(projectId, userId, role)` | `PATCH /projects/{id}/members/{userID}` | `{role}` | `ProjectMember` |
| `removeMember(projectId, userId)` | `DELETE /projects/{id}/members/{userID}` | 无 | `{success: true}` |

同时把 `ProjectMember` 类型与真实响应对齐：`project_id`、`user_id`、`role`、`status`、`muted`、`joined_at`、`added_by`；不假定后端返回 `username`。

### 5.2 用户 API

`GET /admin/users` 保持 `request()`。本功能涉及的 `createUser`、`updateUser`、`resetPassword` 改用 `requestWithMeta()`，并同步唯一调用方 `AdminUsersView.vue`：

```ts
const { data, requestId } = await createUser(payload)
```

这样成功和失败都能显示请求 ID；失败继续由 `ApiError.requestId` 透传。

### 5.3 错误处理

| HTTP / code | UI 行为 |
|---|---|
| 400 `missing_idempotency_key` | 通用错误；视为客户端缺陷，不自动重试 |
| 400 `last_owner` | 保留弹窗并提示先指定另一位 owner |
| 400 `cannot_modify_self` | 刷新用户列表；正常 UI 不应触发 |
| 403 `permission_denied` | 提示权限已变化，关闭写弹窗并刷新成员 |
| 403 `registration_disabled` | 登录页显示联系管理员文案 |
| 404 `user_not_found` | 添加弹窗保留，聚焦用户字段 |
| 409 `username_taken` | 创建弹窗保留，聚焦用户名 |
| 409 `last_active_admin` | 用户编辑弹窗保留，提示先配置另一位 admin |
| 409 `duplicate_idempotency_key` | 显示已有请求 ID，不自动重复写 |
| 网络/5xx | 保留草稿，允许用户主动重试 |

写请求不自动重试。创建账号后加项目失败的场景只重试第二步，避免重复账号。

## 6. i18n 文案清单

沿用现有 `adminUsers` 和 `projectDashboard` 命名空间；共用按钮使用已有 `common.*`。以下为新增/调整的关键文案，`zh.ts` 与 `en.ts` 必须同一提交保持完全同构。

### 6.1 `projectDashboard.memberManagement`

| key | 中文 | English |
|---|---|---|
| `add` | 添加成员 | Add member |
| `search` | 搜索成员或用户 ID | Search member or user ID |
| `allRoles` | 全部项目角色 | All project roles |
| `user` | 用户 | User |
| `userId` | 用户 ID | User ID |
| `userIdPlaceholder` | 粘贴用户 ID | Paste a user ID |
| `userIdHelp` | 请向管理员或用户本人获取准确的用户 ID | Ask an admin or the user for the exact user ID |
| `projectRole` | 项目角色 | Project role |
| `editRole` | 修改角色 | Change role |
| `remove` | 移除 | Remove |
| `copyId` | 复制 ID | Copy ID |
| `roleOwnerHelp` | 可管理项目和成员 | Can manage the project and members |
| `roleMaintainerHelp` | 可维护项目业务数据 | Can maintain project data |
| `roleMemberHelp` | 可参与项目工作 | Can contribute to the project |
| `roleViewerHelp` | 只读访问 | Read-only access |
| `addSuccess` | 成员已添加（request_id: {requestId}） | Member added (request_id: {requestId}) |
| `updateSuccess` | 项目角色已更新（request_id: {requestId}） | Project role updated (request_id: {requestId}) |
| `removeSuccess` | 成员已移除（request_id: {requestId}） | Member removed (request_id: {requestId}) |
| `removeConfirm` | 确认将“{user}”移出项目“{project}”？账号本身不会被删除。 | Remove “{user}” from “{project}”? The account will not be deleted. |
| `removeSelfWarning` | 移除后你将失去当前项目访问权限。 | You will lose access to this project. |
| `lastOwnerHint` | 项目必须至少保留一位负责人 | A project must keep at least one owner |
| `directoryUnavailable` | 无可用用户目录，请输入准确的用户 ID | User directory unavailable; enter the exact user ID |
| `goToUsers` | 前往用户管理 | Go to user management |
| `loadFailed` | 项目成员加载失败 | Failed to load project members |

现有 `roleOwner / roleMaintainer / roleMember / roleViewer` 继续复用，不重复建 key。

### 6.2 `adminUsers`

| key | 中文 | English |
|---|---|---|
| `summaryTotal` | 总用户 | Total users |
| `summaryActive` | 活跃 | Active |
| `summaryMustChangePassword` | 待改密 | Password change required |
| `summaryDisabled` | 已停用 | Disabled |
| `filterRole` | 全部系统角色 | All system roles |
| `filterStatus` | 全部账号状态 | All account statuses |
| `clearFilters` | 清空筛选 | Clear filters |
| `tablePasswordStatus` | 密码状态 | Password status |
| `mustChangePassword` | 待首次改密 | Change required |
| `passwordReady` | 已设置 | Ready |
| `editUser` | 编辑用户 | Edit user |
| `systemRole` | 系统角色 | System role |
| `joinProject` | 加入项目 | Add to project |
| `project` | 项目 | Project |
| `projectRole` | 项目角色 | Project role |
| `createAndJoin` | 创建后加入项目 | Add to a project after creation |
| `accountCreated` | 账号已创建 | Account created |
| `accountCreatedAndJoined` | 账号已创建并加入项目 | Account created and added to project |
| `joinFailedAfterCreate` | 账号已创建，但加入项目失败。请保存临时密码后重试加入。 | Account created, but adding it to the project failed. Save the temporary password and retry. |
| `retryJoin` | 重试加入项目 | Retry adding to project |
| `createRequestId` | 创建请求 ID | Creation request ID |
| `joinRequestId` | 加入项目请求 ID | Membership request ID |
| `unsavedPasswordConfirm` | 尚未复制临时密码，确认关闭？关闭后无法再次查看。 | The temporary password has not been copied. Close anyway? It cannot be viewed again. |
| `joinSuccess` | 已加入项目（request_id: {requestId}） | Added to project (request_id: {requestId}) |
| `noProjects` | 暂无可选项目 | No projects available |

### 6.3 `login`

| key | 中文 | English |
|---|---|---|
| `registrationDisabled` | 当前未开放自助注册，请联系管理员创建账号。 | Self-registration is unavailable. Contact an administrator to create an account. |
| `waitingForProject` | 账号已创建；请联系项目负责人或管理员加入项目。 | Your account is ready. Ask a project owner or administrator to add you to a project. |

动态角色 key 禁止模板字符串拼接，继续使用显式对象映射。

## 7. 权限控制

### 7.1 前端判定

```ts
const currentProjectMember = members.find((m) => m.user_id === auth.user?.id)
const canManageMembers = auth.isAdmin || ['owner', 'maintainer'].includes(currentProjectMember?.role || '')
```

| 身份 | 查看成员 | 添加/改角色/移除 | 用户管理页 |
|---|---:|---:|---:|
| system admin | 是 | 是 | 是 |
| project owner | 是 | 是 | 否 |
| project maintainer | 是 | 按需求应为是；见 §10.1 | 否 |
| project member/viewer | 是 | 否 | 否 |
| 非项目成员 | 后端拒绝 | 否 | 仅 admin 可进 |

按钮隐藏只用于 UX。所有请求仍由后端认证、项目权限、CSRF、幂等和审计中间件强校验。

### 7.2 防止权限漂移

- 不用系统 `user.role === 'maintainer'` 推断项目权限；必须读取当前项目成员角色。
- 每次项目切换重新加载成员并重新计算权限。
- 收到 403 后关闭管理弹窗并刷新成员，避免保留过期授权 UI。
- admin 路由继续使用 `meta.admin` 与现有导航 `minRole: 'admin'`，不新增守卫规则。
- 不在前端缓存“允许管理成员”的持久标志。

## 8. 改动文件清单

| 文件 | 改动 |
|---|---|
| `web-ui/src/api/projects.ts` | 补齐成员字段；增加 add/update/remove；写请求返回 meta |
| `web-ui/src/api/auth.ts` | 用户管理写请求改用 `requestWithMeta` |
| `web-ui/src/components/business/ProjectDashboard.vue` | 成员筛选、权限判定、三个管理流程及独立状态 |
| `web-ui/src/views/AdminUsersView.vue` | 概览、筛选、编辑、加入项目、创建后加入与部分成功处理 |
| `web-ui/src/i18n/zh.ts` | 新增中文 key |
| `web-ui/src/i18n/en.ts` | 同构英文 key |
| `web-ui/src/api/__tests__/contract.test.ts` | 校验三个成员写 API 的方法、路径和 body |
| `web-ui/src/components/business/__tests__/projectDashboard.test.ts` | 权限显隐、添加、改角色、移除、最后 owner 保护、项目切换 |
| `web-ui/src/views/__tests__/adminUsersView.test.ts` | 筛选、编辑、创建后加入、部分成功与 request_id |

不修改 `router/index.ts`、`config/navigation.ts`、两个 Pinia store 或基础组件；现有结构已覆盖需求。

## 9. 实现步骤

1. 在 `projects.ts` 增加成员写 API 和真实类型；在 API 契约测试中锁定请求形态。
2. 将用户管理写 API 改为返回 `{data, requestId}`，同步 `AdminUsersView` 现有调用与测试。
3. 先实现 `ProjectDashboard` 的独立成员加载、权限计算、添加/改角色/移除及项目切换清理。
4. 精修 `AdminUsersView` 的本地统计、筛选、统一编辑弹窗和加入项目操作。
5. 在创建用户结果弹窗中串联可选的加入项目步骤，并实现“账号成功、成员失败”的可恢复状态。
6. 补齐中英文 key，运行 i18n 双向 key 测试；检查模板中无硬编码新增文案。
7. 补齐组件测试和 API 契约测试，再执行全量前端测试、覆盖率、构建和静态产物同步。

## 10. 风险与处理

### 10.1 当前权限实现与需求不一致

已按目标权限对齐：`go-server/middleware/permission.go` 的项目 `maintainer` 具有 `PermManageMembers`，API 契约与权限测试同步更新；前端按 `owner / maintainer / admin` 显示管理入口。

### 10.2 非 admin 缺少用户目录

`GET /api/v1/admin/users` 仅 admin 可用，成员列表又不返回姓名。因此 owner/maintainer 的无后端改动基线只能按精确 `user_id` 添加，体验弱但功能可完成。

可选后端优化：提供受权限保护的最小用户搜索接口，或让成员列表返回 `username/display_name`。返回字段只应包含 ID、用户名、显示名、disabled，不暴露系统角色、登录信息或凭据。本方案不依赖该优化上线。

### 10.3 注册开关不可探测

当前没有公开的注册配置读取接口，前端不能预先隐藏入口。基线以 `registration_disabled` 响应为准。可选后端优化是把 `allow_register` 放入公开运行时配置端点；没有实际隐藏需求时不必新增。

### 10.4 两步创建不是事务

“创建账号 + 加入项目”由两个既有 API 组成，无法原子提交。不得因第二步失败删除账号或自动重放第一步；结果弹窗必须保留临时密码和 user ID，并只允许重试成员步骤。

### 10.5 角色与最后 owner

前端的唯一 owner 禁用逻辑只减少误操作，存在并发变化。后端 `last_owner` 校验是最终边界；收到错误后刷新列表。

### 10.6 大列表

用户与成员接口目前无前端分页契约，本方案采用本地筛选。团队用户达到约 500 或出现明显渲染延迟时，再设计服务端分页/搜索；当前不引入虚拟列表。

### 10.7 可访问性与视觉

- 所有图标按钮有可见文本或 `aria-label`；危险操作不只靠颜色区分。
- 键盘可完成选择、提交和取消；焦点沿用全局 `:focus-visible`。
- 新样式只用 `tokens.css` / dark theme 令牌，并遵守 reduced-motion。
- 管理弹窗宽度使用响应式 `min(..., 92vw)`；移动端不产生横向滚动。

## 11. 验证方式

### 11.1 自动化测试

API 契约测试至少断言：

- 三个成员写函数的 method、URL、body 正确，并走 `requestWithMeta`。
- 用户写 API 返回 meta 后，调用方正确读取 `data` 与 `requestId`。

组件测试至少覆盖：

- admin/owner/maintainer 与 member/viewer 的管理入口显隐。
- 项目切换后成员重新加载、旧弹窗关闭。
- admin 候选用户排除 agent、disabled 和已加入成员。
- 添加、改角色、移除成功后刷新；错误时保留草稿并显示 request_id。
- 唯一 owner 不能在 UI 中被降级或移除，服务端 `last_owner` 仍能正确展示。
- 创建用户后加入项目成功；账号成功而加项目失败时不再次调用创建 API。
- 当前账户无自修改危险操作；筛选和空状态正确。
- 中英文 key 双向一致，无动态拼接 key。

执行：

```bash
cd web-ui
npm test
npm run test:coverage
npm run build
grep -rnE '#[0-9a-fA-F]{3,8}\b|rgba?\(' src --include='*.vue'
```

构建后按仓库现有流程全量同步 `dist` 到 `go-server/static/`，再确认静态产物一致性。

### 11.2 手工验收矩阵

| 场景 | 期望 |
|---|---|
| admin 创建用户并加入当前项目 | 一次完成，临时密码和两个 request_id 可见 |
| 创建成功、加入失败 | 账号保留，只重试加入步骤 |
| owner 添加已存在用户 | 成员出现，角色正确 |
| maintainer 管理成员 | 仅在 §10.1 权限对齐后成功 |
| member/viewer 访问仪表盘 | 可看成员，无写入口；手工构造请求仍被后端拒绝 |
| 修改成员角色 | 刷新后角色生效 |
| 移除普通成员 | 成员消失，用户账号仍在 |
| 降级/移除唯一 owner | UI 阻止；并发情况下后端返回 `last_owner` |
| 移除自己 | 警告后跳回项目列表，当前项目不再可访问 |
| 注册关闭 | 显示联系管理员提示，不误报网络错误 |
| 注册开启 | 注册并登录成功；无项目时显示等待加入提示 |
| 中文/English 切换 | 新增界面无硬编码、无缺 key |
| 手机宽度与深色模式 | 无横向滚动，焦点、状态和危险操作可辨识 |

## 12. 验收标准

- admin 能在用户页完成创建账号、获得临时密码并加入一个项目。
- 有实际后端权限的项目成员管理员能在当前项目添加、改角色和移除成员。
- 普通成员/viewer 不显示成员写入口，后端仍拒绝越权请求。
- 所有新增文案中英文齐全；写操作的成功或失败反馈包含 `request_id`。
- 列表具备加载、空、错误状态；mobile、dark mode、键盘操作可用。
- 不新增后端 API、路由、store、依赖或不必要的组件抽象。
