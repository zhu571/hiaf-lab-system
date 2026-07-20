# HIAF Lab System 前端代码阅读指南

本文面向有 Vue 3 基础、但第一次接触本项目的开发者。当前前端使用 Vue 3（Composition API + `<script setup>`）、TypeScript、Element Plus、Pinia、Vue Router 和 Vite（单文件构建，JS/CSS 全部内联进 index.html）。建议按“入口 → 路由 → 布局 → store → API → 页面”的顺序阅读。

> 本文以当前 Phase 2.6 代码为准。项目权限由路由守卫和后端共同校验；不要使用已经废弃的 `RequireProjectAccess` 写法。

## 1. 项目结构概览

```text
web-ui/
├── index.html                    # Vite HTML 入口，提供 #app 挂载点和移动端 viewport。
├── package.json                  # 依赖和 dev/build/preview 脚本。
├── package-lock.json             # npm 锁文件，保证依赖安装可复现。
├── tsconfig.json                 # TypeScript 严格模式和 Vue 文件检查范围。
├── vite.config.ts                # Vue 插件、单文件构建（内联 JS/CSS）、开发服务器及 API 代理。
└── src/
    ├── main.ts                   # 创建 Vue 应用并注册 Pinia、Router、Element Plus。
    ├── App.vue                   # 根组件；公开页直接渲染，业务页进入 AppLayout。
    ├── style.css                 # 页面、工具栏、面板等全局基础样式。
    ├── env.d.ts                  # 声明 *.vue 模块，使 TypeScript 能识别 SFC。
    ├── api/
    │   ├── client.ts             # axios 实例、响应包解包、CSRF 和幂等键拦截器。
    │   ├── auth.ts               # 登录、刷新、用户管理和改密 API。
    │   ├── projects.ts           # 项目及项目成员 API 和类型。
    │   ├── logs.ts               # 日报和项目日志 API 和类型。
    │   ├── issues.ts             # Issue、状态流转和评论 API 和类型。
    │   ├── experiences.ts        # 经验的查询、创建、发布和归档 API。
    │   └── audit.ts              # 按 request_id 查询审计记录。
    ├── components/
    │   ├── AppLayout.vue         # 登录后桌面侧栏、移动端底栏和 RouterView 容器。
    │   ├── ProjectSidebar.vue    # 可搜索、可切换当前项目的项目列表。
    │   ├── CommentSection.vue    # Issue 评论展示和提交组件。
    │   └── StatusBadge.vue       # 将业务状态映射为 Element Plus 标签颜色。
    ├── composables/
    │   └── useMobile.ts          # 统一提供 768px 移动端媒体查询。
    ├── router/
    │   └── index.ts              # 路由表、登录恢复、管理员和首次改密守卫。
    ├── stores/
    │   ├── auth.ts               # 当前用户、认证初始化状态和登录/登出动作。
    │   └── project.ts            # 项目列表、当前项目和项目切换动作。
    └── views/
        ├── LoginView.vue         # 登录页。
        ├── ProjectsView.vue      # 项目列表、项目仪表盘和新建项目。
        ├── DailyReportView.vue   # 今日日报、原文保存、项目日志和提交。
        ├── DailyHistoryView.vue  # 日报历史及按日期查询。
        ├── IssuesView.vue        # 项目 Issue 看板、详情、评论和状态流转。
        ├── ExperiencesView.vue   # 经验检索、创建、发布和归档。
        ├── AuditView.vue         # 按 request_id 查询审计链路。
        ├── SettingsView.vue      # 修改密码和退出登录。
        └── AdminUsersView.vue    # 管理员创建用户、改角色和重置密码。
```

应用启动代码很短，所有全局能力都在这里注册：

```ts
// src/main.ts
createApp(App).use(createPinia()).use(router).use(ElementPlus).mount('#app')
```

## 2. 路由系统

### 2.1 当前路由

[`src/router/index.ts`](src/router/index.ts) 当前有 10 条路由记录：1 条根路径重定向和 9 个页面组件。用户口中的“10 个页面”在当前代码中的准确口径如下。

