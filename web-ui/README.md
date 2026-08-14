# HIAF Lab System 前端代码阅读指南

本文面向有 Vue 3 基础、但第一次接触本项目的开发者。当前前端使用 Vue 3（Composition API + `<script setup>`）、TypeScript、Element Plus（按需自动注册）、Pinia、Vue Router、vue-i18n（中/英）和 Vite（路由级懒加载 + vendor 分包的多 chunk 构建）。建议按“入口 → 路由 → 布局 → store → API → 页面”的顺序阅读。

> 本文以当前落地代码为准。项目权限由路由守卫和后端共同校验；不要使用已经废弃的 `RequireProjectAccess` 写法。

## 1. 项目结构概览

```text
web-ui/
├── index.html                    # Vite HTML 入口，提供 #app 挂载点和移动端 viewport（viewport-fit=cover）。
├── package.json                  # 依赖和 dev/build/preview 脚本。
├── package-lock.json             # npm 锁文件，保证依赖安装可复现。
├── tsconfig.json                 # TypeScript 严格模式和 Vue 文件检查范围。
├── vite.config.ts                # Vue 插件、Element Plus 按需注册、vendor 分包、开发服务器及 API 代理。
├── vitest.config.ts              # 前端单测配置（jsdom + vue 插件 + coverage 窄口径门禁）。
├── public/
│   └── sw.js                     # 历史 PWA Service Worker 的“自杀式”清理脚本（复制进 dist，不做 PWA）。
├── e2e/                          # Playwright 冒烟（11 spec 13 用例）。
└── src/
    ├── main.ts                   # 创建 Vue 应用：注册 Pinia、Router、i18n；setupChartDefaults + useTheme 激活。
    ├── App.vue                   # 根组件；公开页直接渲染，业务页进入 AppLayout。
    ├── env.d.ts                  # 声明 *.vue 模块，使 TypeScript 能识别 SFC。
    ├── api/
    │   ├── client.ts             # axios 实例、CSRF/幂等键请求拦截器、运行时校验/401 单飞刷新/错误分类/网络重试。
    │   ├── auth.ts               # 登录、刷新、用户管理、改密、语言偏好 API。
    │   ├── projects.ts           # 项目及项目成员 API 和类型。
    │   ├── logs.ts               # 日报、项目日志、AI 解析（aiParseReport）API。
    │   ├── issues.ts             # Issue、状态流转和评论 API 和类型。
    │   ├── experiences.ts        # 经验的查询、创建、发布和归档 API。
    │   ├── audit.ts              # 审计：按 request_id 查链路（getAudit）+ 事件列表（listAuditEvents）。
    │   ├── agent.ts              # Agent 候选（listAgentCandidates/approve/reject）和动作时间线（getCandidateTrace）。
    │   ├── instruments.ts        # 仪器列表、白名单、命令执行、AI 指令解释（interpretCommand）、压电/气室控制。
    │   ├── sensors.ts            # 传感器最新值与历史查询（getLatest/getHistory）。
    │   ├── runs.ts               # 实验运行、状态流转、运行步骤（listRunSteps/applyRunTemplate 等）。
    │   ├── rfmatch.ts            # RF 匹配记录的增删改查。
    │   ├── testdata.ts           # 测试数据 CRUD。
    │   ├── stepTemplates.ts      # 步骤模板 CRUD + AI 生成步骤（generateSteps）。
    │   ├── assembly.ts           # 装配步骤、状态流转、排序、模板应用。
    │   ├── todos.ts              # 待办 CRUD + LLM 解析/添加 + ntfy 订阅（getNotificationTopic/provisionTopic/redeemTopic）。
    │   ├── attachments.ts        # 附件上传（uploadAttachment）、列表、blob 下载、实体关联。
    │   └── system.ts             # 版本查询、触发系统更新、更新日志 SSE 流（connectUpdateStream）。
    ├── components/
    │   ├── base/                 # 通用基础件（无业务语义、props/slots 驱动）：
    │   │   ├── StatusBadge.vue   # 状态→UI 映射（tone/labelKey 查 utils/statusMeta.ts 注册表）。
    │   │   ├── StateBlock.vue    # 加载骨架/错误重试/空态三态收口。
    │   │   ├── FormDialog.vue    # 表单弹窗（label-position=top + 取消/确定）。
    │   │   └── ResponsiveTable.vue  # 响应式表格：桌面 el-table，移动端 card 插槽卡片列表。
    │   └── business/             # 业务复合件（可直读 store/API）：
    │       ├── SensorTrendChart.vue  # 传感器趋势图（独立 script 导出窗口数学纯函数，可单测）。
    │       ├── AskDialog.vue     # AI 问答抽屉（模块级单例开关）。
    │       ├── AskResultPanel.vue   # 问答结果表格与来源跳转。
    │       ├── ProjectDashboard.vue # 项目阶段流程图。
    │       ├── ProjectSidebar.vue   # 可搜索项目列表（仅 ProjectsView 使用）。
    │       ├── CommentSection.vue   # 评论列表与输入框（submit 事件交给父页）。
    │       ├── MarkdownView.vue  # markdown-it 只读渲染（html:false 防注入）。
    │       ├── StepItemsEditor.vue  # 步骤列表编辑器。
    │       ├── TestDataBatchEditor.vue  # 批量录入编辑器。
    │       └── InstrumentAiChat.vue   # 仪器 AI 对话区。
    ├── composables/
    │   ├── useMobile.ts          # 统一提供 768px 移动端媒体查询（VueUse useMediaQuery）。
    │   ├── useNotify.ts          # showApiError：按错误分类展示后端错误并附带 request_id。
    │   ├── useAskDialog.ts       # AskDialog 全局开关（模块级单例）。
    │   ├── askRoutes.ts          # AI 问答结果→路由映射纯函数（hasRowRoute/canOpenRow/tableToRoute）。
    │   ├── useAsyncData.ts       # 异步加载封装（seq 竞态丢弃 + unmount 后忽略回写）。
    │   ├── usePolling.ts         # 轮询封装（visibilitychange 暂停 + unmount 清理）。
    │   ├── usePagination.ts      # 分页状态封装。
    │   ├── useRunSteps.ts        # RunDetailView 步骤状态机与模板应用逻辑。
    │   ├── useRunReports.ts      # RunDetailView 日报关联逻辑。
    │   └── useTheme.ts           # 主题单例（light/dark/auto，@vueuse useColorMode）。
    ├── config/
    │   └── navigation.ts         # 导航单一数据源：NAV_ITEMS + filterNavByRole 角色过滤。
    ├── i18n/
    │   ├── index.ts              # createI18n：locale 优先级为 后端 user.language > localStorage > 中文。
    │   ├── zh.ts                 # 中文文案基准。
    │   └── en.ts                 # 英文文案，与 zh.ts 保持同一 key 结构（keys.test.ts 双向比对）。
    ├── layouts/
    │   ├── AppLayout.vue         # 登录后外壳：桌面侧栏、移动端顶栏/底栏、RouterView 容器、Agent 待审徽章。
    │   ├── MobileTopBar.vue      # 移动端顶部栏：返回键 + 基于 meta.titleKey 的标题。
    │   ├── ProjectLayout.vue     # 项目工作区壳：项目头、Tab 切换（仪表盘/Issue/实验运行/测试数据/RF匹配/装配）。
    │   └── DailyReportShell.vue  # 日报壳：今日录入/历史查询两个子路由的 Tab 容器。
    ├── router/
    │   ├── index.ts              # 路由表（全部懒加载 + 兼容重定向 + catch-all）。
    │   └── guard.ts              # 守卫纯函数 resolveRouteGuard（四规则：未登录/admin/reviewer/must_change_password）。
    ├── stores/
    │   ├── auth.ts               # 当前用户、认证初始化状态、isAdmin/canReviewAgent、登录/登出/语言动作。
    │   └── project.ts            # 项目列表、当前项目和项目切换动作。
    ├── styles/
    │   ├── index.css             # 汇总入口（main.ts 唯一样式 import）。
    │   ├── tokens.css            # 全部设计令牌（色彩/字体/间距/圆角/阴影/z-index）。
    │   ├── base.css              # reset/排版/选区/滚动条/过渡/focus-visible/reduced-motion。
    │   ├── utilities.css         # .page/.toolbar/.panel/.panel-head 等全局类。
    │   ├── element-overrides.css # Element Plus 变量覆写与细节打磨。
    │   └── themes/dark.css       # 暗色令牌覆写（html.dark，切换入口见 SettingsView）。
    ├── utils/
    │   ├── datetime.ts           # formatDateTime/formatDate/formatTime/formatRelative（locale 跟随 i18n）。
    │   ├── statusMeta.ts         # 状态→UI 注册表（domain 键控 + labelKey + 三级色，枚举以后端为准）。
    │   ├── chartTheme.ts         # 图表配置收口（setupChartDefaults/refreshDefaults/chartPalette/buildChartGroups）。
    │   └── testDataPaste.ts      # 测试数据粘贴解析。
    └── views/                    # 25 个页面（详情见第 2 节路由表）。
```

