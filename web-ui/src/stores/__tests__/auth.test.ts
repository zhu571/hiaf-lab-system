import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from '../auth'
import type { UserInfo } from '../../api/auth'

// mock api 层：登录/登出/资料接口全部打桩，不发起任何真实网络请求。
// setCSRFToken / setLocale 保留真实实现（模块级变量 / localStorage，无副作用断言点）。
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

function makeUser(role: string, language = 'zh'): UserInfo {
  return {
    id: 'u1',
    username: 'tester',
    display_name: 'Tester',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language
  }
}

describe('auth store 权限逻辑', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('canReviewAgent：admin/maintainer 为 true，viewer 与未登录为 false', () => {
    const store = useAuthStore()
    expect(store.canReviewAgent).toBe(false)

    store.user = makeUser('viewer')
    expect(store.canReviewAgent).toBe(false)

    store.user = makeUser('maintainer')
    expect(store.canReviewAgent).toBe(true)

    store.user = makeUser('admin')
    expect(store.canReviewAgent).toBe(true)
  })

  it('isAdmin：仅 admin 角色为 true', () => {
    const store = useAuthStore()
    store.user = makeUser('maintainer')
    expect(store.isAdmin).toBe(false)

    store.user = makeUser('admin')
    expect(store.isAdmin).toBe(true)
  })

  it('未登录时 user 为 null、ready 为 false', () => {
    const store = useAuthStore()
    expect(store.user).toBeNull()
    expect(store.ready).toBe(false)
  })
})

describe('auth store 登录/登出 action', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('login 成功：写入 user、ready=true、返回响应数据', async () => {
    const user = makeUser('admin')
    const data = { user, csrf_token: 'csrf-1', must_change_password: false }
    mocks.login.mockResolvedValue(data)

    const store = useAuthStore()
    const result = await store.login('tester', 'secret')

    expect(mocks.login).toHaveBeenCalledWith('tester', 'secret')
    expect(result).toEqual(data)
    expect(store.user).toEqual(user)
    expect(store.ready).toBe(true)
  })

  it('login 失败：异常向上抛，user 保持 null', async () => {
    mocks.login.mockRejectedValue(new Error('密码错误'))

    const store = useAuthStore()
    await expect(store.login('tester', 'wrong')).rejects.toThrow('密码错误')
    expect(store.user).toBeNull()
    expect(store.ready).toBe(false)
  })

  it('loadMe 成功路径：user=me() 且 ready=true', async () => {
    const user = makeUser('maintainer')
    mocks.me.mockResolvedValue(user)

    const store = useAuthStore()
    await store.loadMe()

    expect(mocks.me).toHaveBeenCalled()
    expect(mocks.refresh).not.toHaveBeenCalled()
    expect(store.user).toEqual(user)
    expect(store.ready).toBe(true)
  })

  it('loadMe 首查失败：先 refresh 再重查 me()', async () => {
    const user = makeUser('viewer')
    mocks.me.mockRejectedValueOnce(new Error('token expired'))
    mocks.refresh.mockResolvedValue({ user, csrf_token: 'csrf-2', must_change_password: false })
    mocks.me.mockResolvedValue(user)

    const store = useAuthStore()
    await store.loadMe()

    expect(mocks.refresh).toHaveBeenCalled()
    expect(mocks.me).toHaveBeenCalledTimes(2)
    expect(store.user).toEqual(user)
    expect(store.ready).toBe(true)
  })

  it('loadMe 双失败：ready 仍为 true（finally），user 为 null', async () => {
    mocks.me.mockRejectedValue(new Error('network'))
    mocks.refresh.mockRejectedValue(new Error('network'))

    const store = useAuthStore()
    await expect(store.loadMe()).rejects.toThrow('network')
    expect(store.ready).toBe(true)
    expect(store.user).toBeNull()
  })

  it('logout：调用 api logout 并清空 user', async () => {
    mocks.logout.mockResolvedValue({ success: true })

    const store = useAuthStore()
    store.user = makeUser('admin')
    await store.logout()

    expect(mocks.logout).toHaveBeenCalled()
    expect(store.user).toBeNull()
  })

  it('setLanguage：更新 user 并调用 setLocale', async () => {
    const updated = makeUser('admin', 'en')
    mocks.updateProfile.mockResolvedValue(updated)

    const store = useAuthStore()
    await store.setLanguage('en')

    expect(mocks.updateProfile).toHaveBeenCalledWith({ language: 'en' })
    expect(store.user).toEqual(updated)
  })
})