| 路径 | 目标 | 访问要求 | 导航入口 |
|---|---|---|---|
| `/` | 重定向到 `/projects` | 随目标路由 | 无 |
| `/login` | `LoginView` | 公开，`meta.public` | 无 |
| `/projects` | `ProjectsView` | 登录 | 项目 |
| `/daily-report` | `DailyReportView` | 登录 | 日报 |
| `/projects/:id/issues` | `IssuesView` | 登录；`:id` 是项目 ID | 问题 |
| `/experiences` | `ExperiencesView` | 登录 | 经验 |
| `/audit` | `AuditView` | 登录 | 审计（仅桌面） |
| `/settings` | `SettingsView` | 登录 | 设置 |
| `/daily-reports` | `DailyHistoryView` | 登录 | 历史（仅桌面） |
| `/admin/users` | `AdminUsersView` | 登录且角色为 `admin` | 用户（仅管理员、仅桌面） |

路由采用 HTML5 history 模式，部署服务器需要把未知前端路径回退到 `index.html`：

```ts
// src/router/index.ts
const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/projects' },
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/projects/:id/issues', component: IssuesView },
    { path: '/admin/users', component: AdminUsersView, meta: { admin: true } }
  ]
})
```

### 2.2 根组件和布局

公开路由绕过业务布局；其余路由都由 `AppLayout` 内部的 `<RouterView />` 渲染：

```vue
<!-- src/App.vue -->
<template>
  <RouterView v-if="$route.meta.public" />
  <AppLayout v-else />
</template>
```

因此登录页没有侧栏，登录后的页面自动共享桌面侧栏或移动端底栏。

### 2.3 导航守卫和登录保护

全局 `beforeEach` 依次完成四件事：

1. 首次进入受保护页面时调用 `auth.loadMe()` 恢复 Cookie 会话。
2. 恢复失败或没有用户时跳转登录页。
3. `meta.admin` 页面只允许管理员，其他用户回到项目页。
4. 首次登录尚未改密的用户只能访问设置页。

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
  if (!to.meta.public && !auth.user) return '/login'
  if (to.meta.admin && !auth.isAdmin) return '/projects'
  if (to.path !== '/settings' && auth.user?.must_change_password) return '/settings'
})
```

前端守卫只改善用户体验，不是安全边界；API 权限仍由 Go 后端强校验。

## 3. API 客户端

### 3.1 axios 封装和统一响应

所有业务 API 都复用 [`src/api/client.ts`](src/api/client.ts) 中的实例。`baseURL` 已固定为 `/api/v1`，开发时 Vite 再把 `/api` 代理到 Go 服务。

```ts
// src/api/client.ts
export const api = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
  headers: {
    'Content-Type': 'application/json'
  }
})

export async function request<T>(config: AxiosRequestConfig) {
  const response = await api.request<Envelope<T>>(config)
  return response.data.data
}
```

后端成功响应形如 `{ data, request_id }`，`request<T>()` 当前只把 `data` 返回给页面。错误拦截器则把后端 `error.message` 统一转换成普通 `Error`：

```ts
// src/api/client.ts
api.interceptors.response.use(
  (response) => response,
  (error) => {
    const message = error.response?.data?.error?.message || error.message || '请求失败'
    return Promise.reject(new Error(message))
  }
)
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