应用启动代码很短，所有全局能力都在这里注册：

```ts
// src/main.ts
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'
import './styles/index.css'

setupChartDefaults() // 图表配置收口：唯一 Chart.register 点
useTheme()           // 主题单例激活（light/dark/auto）

createApp(App).use(createPinia()).use(router).use(i18n).mount('#app')
```

Element Plus 的组件和指令（含 `v-loading`）由 `unplugin-vue-components` 自动按需注册并引入样式；只有代码里显式 `import { ElMessage }` 的组件需要手动引入一次样式（如上）。

## 2. 路由系统

### 2.1 当前路由

[`src/router/index.ts`](src/router/index.ts) 中所有页面组件都是 `() => import(...)` 懒加载，首屏只下载当前路由的 chunk。路由分为三类：公开路由（`/login`）、项目无关的一级页面、以及挂在 `ProjectLayout` / `DailyReportShell` 下的子路由。

| 路径 | 目标 | 访问要求 | 导航入口 |
|---|---|---|---|
| `/` | `DashboardView` | 登录 | 首页（仪表盘） |
| `/login` | `LoginView` | 公开，`meta.public` | 无 |
| `/projects` | `ProjectsView` | 登录 | 项目 |
| `/daily-report` | `DailyReportShell` | 登录 | 日报 |
| `/daily-report`（子路由空串） | `DailyReportView` | 登录 | 今日录入 |
| `/daily-report/history` | `DailyHistoryView` | 登录 | 历史查询 |
| `/projects/:id` | `ProjectLayout` | 登录；`:id` 是项目 ID | 项目工作区 |
| `/projects/:id`（子路由空串） | `ProjectDashboard` | 登录 | 项目仪表盘 |
| `/projects/:id/issues` | `IssuesView` | 登录 | 问题 |
| `/projects/:id/experiment-runs` | `RunListView` | 登录 | 实验运行 |
| `/projects/:id/test-data` | `TestDataView` | 登录 | 测试数据 |
| `/projects/:id/rf-matching` | `RFMatchingView` | 登录 | RF 匹配 |
| `/projects/:id/assembly` | `AssemblyView` | 登录 | 装配 |
| `/experiment-runs/:id` | `RunDetailView` | 登录 | 运行详情 |
| `/step-templates` | `StepTemplatesView` | 登录 | 步骤模板 |
| `/attachments` | `AttachmentView` | 登录 | 附件 |
| `/instrument-measure` | `InstrumentMeasureView` | 登录 | 仪器 |
| `/gas-control` | `GasControlView` | 登录 | 气体控制 |
| `/sensors` | `SensorsView` | 登录 | 传感器 |
| `/todos` | `TodoView` | 登录 | 待办 |
| `/experiences` | `ExperiencesView` | 登录 | 经验 |
| `/audit` | `AuditView` | 登录 | 审计 |
| `/settings` | `SettingsView` | 登录 | 设置 |
| `/manual` | `ManualView` | 登录 | 手册 |
| `/daily-reports/:id` | `DailyReportDetailView` | 登录 | 日报详情 |
| `/admin/users` | `AdminUsersView` | 登录且角色为 `admin`（`meta.admin`） | 用户（仅管理员） |
| `/agent-candidates` | `AgentCandidatesView` | 登录且角色为 `admin`/`maintainer`（`meta.reviewer`） | AI 审核（仅审核员） |

