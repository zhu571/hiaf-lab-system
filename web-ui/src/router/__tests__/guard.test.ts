import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import router from '../index'
import { resolveRouteGuard } from '../guard'
import { useAuthStore } from '../../stores/auth'
import { makeUser } from '../../test-utils/factories'

// 路由守卫矩阵（方案 §3.5）：复用 router/index.ts:100 导出的真实 router 实例
// （routes 内联未导出、beforeEach 闭包引 auth store，重建即失去钩子；
//  jsdom 实现 History API，createWebHistory 可直接工作）。
// 路由解析/守卫不触发懒加载组件 import（组件仅在实际渲染时才加载），放行用例无真实网络依赖。
// 每个用例前置真实 pinia 并直接预置 user 态，auth.loadMe 经 api/auth 打桩兜底。

const mocks = vi.hoisted(() => ({
  login: vi.fn(),
  me: vi.fn(),
  refresh: vi.fn(),
  updateProfile: vi.fn(),
  logout: vi.fn()
}))

vi.mock('../../api/auth', () => ({
  login: mocks.login,
  me: mocks.me,
  refresh: mocks.refresh,
  updateProfile: mocks.updateProfile,
  logout: mocks.logout
}))

beforeEach(async () => {
  vi.clearAllMocks()
  // 新 pinia：守卫执行时 useAuthStore() 取到的是当次用例预置的角色态
  setActivePinia(createPinia())
  // 复位路由到 public 页（user=null 时 /login 全规则放行）
  await router.push('/login')
})

