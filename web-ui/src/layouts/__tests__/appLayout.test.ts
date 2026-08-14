import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AppLayout from '@/layouts/AppLayout.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAskDialog } from '@/composables/useAskDialog'
import { useAuthStore } from '@/stores/auth'
import type { UserInfo } from '@/api/auth'

// AppLayout 组件测试（测试方案 §3.2 🟢 smoke）：
// 侧栏导航渲染、.nav-ask 按钮触发 openAskDialog、viewer 角色导航项过滤（filterNavByRole）。

vi.mock('@/api/agent', () => ({
  listAgentCandidates: vi.fn()
}))

vi.mock('@/api/ask', () => ({
  askChat: vi.fn(),
  askHistory: vi.fn(),
  askHistoryDetail: vi.fn(),
  newIdempotencyKey: vi.fn(() => 'idem_1')
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/', meta: {} }),
  useRouter: () => ({ push: vi.fn() })
}))

import { listAgentCandidates } from '@/api/agent'

function makeUser(role: string): UserInfo {
  return {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
}

async function mountLayout(role: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().user = makeUser(role)
  const wrapper = mount(AppLayout, {
    global: {
      plugins: [createTestI18n(), pinia],
      // ElDropdown 在 jsdom 触发 "Maximum recursive updates"（popper 内部循环），本用例不断言下拉菜单；
      // RouterLink 渲染为纯插槽容器（保留导航项文本），RouterView 空占位
      stubs: {
        RouterView: true,
        RouterLink: { template: '<a><slot /></a>' },
        teleport: true,
        ElDropdown: true
      }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listAgentCandidates).mockReset()
})

afterEach(() => {
  useAskDialog().askOpen.value = false
  vi.restoreAllMocks()
})

describe('AppLayout 导航渲染', () => {
  it('admin：主组/系统组导航项渲染，.nav-ask 存在，待审核徽章展示候选总数', async () => {
    vi.mocked(listAgentCandidates).mockResolvedValue({
      items: [],
      total: 3,
      page: 1,
      per_page: 1
    })
    const wrapper = await mountLayout('admin')
    expect(wrapper.find('.nav').exists()).toBe(true)
    expect(wrapper.text()).toContain('项目')
    expect(wrapper.text()).toContain('AI审核')
    expect(wrapper.find('.nav-ask').exists()).toBe(true)
    expect(wrapper.text()).toContain('AI 问答')
    expect(listAgentCandidates).toHaveBeenCalledWith(
      expect.objectContaining({ status: 'pending_review' })
    )
    await flushPromises()
    expect(wrapper.find('.nav-badge .el-badge__content').text()).toBe('3')
    wrapper.unmount()
  })

  it('viewer：maintainer/admin 专属导航项（AI审核/用户管理）被过滤，徽章不拉取', async () => {
    const wrapper = await mountLayout('viewer')
    expect(wrapper.text()).toContain('项目')
    expect(wrapper.text()).not.toContain('AI审核')
    expect(wrapper.text()).not.toContain('用户管理')
    expect(listAgentCandidates).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('点击 .nav-ask 触发 openAskDialog（useAskDialog 单例 askOpen 置真）', async () => {
    const wrapper = await mountLayout('maintainer')
    expect(useAskDialog().askOpen.value).toBe(false)
    await wrapper.find('.nav-ask').trigger('click')
    expect(useAskDialog().askOpen.value).toBe(true)
    wrapper.unmount()
  })
})