兼容重定向（保留旧链接不 404）：

```ts
{ path: '/issues', redirect: '/projects' },
{ path: '/daily-reports', redirect: '/daily-report/history' },
{ path: '/runs/:id', redirect: '/experiment-runs/:id' },
{ path: '/projects/:id/runs', redirect: '/projects/:id/experiment-runs' },
{ path: '/instruments', redirect: '/instrument-measure' }
```

路由采用 HTML5 history 模式，部署服务器需要把未知前端路径回退到 `index.html`（见第 8 节）。每条路由都带 `meta.titleKey`，供 `MobileTopBar` 解析标题（项目工作区则优先显示项目名）。

### 2.2 根组件和布局

公开路由绕过业务布局；其余路由都由 `AppLayout` 内部的 `<RouterView />` 渲染：

```vue
<!-- src/App.vue -->
<template>
  <RouterView v-if="$route.meta.public" />
  <AppLayout v-else />
</template>
```

因此登录页没有侧栏，登录后的页面自动共享桌面侧栏、移动端顶栏/底栏。

### 2.3 导航守卫和登录保护

守卫逻辑收敛为 `src/router/guard.ts` 的纯函数 `resolveRouteGuard(to, { ready, user })`，`index.ts` 的 `beforeEach` 只做「需要时 loadMe + 调纯函数 + 应用结果」：

1. 首次进入受保护页面时调用 `auth.loadMe()` 恢复 Cookie 会话。
2. 恢复失败或没有用户时跳转登录页。
3. `meta.admin` 页面只允许管理员。
4. `meta.reviewer` 页面只允许管理员/维护者（`auth.canReviewAgent`）。
5. 首次登录尚未改密的用户只能访问设置页。

```ts
// src/router/index.ts
router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!to.meta.public && !auth.ready) {
    try {
      await auth.loadMe()
    } catch {
      return '/login'
    }
  }
  return resolveRouteGuard(to, { ready: auth.ready, user: auth.user })
})
```

meta 收敛为四键：`public?` / `admin?` / `reviewer?` / `titleKey`，**没有 `requiresAuth`**——语义统一为「非 public 即需登录」；未匹配路径由 catch-all（`/:pathMatch(.*)*` → `/`）重定向首页，不再渲染空白。

前端守卫只改善用户体验，不是安全边界；API 权限仍由 Go 后端强校验。

## 3. API 客户端

### 3.1 axios 封装和统一响应

所有业务 API 都复用 [`src/api/client.ts`](src/api/client.ts) 中的实例。`baseURL` 已固定为 `/api/v1`，开发时 Vite 再把 `/api` 代理到 Go 服务。

```ts
// src/api/client.ts
export const api = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' }
})

export async function request<T>(config: AxiosRequestConfig) {
  const response = await api.request<Envelope<T>>(config)
  return response.data.data
}

// 需要把 request_id 展示给用户（如“提交成功，request_id: xxx”）时用这个
export async function requestWithMeta<T>(config: AxiosRequestConfig) {
  const response = await api.request<Envelope<T>>(config)
  return { data: response.data.data, requestId: response.data.request_id }
}
```

### 3.2 请求拦截器：CSRF 和幂等键

每个非 `GET/HEAD/OPTIONS` 请求都会自动获得新的 `Idempotency-Key`。拦截器优先使用内存中的 CSRF token；页面刷新后内存为空时，从 `csrf_token` Cookie 恢复，再写入 `X-CSRF-Token`。`newIdempotencyKey()` 优先用 `crypto.randomUUID()`，内网 HTTP 非安全上下文不可用时回退到时间戳+随机串：

```ts
// src/api/client.ts
api.interceptors.request.use((config) => {
  config.headers = AxiosHeaders.from(config.headers)
  const method = (config.method || 'get').toUpperCase()
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    config.headers.set('Idempotency-Key', newIdempotencyKey())
    csrfToken = decodeURIComponent(csrfFromCookie() || '')
    if (csrfToken) config.headers.set('X-CSRF-Token', csrfToken)
  }
  return config
})
```

页面和业务 API 不要手动重复添加这两个请求头。

### 3.3 响应拦截器：运行时校验与兜底

