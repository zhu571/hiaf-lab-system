import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminUsersView from '@/views/AdminUsersView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { UserInfo } from '@/api/auth'

// AdminUsersView 页面测试（测试方案 §3.2 🟡）：用户列表 + admin 专属操作按钮显隐（自身行隐藏）。

vi.mock('@/api/auth', () => ({
  listUsers: vi.fn(),
  createUser: vi.fn(),
  updateUser: vi.fn(),
  resetPassword: vi.fn(),
  listInvitationCodes: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, per_page: 20 }),
  createInvitationCode: vi.fn(),
  revokeInvitationCode: vi.fn(),
  login: vi.fn(),
  refresh: vi.fn(),
  me: vi.fn(),
  updateProfile: vi.fn(),
  logout: vi.fn()
}))

import { listUsers, updateUser } from '@/api/auth'

function makeUser(overrides: Partial<UserInfo> = {}): UserInfo {
  return {
    id: 'user_01',
    username: 'haofan',
    display_name: '郝帆',
    role: 'admin',
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh',
    ...overrides
  }
}

async function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore(pinia).user = makeUser({ id: 'admin_self', role: 'admin' })
  const wrapper = mount(AdminUsersView, {
    global: {
      plugins: [createTestI18n(), pinia],
      stubs: { teleport: true, ElSelect: true }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listUsers).mockReset()
  vi.mocked(updateUser).mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AdminUsersView 用户列表', () => {
  it('用户列表渲染（用户名/角色/状态），非本人行显示角色变更/重置密码/停用入口', async () => {
    vi.mocked(listUsers).mockResolvedValue([
      makeUser({ id: 'admin_self', username: 'haofan' }),
      makeUser({ id: 'user_02', username: 'zhangsan', role: 'viewer' }),
      makeUser({ id: 'user_03', username: 'lisi', role: 'member', disabled: true })
    ])
    const wrapper = await mountView()
    expect(listUsers).toHaveBeenCalled()
    expect(wrapper.text()).toContain('haofan')
    expect(wrapper.text()).toContain('zhangsan')
    // 自身行（admin_self）显示「当前账户」无操作按钮
    expect(wrapper.text()).toContain('当前账户')
    const actionButtons = wrapper.findAll('button').map((b) => b.text().trim())
    expect(actionButtons).toEqual(expect.arrayContaining(['角色变更', '重置密码', '停用']))
  })

  it('admin 专属操作：确认后调 updateUser（停用/启用），加载失败 StateBlock 错误 + 重试', async () => {
    vi.mocked(listUsers)
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce([makeUser({ id: 'user_02', username: 'zhangsan', role: 'viewer' })])
    vi.mocked(updateUser).mockResolvedValue({ data: makeUser({ id: 'user_02', disabled: true }), requestId: 'req-1' })
    const wrapper = await mountView()
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    await wrapper.findAll('button').find((b) => b.text().trim() === '重试')!.trigger('click')
    await flushPromises()
    expect(listUsers).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.state-block-error').exists()).toBe(false)
    // 停用按钮（ElMessageBox.confirm 默认 resolve）→ updateUser({disabled:true})
    const stops = wrapper.findAll('button').filter((b) => b.text().trim() === '停用')

    // el-table 存在隐藏测量副本按钮，限定 tbody 内真实行按钮
    const disableBtn = wrapper.findAll('button').find((b) => b.text().trim() === '停用' && b.element.closest('tbody'))!
    await disableBtn.trigger('click')
    await flushPromises()
    expect(updateUser).toHaveBeenCalledWith('user_02', { disabled: true })
  })
})