describe('路由守卫：未登录与会话失效', () => {
  it('未登录（loadMe 双失败）访问 /todos → 重定向 /login', async () => {
    mocks.me.mockRejectedValue(new Error('401'))
    mocks.refresh.mockRejectedValue(new Error('401'))

    await router.push('/todos')
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('ready=true 但 user=null（会话态丢失）→ /login，且不再调用 loadMe', async () => {
    const store = useAuthStore()
    store.ready = true

    await router.push('/todos')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(mocks.me).not.toHaveBeenCalled()
  })
})

describe('路由守卫：角色权限（admin/reviewer meta）', () => {
  it('viewer 访问 /admin/users（meta.admin）→ 重定向 /projects', async () => {
    const store = useAuthStore()
    store.user = makeUser({ role: 'viewer' })
    store.ready = true

    await router.push('/admin/users')
    expect(router.currentRoute.value.path).toBe('/projects')
  })

  it('viewer 访问 /agent-candidates（meta.reviewer）→ 重定向 /projects', async () => {
    const store = useAuthStore()
    store.user = makeUser({ role: 'viewer' })
    store.ready = true

    await router.push('/agent-candidates')
    expect(router.currentRoute.value.path).toBe('/projects')
  })

  it('maintainer 访问 /agent-candidates（meta.reviewer）→ 放行', async () => {
    const store = useAuthStore()
    store.user = makeUser({ role: 'maintainer' })
    store.ready = true

    await router.push('/agent-candidates')
    expect(router.currentRoute.value.path).toBe('/agent-candidates')
  })
})

describe('路由守卫：must_change_password', () => {
  it('must_change_password 用户访问 /todos → 重定向 /settings', async () => {
    const store = useAuthStore()
    store.user = makeUser({ role: 'viewer', must_change_password: true })
    store.ready = true

    await router.push('/todos')
    expect(router.currentRoute.value.path).toBe('/settings')
  })

  it('must_change_password 用户访问 /settings 本身 → 放行', async () => {
    const store = useAuthStore()
    store.user = makeUser({ role: 'viewer', must_change_password: true })
    store.ready = true

    await router.push('/settings')
    expect(router.currentRoute.value.path).toBe('/settings')
  })
})

describe('路由守卫：public 与兼容重定向', () => {
  it('/login（meta.public）未登录直接放行，不触发 loadMe', async () => {
    await router.push('/login')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(mocks.me).not.toHaveBeenCalled()
  })

  it('兼容重定向：/issues → /projects', async () => {
    const store = useAuthStore()
    store.user = makeUser({ role: 'admin' })
    store.ready = true

    await router.push('/issues')
    expect(router.currentRoute.value.path).toBe('/projects')
  })

  it('兼容重定向：/runs/:id → /experiment-runs/:id', async () => {
    const store = useAuthStore()
    store.user = makeUser({ role: 'admin' })
    store.ready = true

    await router.push('/runs/run-123')
    // 函数化 redirect：:id 参数插值进目标 path，旧链接不再以字面 :id 请求后端 404。
    expect(router.currentRoute.value.path).toBe('/experiment-runs/run-123')
    expect(router.currentRoute.value.params.id).toBe('run-123')
  })
})

// S0 抽出的守卫纯函数（重构方案 §3.6）：四规则逐条断言返回值，意图与原 beforeEach 内联逻辑一致。
describe('resolveRouteGuard 纯函数：四规则', () => {
  // 最小 RouteLocation 形态：纯函数只消费 path 与 meta
  const to = (path: string, meta: Record<string, unknown> = {}) => ({ path, meta }) as never

  it('规则1 未登录：非 public 且 user=null → /login', () => {
    expect(resolveRouteGuard(to('/todos'), { ready: true, user: null })).toBe('/login')
  })

  it('规则1 未登录且未 ready（loadMe 未完成）同样 → /login', () => {
    expect(resolveRouteGuard(to('/todos'), { ready: false, user: null })).toBe('/login')
  })

  it('规则1 public 路由（/login）无 user 放行', () => {
    expect(resolveRouteGuard(to('/login', { public: true }), { ready: true, user: null })).toBeUndefined()
  })

  it('规则2 admin 越权：viewer/maintainer 访问 meta.admin → /projects，admin 放行', () => {
    const adminRoute = to('/admin/users', { admin: true })
    expect(resolveRouteGuard(adminRoute, { ready: true, user: makeUser({ role: 'viewer' }) })).toBe('/projects')
    expect(resolveRouteGuard(adminRoute, { ready: true, user: makeUser({ role: 'maintainer' }) })).toBe('/projects')
    expect(resolveRouteGuard(adminRoute, { ready: true, user: makeUser({ role: 'admin' }) })).toBeUndefined()
  })

  it('规则3 reviewer 越权：viewer → /projects，maintainer/admin 放行', () => {
    const reviewRoute = to('/agent-candidates', { reviewer: true })
    expect(resolveRouteGuard(reviewRoute, { ready: true, user: makeUser({ role: 'viewer' }) })).toBe('/projects')
    expect(resolveRouteGuard(reviewRoute, { ready: true, user: makeUser({ role: 'maintainer' }) })).toBeUndefined()
    expect(resolveRouteGuard(reviewRoute, { ready: true, user: makeUser({ role: 'admin' }) })).toBeUndefined()
  })

  it('规则4 must_change_password：非 /settings 强制 /settings，/settings 本身放行', () => {
    const pendingUser = makeUser({ role: 'viewer', must_change_password: true })
    expect(resolveRouteGuard(to('/todos'), { ready: true, user: pendingUser })).toBe('/settings')
    expect(resolveRouteGuard(to('/settings'), { ready: true, user: pendingUser })).toBeUndefined()
  })
})

describe('路由守卫：catch-all 404', () => {
  it('未匹配路径（登录态）→ 重定向 /', async () => {
    const store = useAuthStore()
    store.user = makeUser({ role: 'admin' })
    store.ready = true

    await router.push('/no-such-page')
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('未匹配路径（未登录）→ catch-all 回 / 后由守卫送去 /login', async () => {
    await router.push('/no-such-page')
    expect(router.currentRoute.value.path).toBe('/login')
  })
})

describe('路由可达性：28 条组件路由懒加载全部可解析', () => {
  it('admin 登录态逐条访问全部组件路由，均到达目标路径（lazy import 模块无解析失败）', async () => {
    const store = useAuthStore()
    store.user = makeUser({ role: 'admin' })
    store.ready = true

    // router/index.ts:6-32,37 全部 28 个懒加载组件路由（含 /projects/:id 6 子页、/daily-report 2 子页）
    const routes = [
      '/',
      '/login',
      '/projects',
      '/daily-report',
      '/daily-report/history',
      '/projects/proj-1',
      '/projects/proj-1/issues',
      '/projects/proj-1/experiment-runs',
      '/projects/proj-1/test-data',
      '/projects/proj-1/rf-matching',
      '/projects/proj-1/assembly',
      '/experiment-runs/run-1',
      '/step-templates',
      '/attachments',
      '/instrument-measure',
      '/gas-control',
      '/sensors',
      '/todos',
      '/experiences',
      '/audit',
      '/alerts',
      '/settings',
      '/manual',
      '/daily-reports/rep-1',
      '/admin/users',
      '/agent-candidates'
    ]
    for (const path of routes) {
      await router.push(path)
      expect(router.currentRoute.value.path).toBe(path)
    }
  })
})