响应拦截器按顺序做四件事（对应批次 4 的 C15 改动，页面层因此不再需要到处处理“空列表”和“结构异常”）：

1. **非 JSON 端点放行**：`responseType === 'blob'` 或 Content-Type 不含 `application/json` 的响应（如附件下载）原样返回。
2. **运行时结构断言**：JSON 响应必须是 `{ data, request_id }` 信封，`data` 缺失或 `request_id` 不是字符串时直接拒绝并抛出“API 响应结构异常”错误——避免把坏结构静默传给页面。
3. **空 data 兜底**：Go 的 nil slice 序列化为 `data: null`。此时按请求形态兜底：GET 集合端点（末段是纯小写字母，如 `projects`、`todos`）给 `[]`，详情类请求给 `{}`，页面无需再判断。
4. **错误统一化**：把后端 `error.message` 转成 `Error`，并附带 `requestId` 和 `status` 字段（页面可用 `useNotify.ts` 的 `showApiError` 展示 `request_id`）。

```ts
// src/api/client.ts
api.interceptors.response.use(
  (response) => {
    // ... blob / 非 JSON 放行 ...
    const body = response.data
    if (!body || typeof body !== 'object' || !('data' in body) || typeof body.request_id !== 'string') {
      return Promise.reject(new Error(`API 响应结构异常（缺 data/request_id）：${response.config.url}`))
    }
    if (body.data === null || body.data === undefined) {
      body.data = isCollectionRequest(response.config) ? [] : {}
    }
    return response
  },
  async (error) => {
    // 401 单飞刷新 + 重试一次（见下）；失败后错误统一转 Error（带 requestId/status）
  }
)
```

### 3.4 401 单飞刷新

access token 只有 15 分钟。除 `/auth/login`、`/auth/refresh` 之外的接口返回 401 时，axios 拦截器会调用 `refreshSession()` 刷新并原样重试一次；刷新也失败说明会话已失效，整页跳回登录。

单飞（single-flight）通过模块级 `refreshPromise` 实现：并发多个 401 只发起一次 refresh，成功后同步更新内存 CSRF token。该函数也以 `refreshAuthSession()` 导出，供不经过 axios 的 SSE 流（系统更新日志）在 401 后复用同一个 refresh，避免双刷新竞态。

```ts
// src/api/client.ts
function refreshSession(): Promise<boolean> {
  refreshPromise ??= api
    .post('/auth/refresh', {})
    .then((res) => { /* 更新 csrf */ return true })
    .catch(() => false)
    .finally(() => { refreshPromise = null })
  return refreshPromise
}
```

新增接口时，应在 `src/api/` 的对应业务文件中声明类型并调用 `request<T>()`，页面不要直接创建另一个 axios 实例。例如：

```ts
// src/api/issues.ts
export function listIssues(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: Issue[]; total: number; page: number }>({
    url: `/projects/${projectId}/issues`,
    params
  })
}
```

### 3.5 Cookie 认证

浏览器不把 token 放入 `localStorage`。登录和刷新成功后，Go 后端设置同源 Cookie：

- `access_token`：HttpOnly，API 中间件用它识别当前用户。
- `refresh_token`：HttpOnly，access token 失效后用于刷新。
- `csrf_token`：非 HttpOnly，供前端读取并回传到请求头。

`withCredentials: true` 让 axios 在请求中携带 Cookie。由于 access/refresh Cookie 是 HttpOnly，前端代码不能也不需要读取它们。`auth.loadMe()` 先请求 `/auth/me`；失败后刷新，再重试 `/auth/me`：

```ts
// src/stores/auth.ts
async loadMe() {
  try {
    try {
      this.user = await authApi.me()
    } catch {
      await authApi.refresh()
      this.user = await authApi.me()
    }
    applyUserLanguage(this.user)
  } finally {
    this.ready = true
  }
}
```

## 4. 状态管理

项目只有两个 Pinia store，均使用 Options Store 写法。

### 4.1 auth store

[`src/stores/auth.ts`](src/stores/auth.ts) 管理：

- `user`：当前登录用户，未登录时为 `null`。
- `ready`：是否已经尝试恢复会话，避免每次路由跳转都请求 `/auth/me`。
- `isAdmin`：由 `user.role === 'admin'` 派生的管理员判断。
- `canReviewAgent`：`admin`/`maintainer` 可审核 AI 候选（与后端 ListCandidates 权限一致）。
- `login/loadMe/logout/setLanguage`：认证与语言动作。

登录或加载用户资料后，会以后端 `user.language` 覆盖显示语言（`applyUserLanguage` 调 `setLocale`），`localStorage` 只是兜底。

```ts
// src/stores/auth.ts
getters: {
  isAdmin: (state) => state.user?.role === 'admin',
  canReviewAgent: (state) => ['admin', 'maintainer'].includes(state.user?.role || '')
},
actions: {
  async login(username: string, password: string) {
    const data = await authApi.login(username, password)
    this.user = data.user
    this.ready = true
    if (data.csrf_token) setCSRFToken(data.csrf_token)
    applyUserLanguage(this.user)
    return data
  }
}
```

### 4.2 project store

[`src/stores/project.ts`](src/stores/project.ts) 管理跨页面共享的项目上下文：

- `projects`：当前用户可见的项目列表。
- `currentId`：用户当前选择的项目 ID。
- `current`：按 `currentId` 查找项目，未选择时回退到第一项。
- `load/select`：加载项目和切换当前项目。