### 3.2 Cookie 认证

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
  } finally {
    this.ready = true
  }
}
```

### 3.3 CSRF token 和幂等键

登录或刷新响应里的 CSRF token 先保存在模块内存中：

```ts
// src/api/auth.ts
export async function login(username: string, password: string) {
  const data = await request<LoginResponse>({
    url: '/auth/login', method: 'POST', data: { username, password }
  })
  setCSRFToken(data.csrf_token)
  return data
}
```

每个非 `GET/HEAD/OPTIONS` 请求都会自动获得新的 `Idempotency-Key`。拦截器优先使用内存中的 CSRF token；页面刷新后内存为空时，从 `csrf_token` Cookie 恢复，再写入 `X-CSRF-Token`：

```ts
// src/api/client.ts
api.interceptors.request.use((config) => {
  config.headers = AxiosHeaders.from(config.headers)
  const method = (config.method || 'get').toUpperCase()
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    config.headers.set('Idempotency-Key', crypto.randomUUID())
    csrfToken ||= decodeURIComponent(csrfFromCookie() || '')
    if (csrfToken) config.headers.set('X-CSRF-Token', csrfToken)
  }
  return config
})
```

页面和业务 API 不要手动重复添加这两个请求头。

## 4. 状态管理

项目只有两个 Pinia store，均使用 Options Store 写法。

### 4.1 auth store

[`src/stores/auth.ts`](src/stores/auth.ts) 管理：

- `user`：当前登录用户，未登录时为 `null`。
- `ready`：是否已经尝试恢复会话，避免每次路由跳转都请求 `/auth/me`。
- `isAdmin`：由 `user.role` 派生的管理员判断。
- `login/loadMe/logout`：认证动作。

```ts
// src/stores/auth.ts
export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null as UserInfo | null,
    ready: false
  }),
  getters: {
    isAdmin: (state) => state.user?.role === 'admin'
  },
  actions: {
    async logout() {
      await authApi.logout()
      this.user = null
    }
  }
})
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
    this.projects = await listProjects()
    if (!this.currentId && this.projects[0]) this.currentId = this.projects[0].id
  },
  select(id: string) {
    this.currentId = id
  }
}
```

`ProjectSidebar` 修改 `currentId`，`AppLayout` 据此生成当前项目的 Issue 链接；具体项目页仍以路由参数 `:id` 为优先来源。

## 5. 核心组件

### AppLayout

[`src/components/AppLayout.vue`](src/components/AppLayout.vue) 是所有受保护页面的外壳。它加载项目列表，在桌面显示固定侧栏，在移动端显示五项底部导航，并仅向管理员加入用户管理入口。

```ts
// src/components/AppLayout.vue
const navItems = computed(() => {
  const projectId = projects.current?.id || projects.currentId
  const items = [
    { label: '项目', path: '/projects', icon: FolderOpened },
    { label: '日报', path: '/daily-report', icon: Document },
    { label: '问题', path: projectId ? `/projects/${projectId}/issues` : '/projects', icon: Tickets },
    // ...
  ]
  if (auth.isAdmin) items.push({ label: '用户', path: '/admin/users', icon: User })
  return items
})
```

### ProjectSidebar

[`src/components/ProjectSidebar.vue`](src/components/ProjectSidebar.vue) 展示可搜索的项目列表，点击时只更新 project store，不自行跳转路由。

```ts
// src/components/ProjectSidebar.vue
const filtered = computed(() => store.projects.filter((item) =>
  `${item.name} ${item.code}`.toLowerCase().includes(keyword.value.toLowerCase())
))
```

```vue
<button
  v-for="project in filtered"
  :class="['project-item', { active: project.id === store.currentId }]"
  @click="store.select(project.id)"
>
```

### CommentSection

[`src/components/CommentSection.vue`](src/components/CommentSection.vue) 负责 Issue 评论列表、输入框和空状态。它不直接调用 API，而是通过 `submit` 事件把内容交给父页面，保持组件可复用。

```ts
// src/components/CommentSection.vue
defineProps<{ comments: Comment[] }>()
const emit = defineEmits<{ submit: [content: string] }>()

function submit() {
  emit('submit', content.value)
  content.value = ''
}
```

父组件的使用方式：

```vue
<!-- src/views/IssuesView.vue -->
<CommentSection :comments="selected.comments || []" @submit="comment" />
```

### StatusBadge

[`src/components/StatusBadge.vue`](src/components/StatusBadge.vue) 把多个模块共享的状态映射为统一颜色；未知状态使用 `primary`。

```ts
// src/components/StatusBadge.vue
const type = computed(() => {
  if (['active', 'published', 'confirmed', 'resolved'].includes(props.value)) return 'success'
  if (['draft', 'candidate', 'open'].includes(props.value)) return 'warning'
  if (['archived', 'closed', 'locked'].includes(props.value)) return 'info'
  return 'primary'
})
```

## 6. 页面开发模式：以 LoginView 为例

本项目的页面使用标准 Vue 单文件组件，顺序统一为 `<template>`、`<script setup lang="ts">`、`<style scoped>`。

### 6.1 template：声明界面和事件

模板使用 Element Plus 表单；`@submit.prevent` 阻止浏览器刷新，输入值通过 `v-model` 绑定到响应式表单，加载和错误状态直接驱动 UI。

```vue
<!-- src/views/LoginView.vue -->
<el-form label-position="top" @submit.prevent="submit">
  <el-form-item label="用户名">
    <el-input v-model="form.username" autocomplete="username" />
  </el-form-item>
  <el-form-item label="密码">
    <el-input v-model="form.password" type="password" autocomplete="current-password" show-password />
  </el-form-item>
  <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" />
  <el-button type="primary" native-type="submit" :loading="loading">登录</el-button>
