import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import router from '../index'
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