```ts
// src/stores/project.ts
getters: {
  current: (state) =>
    state.projects.find((item) => item.id === state.currentId) || state.projects[0]
},
actions: {
  async load() {
    // 后端空列表的 data: null（Go nil slice）由 client.ts 响应拦截器统一兜底为 []
    this.projects = await listProjects()
    if (!this.currentId && this.projects[0]) this.currentId = this.projects[0].id
  },
  select(id: string) {
    this.currentId = id
  }
}
```

`ProjectSidebar` 点击项目时调用 `store.select()` 并跳转 `/projects/:id`；`ProjectLayout` 里可以通过切换器随时切换项目工作区。

## 5. 核心组件

### AppLayout

[`src/layouts/AppLayout.vue`](src/layouts/AppLayout.vue) 是所有受保护页面的外壳。它加载项目列表，在桌面显示固定侧栏（分组为“主导航 + 系统”），在移动端显示 `MobileTopBar` 和五项底部导航，并加载 Agent 待审候选数徽章。

导航数据来自 `src/config/navigation.ts` 单一数据源（`NAV_ITEMS`，每项含 `path/icon/titleKey/minRole/mobile`），角色过滤由纯函数 `filterNavByRole(items, role)` 完成——新增页面只改该文件与路由表，不再手改 AppLayout：

```ts
// src/config/navigation.ts
export const NAV_ITEMS: NavEntry[] = [
  { path: '/', icon: HomeFilled, titleKey: 'nav.home' },
  { path: '/projects', icon: FolderOpened, titleKey: 'nav.projects' },
  // ...
  { path: '/agent-candidates', icon: MagicStick, titleKey: 'nav.aiReview', minRole: 'maintainer' },
  // systemItems 用 group: 'system' 区分
]
```

桌面侧栏三个 computed 都由「按 group 过滤 + `filterNavByRole`」派生：`navItems`（首页、项目、待办、日报、经验、附件 + AI 审核）、`systemItems`（气体控制、仪器、传感器、用户、审计、手册）、`mobileItems`（`mobile: true` 的 5 项：首页、项目、待办、日报、我的）。

Agent 徽章（C11）：仅 `canReviewAgent` 时每 30s 轮询 `listAgentCandidates({ status: 'pending_review', page: 1, per_page: 1 })` 的 `total` 显示未读数；页面隐藏时暂停，进入候选页或角色到位时立即刷新（轮询由 `usePolling` 承载）。

侧栏高亮使用前缀匹配（`/projects/:id/*` 与 `/projects` 是兄弟路由记录，`RouterLink` 的自动高亮只匹配同一条记录）：

```ts
function navActive(path: string) {
  if (path === '/') return route.path === '/'
  return route.path === path || route.path.startsWith(path + '/')
}
```

### MobileTopBar

[`src/components/MobileTopBar.vue`](src/components/MobileTopBar.vue) 是移动端固定顶栏：非一级路径（底栏 5 项和 `/login`）显示返回键（深链打开时回退到首页），标题优先取项目工作区的当前项目名，否则用 `route.meta.titleKey` 查 i18n。

### ProjectLayout / ProjectDashboard / DailyReportShell

- [`src/layouts/ProjectLayout.vue`](src/layouts/ProjectLayout.vue)：项目工作区壳。加载项目详情（含失败回退页），显示项目头（返回、名称/阶段标签、切换项目）和 6 个 Tab（仪表盘/问题/实验运行/测试数据/RF 匹配/装配），Tab 内容由子路由渲染；项目上下文的唯一事实来源是路由参数。
- [`src/components/business/ProjectDashboard.vue`](src/components/business/ProjectDashboard.vue)：项目阶段流程图，展示阶段节点状态并触发推进/回退确认（阶段机由后端决定，前端只发 `transitionProject`）。
- [`src/layouts/DailyReportShell.vue`](src/layouts/DailyReportShell.vue)：日报“今日录入/历史查询”两个子路由的 Tab 容器。

### ProjectSidebar

[`src/components/business/ProjectSidebar.vue`](src/components/business/ProjectSidebar.vue) 展示可搜索的项目列表（按名称/编码过滤），点击后先 `store.select()` 再跳转项目工作区：

```ts
function open(id: string) {
  store.select(id)
  router.push(`/projects/${id}`)
}
```

### CommentSection

[`src/components/business/CommentSection.vue`](src/components/business/CommentSection.vue) 负责评论列表、输入框和空状态。它不直接调用 API，而是通过 `submit` 事件把内容交给父页面，保持组件可复用：

```ts
defineProps<{ comments: Comment[] }>()
const emit = defineEmits<{ submit: [content: string] }>()

function submit() {
  emit('submit', content.value)
  content.value = ''
}
```

### StatusBadge

[`src/components/base/StatusBadge.vue`](src/components/base/StatusBadge.vue) 通过 `props { domain, value }` 查 `src/utils/statusMeta.ts` 注册表渲染统一颜色与 i18n label；枚举值集以后端（`docs/api-contract.md`）为唯一事实源，未注册的 value 降级显示原文并 `console.warn`、tone 落 `info`：

```ts
// src/utils/statusMeta.ts（节选）
export const STATUS_META: Record<StatusDomain, Record<string, StatusMeta>> = {
  runStatus: {
    running: { tone: 'success', labelKey: 'runList.status.running', graphic: '--ok', text: '--ok-text', soft: '--ok-soft' },
    // ...
  },
  // 域：runStatus / stepStatus / issueStatus / issueSeverity / alertLevel / instrumentState / testQuality / todoPriority / userRole / projectStage
}
```

### MarkdownView