</el-form>
```

### 6.2 script：组合状态和业务流程

页面只组织交互：调用 auth store 登录，根据后端返回的首次改密标志决定跳转，并在 `finally` 中可靠关闭 loading。具体 HTTP 细节留在 API 层。

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

### 6.3 style：局部样式

`scoped` 限制样式只作用于当前组件；跨页面复用的 `.page`、`.panel`、`.toolbar` 等放在 `src/style.css`。

```css
/* src/views/LoginView.vue */
<style scoped>
.login-panel {
  background: #fff;
  border-radius: 8px;
  max-width: 420px;
  padding: 28px;
  width: 100%;
}
</style>
```

其他页面沿用同一分工：模板负责展示，script 组合 API/store/router，局部 CSS 负责页面特有布局。列表页应明确处理加载中、空和错误状态；当前早期页面并非都已完整覆盖，新增页面不要继续省略。

## 7. 如何新增一个页面

以下以新增“设备”页 `/instruments` 为例。只创建实际需要的文件，不新增一层 service、hook 或包装器。

### 第 1 步：创建 View

新建 `src/views/InstrumentsView.vue`，保持现有 SFC 结构：

```vue
<template>
  <div class="page">
    <div class="toolbar"><h2>设备</h2></div>
    <section class="panel">
      <el-empty v-if="items.length === 0" description="暂无设备" />
      <el-table v-else :data="items" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { listInstruments, type Instrument } from '../api/instruments'

const items = ref<Instrument[]>([])
onMounted(async () => { items.value = await listInstruments() })
</script>
```

### 第 2 步：注册路由

在 `src/router/index.ts` 导入页面并添加路由。普通登录页不需要额外 meta；管理员页使用现有的 `meta.admin`。

```ts
import InstrumentsView from '../views/InstrumentsView.vue'

// routes 中
{ path: '/instruments', component: InstrumentsView }
```

项目对象权限不要在前端发明 `RequireProjectAccess` 一类组件；按现有模式从路由参数取得项目 ID，调用 API，让后端执行对象级授权。

### 第 3 步：添加 API 调用

新建 `src/api/instruments.ts`，复用 `request<T>()`。写请求的 Cookie、CSRF 和 `Idempotency-Key` 会由拦截器自动处理。

```ts
import { request } from './client'

export type Instrument = { id: string; name: string; status: string }

export function listInstruments() {
  return request<Instrument[]>({ url: '/instruments' })
}
```

页面只调用 `listInstruments()`，不要直接依赖 axios，也不要在组件里拼 `/api/v1`。

### 第 4 步：添加导航入口

在 `src/components/AppLayout.vue` 的 `navItems` 添加桌面入口；需要出现在移动端时，同时把标签加入 `mobileItems` 的白名单。

```ts
const items = [
  // 现有项目……
  { label: '设备', path: '/instruments', icon: Monitor }
]

const mobileItems = computed(() =>
  navItems.value.filter((item) => ['项目', '日报', '问题', '经验', '设备', '设置'].includes(item.label))
)
```

移动端底栏当前固定为 5 列；新增第 6 项时还需同步调整：

```css
/* src/components/AppLayout.vue */
.bottom-nav {
  grid-template-columns: repeat(5, 1fr);
}
```

最后运行 `npm run build`，同时手工检查登录保护、直接输入 URL、桌面侧栏和 768px 以下布局。

## 8. 单文件构建与部署

生产环境由 Go 服务器 `//go:embed static` 嵌入前端并通过 `r.NotFound` 提供 SPA fallback：所有非 `/api` 路径（包括 `/assets/*.js`）都返回 `index.html`，没有任何静态文件路由。因此构建产物必须是完全自包含的单文件 `index.html`，否则浏览器拿到 `text/html` 的“JS”会拒绝执行，页面空白。