[`src/components/business/MarkdownView.vue`](src/components/business/MarkdownView.vue) 用 markdown-it 做只读渲染：`html: false` 转义原始 HTML 防注入、`linkify` 自动识别 URL、`breaks` 保留换行；链接统一新窗口打开并带 `noopener`。日报原文、经验、手册等展示侧文本都用它。

### ResponsiveTable

[`src/components/base/ResponsiveTable.vue`](src/components/base/ResponsiveTable.vue) 是列表页的响应式底座：桌面端渲染 `el-table`；移动端且有 `card` 插槽时渲染卡片列表（无 card 插槽仍回退表格）。`v-bind="$attrs"` 透传 loading 等属性：

```vue
<ResponsiveTable :rows="events" :loading="eventsLoading">
  <el-table-column prop="created_at" :label="t('audit.time')" width="190" />
  <template #card="{ row }">
    <div class="card-title">{{ row.action }}</div>
    <div class="card-fields"><span>{{ row.method }} {{ row.path }}</span></div>
  </template>
</ResponsiveTable>
```

### StepItemsEditor

[`src/components/business/StepItemsEditor.vue`](src/components/business/StepItemsEditor.vue) 步骤列表编辑器：名称/描述/依赖顺序三列，供步骤模板和实验运行步骤的编辑弹窗复用，通过 `emitChange` 通知父组件收集变更。

## 6. 页面开发模式

本项目的页面使用标准 Vue 单文件组件，顺序统一为 `<template>`、`<script setup lang="ts">`、`<style scoped>`，UI 文案一律走 `useI18n()` 的 `t()` 查 key，不写死中文。

### 6.1 表单页：以 LoginView 为例

模板使用 Element Plus 表单；`@submit.prevent` 阻止浏览器刷新，输入值通过 `v-model` 绑定到响应式表单，加载和错误状态直接驱动 UI：

```vue
<!-- src/views/LoginView.vue -->
<el-form label-position="top" @submit.prevent="submit">
  <el-form-item :label="t('login.username')">
    <el-input v-model="form.username" autocomplete="username" />
  </el-form-item>
  <el-form-item :label="t('login.password')">
    <el-input v-model="form.password" type="password" autocomplete="current-password" show-password />
  </el-form-item>
  <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
  <el-button type="primary" native-type="submit" :loading="loading">{{ t('login.submit') }}</el-button>
</el-form>
```

script 只组织交互：调用 auth store 登录，根据后端返回的首次改密标志决定跳转，并在 `finally` 中可靠关闭 loading。具体 HTTP 细节留在 API 层：

```ts
// src/views/LoginView.vue
const loading = ref(false)
const error = ref('')
const form = reactive({ username: '', password: '' })

async function submit() {
  loading.value = true
  error.value = ''
  try {
    const data = await auth.login(form.username, form.password)
    await router.push(data.must_change_password ? '/settings' : '/projects')
  } catch (err) {
    error.value = err instanceof Error ? err.message : '登录失败'
  } finally {
    loading.value = false
  }
}
```

### 6.2 列表页模式

列表页必须处理加载中、空、错误三种状态，移动端用 `ResponsiveTable` 的 card 插槽提供卡片视图。错误提示统一用 `useNotify.ts` 的 `showApiError`，它会把 `request_id` 一并展示，便于追审计日志：

```ts
// src/composables/useNotify.ts
export function showApiError(err: unknown, fallback: string) {
  const e = err as (Error & { requestId?: string }) | undefined
  const message = e?.message || fallback
  ElMessage.error(e?.requestId ? `${message}（request_id: ${e.requestId}）` : message)
}
```

典型骨架：

```vue
<template>
  <div class="page">
    <div class="toolbar"><h2>{{ t('xxx.title') }}</h2></div>
    <section class="panel">
      <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" />
      <ResponsiveTable v-else :rows="items" :loading="loading">
        <el-table-column prop="name" :label="t('xxx.name')" />
        <template #empty><el-empty :description="t('xxx.empty')" /></template>
        <template #card="{ row }">
          <div class="card-title">{{ row.name }}</div>
        </template>
      </ResponsiveTable>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listXxx, type Xxx } from '../api/xxx'
import { showApiError } from '../composables/useNotify'

const { t } = useI18n()
const items = ref<Xxx[]>([])
const loading = ref(false)
const loadError = ref('')

onMounted(async () => {
  loading.value = true
  try {
    items.value = await listXxx()
  } catch (err) {
    loadError.value = (err as Error).message
  } finally {
    loading.value = false
  }
})
</script>
```

分页、状态筛选、diff 对照、时间线等更复杂的交互参考这些页面：

- `AgentCandidatesView`：真实分页（`el-pagination` + `total`）+ 审核操作 + 五阶段时间线（日报提交 → AI 解析 → 候选生成 → 人工审核 → 执行产物，数据来自 `GET /agent/candidates/{id}/trace`）+ 候选原文与执行后内容的 line/word 双模式 diff（`diff` 包）。
- `InstrumentMeasureView`：仪器状态卡片 + 白名单命令执行 + 常驻 AI 对话面板（`interpretCommand`，候选命令带校验提示，可直接保存为测试数据）。
- `GasControlView`：`EventSource('/api/v1/ws/gascell')` 实时曲线（chart.js），客户端断开自动重连。
- `TodoView`/`DashboardView`：LLM 快捷添加（`llmParse` 后确认 `llmAdd`）。
- `AuditView`：两个 Tab——审计事件列表（`listAuditEvents`，带筛选和分页）和按 `request_id` 查单条链路（`getAudit`）。
- `SensorsView`：SVG 自绘趋势图，支持横向滚动。

### 6.3 style：局部样式

`scoped` 限制样式只作用于当前组件；全局样式拆在 `src/styles/`：`tokens.css`（设计令牌：色彩/字体/间距/圆角/阴影/z-index）、`base.css`（reset/排版/focus-visible/动效）、`utilities.css`（跨页面复用的 `.page`、`.panel`、`.toolbar` 等全局类）、`element-overrides.css`（Element Plus 变量覆写）、`themes/dark.css`（暗色令牌覆写）。新样式优先复用现有令牌与全局类，不重复定义色值。

### 对应后端模块

| 页面 | 后端模块 | 说明 |
|------|----------|------|
| DashboardView | 多个模块聚合 | 首页：仪器/气体/日报/待办速览 |
| LoginView / SettingsView / AdminUsersView | `go-server/auth/` | 登录注册、改密/语言、用户管理 |
| ProjectsView / ProjectLayout / ProjectDashboard | `go-server/projects/` | 项目 CRUD、成员、阶段流转 |
| DailyReportView / DailyHistoryView / DailyReportDetailView | `go-server/logs/` | 日报录入、历史、详情（AI 解析在 `agent/`） |
| IssuesView | `go-server/issues/` | Issue 看板、评论、状态流转 |
| ExperiencesView | `go-server/experiences/` | 经验检索、发布、归档 |
| AuditView | `go-server/audit/` | 审计事件列表 + request_id 链路（hash 链校验在后端） |
| AgentCandidatesView | `go-server/agent/` | AI 候选审核 + trace 时间线 |
| RunListView / RunDetailView | `go-server/runs/` | 实验运行、步骤、模板应用 |
| TestDataView | `go-server/testdata/` | 测试数据 |
| RFMatchingView | `go-server/rfmatch/` | RF 匹配 |
| AssemblyView | `go-server/assembly/` | 装配步骤与流转 |
| StepTemplatesView | `go-server/steptemplates/` | 步骤模板 + AI 生成 |
| AttachmentView | `go-server/attachments/` | 附件上传/下载/关联 |
| InstrumentMeasureView / GasControlView | `go-server/instruments/` | 仪器命令、AI 对话、气体控制 |
| SensorsView | `go-server/sensors/` | 传感器时序 |
| TodoView | `go-server/todos/` | 待办 + LLM + ntfy 订阅 |
| ManualView | `go-server/system/` 等 | 使用手册（MarkdownView 渲染） |

## 7. 如何新增一个页面

以下以新增“设备”页 `/instruments`（实际路径已存在，仅作示例）为例。只创建实际需要的文件，不新增一层 service、hook 或包装器。

### 第 1 步：创建 View 和 i18n 文案

新建 `src/views/InstrumentsView.vue`，保持现有 SFC 结构，文案全部走 `t()`：

```vue
<template>
  <div class="page">
    <div class="toolbar"><h2>{{ t('instruments.title') }}</h2></div>
    <section class="panel">
      <el-empty v-if="items.length === 0" description="暂无设备" />
      <el-table v-else :data="items" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listInstruments, type Instrument } from '../api/instruments'

const { t } = useI18n()
const items = ref<Instrument[]>([])
onMounted(async () => { items.value = await listInstruments() })
</script>
```

在 `src/i18n/zh.ts` 添加 `instruments.*` key，并在 `en.ts` 对齐同一结构（新页面不要写死中文）。

### 第 2 步：注册路由

在 `src/router/index.ts` 用懒加载导入并添加路由。普通登录页不需要额外 meta；管理员页用 `meta.admin`，Agent 审核页用 `meta.reviewer`；所有路由必须带 `meta.titleKey`（MobileTopBar 标题）：

```ts
const InstrumentsView = () => import('../views/InstrumentsView.vue')

// routes 中
{ path: '/instruments', component: InstrumentsView, meta: { titleKey: 'nav.instruments' } }
```

项目对象权限不要在前端发明 `RequireProjectAccess` 一类组件；按现有模式从路由参数取得项目 ID，调用 API，让后端执行对象级授权。

### 第 3 步：添加 API 调用

新建 `src/api/instruments.ts`，复用 `request<T>()`。写请求的 Cookie、CSRF 和 `Idempotency-Key` 会由拦截器自动处理：

```ts
import { request } from './client'

export type Instrument = { id: string; name: string; status: string }

export function listInstruments() {
  return request<Instrument[]>({ url: '/instruments' })
}
```

页面只调用 `listInstruments()`，不要直接依赖 axios，也不要在组件里拼 `/api/v1`。

### 第 4 步：添加导航入口

在 `src/config/navigation.ts` 的 `NAV_ITEMS` 添加一项（系统类入口给 `group: 'system'`，需要角色门槛给 `minRole`，需要出现在移动端底栏给 `mobile: true`），角色过滤由 `filterNavByRole` 统一处理——桌面侧栏与移动端底栏自动跟随，**不需要**再改 `AppLayout.vue`（底栏当前固定 5 项且 CSS 是 `grid-template-columns: repeat(5, 1fr)`，增删底栏项需同步调整样式）。

### 第 5 步：验证

最后运行 `npm run build`（先过 `vue-tsc --noEmit` 类型检查）、`npm test`（vitest 全绿）与 `npm run test:coverage`（窄口径阈值门禁），并手工检查登录保护、直接输入 URL、桌面侧栏、移动端底栏和 768px 以下布局。

## 8. 构建与部署

生产环境由 Go 服务器 `//go:embed static` 嵌入前端并通过 SPA fallback 提供。因此本项目的构建产物是标准多文件：**路由级代码分割（所有页面懒加载）+ vendor 分包**，而不是单文件内联。