[`vite.config.ts`](vite.config.ts) 据此做了三件事：

1. `inlineDynamicImports: true` + `cssCodeSplit: false`：所有动态 import 合并为单个 JS chunk，样式合并为单个 CSS 文件。
2. `assetsInlineLimit: 100000000`：少量静态资源全部 base64 内联。
3. 自定义 `singleFile` 插件在 `generateBundle` 阶段把入口 JS 和 CSS 直接内联进 `index.html`，产物只剩一个文件（约 1.6 MB）。

```ts
// vite.config.ts
build: {
  cssCodeSplit: false,
  assetsInlineLimit: 100000000,
  rollupOptions: { output: { inlineDynamicImports: true } }
}
```

路由保持 HTML5 history 模式：直接访问 `/projects/:id/issues` 这类深层链接时，fallback 同样返回 `index.html`，前端路由正常接管。

部署流程：`npm run build` 后把 `web-ui/dist/` 同步到 `go-server/static/`（`rm -rf go-server/static && cp -r web-ui/dist go-server/static`），再构建 Go 镜像嵌入。

项目曾使用 `vite-plugin-pwa`，现已移除：Service Worker 在内网 HTTP 非 localhost 环境本就不可用，且 `sw.js`、`registerSW.js`、`manifest.webmanifest` 等文件无法被上述后端正确提供。如果将来恢复 HTTPS 部署并修复静态文件路由，再评估是否重新引入 PWA。

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
| 主导航 | 左侧 200px 固定侧栏，显示全部允许入口 | 底部固定导航，只显示项目、日报、问题、经验、设置 |
| 内容区 | 左侧留出 200px，内边距 20px | 无左边距，底部留 72px 防止被导航遮挡 |
| 项目页 | 项目侧栏和仪表盘两列并排 | Element Plus tabs 在“列表/仪表盘”之间切换 |
| Issue 看板 | 4 列状态看板 | 单列纵向排列 |
| 通用面板 | 16px 内边距 | 12px 内边距 |

`AppLayout` 的结构切换：

```vue
<!-- src/components/AppLayout.vue -->
<aside v-if="!isMobile" class="nav">...</aside>
<main class="content"><RouterView /></main>
<nav v-if="isMobile" class="bottom-nav">...</nav>
```

`ProjectsView` 在移动端改变的不只是 CSS，而是交互结构，因此使用 `useMobile()` 分支：

```vue
<!-- src/views/ProjectsView.vue -->
<div v-if="isMobile" class="panel">
  <el-tabs v-model="mobileTab">
    <el-tab-pane label="列表" name="list"><ProjectSidebar /></el-tab-pane>
    <el-tab-pane label="仪表盘" name="dashboard"><Dashboard /></el-tab-pane>
  </el-tabs>
</div>
<div v-else class="projects-layout">
  <div class="panel"><ProjectSidebar /></div>
  <Dashboard />
</div>
```

纯布局变化则留在 CSS，例如 Issue 看板：

```css
/* src/views/IssuesView.vue */
@media (max-width: 768px) {
  .board {
    grid-template-columns: 1fr;
  }
}
```

新增页面至少检查 320px 宽度、768px 断点两侧、触控按钮大小、横向溢出以及底部导航遮挡。

## 10. 开发命令

在 `web-ui/` 目录执行：

```bash
cd /home/zhuhaofan/hiaf-lab-system/web-ui

# 按 package-lock.json 全新安装依赖，CI 和首次拉取代码时使用
npm ci

# 启动开发服务器：http://localhost:5173
# /api 请求会代理到 http://localhost:8000
npm run dev

# 先执行 vue-tsc --noEmit，再生成生产构建到 dist/
npm run build
```

对应的真实脚本定义：

```json
// package.json
{
  "scripts": {
    "dev": "vite --host 0.0.0.0",
    "build": "vue-tsc --noEmit && vite build",
    "preview": "vite preview --host 0.0.0.0"
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