[`vite.config.ts`](vite.config.ts) 做了三件事：

1. **路由懒加载**：`router/index.ts` 里所有页面都用 `() => import()`，Vite 自动为每个路由生成独立 chunk，首屏只加载当前路由。
2. **vendor 分包**：`manualChunks` 把 `@element-plus/icons-vue`、`element-plus`、其余依赖（vue/router/pinia/axios/@vueuse）分别拆成 `vendor-icons`、`vendor-element`、`vendor`，避免业务 chunk 和库代码混在一起。
3. **Element Plus 按需引入**：`unplugin-vue-components` + `ElementPlusResolver`（`importStyle: 'css'`、`directives: true`），组件和 `v-loading` 指令自动注册并只带对应 CSS；`ElMessage`/`ElMessageBox` 在 `main.ts` 手动引入一次样式。

```ts
// vite.config.ts
build: {
  rollupOptions: {
    output: {
      manualChunks(id) {
        if (!id.includes('node_modules')) return
        if (id.includes('@element-plus/icons-vue')) return 'vendor-icons'
        if (id.includes('element-plus')) return 'vendor-element'
        return 'vendor'
      }
    }
  }
}
```

Go 侧的 `spaHandler`（`go-server/main.go`）按以下规则分发（和旧版“全部返回 index.html”不同）：

1. 存在的文件（如 `/assets/index-xxx.js`、`/assets/*.css`）直接返回。
2. 带扩展名但不存在的路径返回真实 404——避免浏览器把 `text/html` 当 JS/CSS 执行导致白屏。
3. 其余非 `/api` 路径回退到 `index.html`，由前端路由接管（HTML5 history 模式）。

`public/sw.js` 是历史 `vite-plugin-pwa` 遗留 Service Worker 的“自杀式”清理脚本（安装后立即自我注销并刷新页面），不是 PWA 功能；项目已不再使用 PWA。

部署流程：`npm run build` 后把 `web-ui/dist/` 同步到 `go-server/static/`（`rm -rf go-server/static && cp -r web-ui/dist go-server/static`），再构建 Go 镜像嵌入。

## 9. 移动端适配

### 9.1 统一断点

[`src/composables/useMobile.ts`](src/composables/useMobile.ts) 使用 VueUse 的响应式媒体查询，断点为 `768px`：

```ts
// src/composables/useMobile.ts
export function useMobile() {
  return useMediaQuery('(max-width: 768px)')
}
```

当模板结构需要变化时使用 `useMobile()`；纯样式变化直接使用相同的 CSS media query。

### 9.2 桌面与移动端差异

| 区域 | 桌面端 | 移动端 |
|---|---|---|
| 顶部 | 无（侧栏含品牌区和用户卡片） | `MobileTopBar`：返回键 + 标题（titleKey/项目名），适配刘海 safe-area |
| 主导航 | 左侧 216px 固定侧栏，按“主导航 + 系统”分组 | 底部固定 5 项：首页、项目、待办、日报、我的 |
| 内容区 | 左侧留出 216px，内边距 24px | 无左边距，上下避开顶栏和底栏（`--safe-area-top/bottom`） |
| 列表页 | `el-table` 表格 | `ResponsiveTable` 的 card 插槽渲染卡片列表 |
| 项目工作区 | 项目头和 Tab 平铺 | 项目头 + Tab 收窄，图表横向滚动 |

`AppLayout` 的结构切换：

```vue
<!-- src/layouts/AppLayout.vue -->
<MobileTopBar v-if="isMobile" />
<aside v-if="!isMobile" class="nav">...</aside>
<main class="content"><RouterView /></main>
<nav v-if="isMobile" class="bottom-nav">...</nav>
```

纯布局变化则留在 CSS，例如移动端底栏：

```css
/* src/layouts/AppLayout.vue */
@media (max-width: 768px) {
  .content {
    margin-left: 0;
    padding: calc(var(--mobile-topbar-height) + var(--safe-area-top) + 8px) 12px
      calc(var(--bottom-nav-height) + var(--safe-area-bottom) + 12px);
  }
}
```

新增页面至少检查 320px 宽度、768px 断点两侧、触控按钮大小、横向溢出以及底部导航遮挡。

## 10. 开发命令

在 `web-ui/` 目录执行：

```bash
cd web-ui

# 按 package-lock.json 全新安装依赖，CI 和首次拉取代码时使用
npm ci

# 启动开发服务器：http://localhost:5173
# /api 请求会代理到 http://localhost:8000
npm run dev

# 先执行 vue-tsc --noEmit，再生成生产构建到 dist/
npm run build

# 单元/组件测试（vitest，测试与被测源码同目录 __tests__/）
npm test

# 覆盖率（窄口径门禁：lines/statements ≥70%、branches ≥65%、functions ≥60%）
npm run test:coverage
```

对应的真实脚本定义：

```json
// package.json
{
  "scripts": {
    "dev": "vite --host 0.0.0.0",
    "build": "vue-tsc --noEmit && vite build",
    "preview": "vite preview --host 0.0.0.0",
    "test": "vitest run",
    "test:coverage": "vitest run --coverage"
  }
}
```

开发服务器代理配置：

```ts
// vite.config.ts
server: {
  port: 5173,
  proxy: {
    '/api': 'http://localhost:8000'
  }
}
```

推荐的新功能阅读路径是：先找 `views/` 中的交互入口，再跟到 `stores/` 或 `api/`，最后查看 `client.ts` 的通用请求行为。这样能最快区分“页面状态”“跨页面状态”和“后端数据”。
