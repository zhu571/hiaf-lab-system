import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MobileTopBar from '@/layouts/MobileTopBar.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useProjectStore } from '@/stores/project'

// MobileTopBar 组件测试（测试方案 §3.2 🟢 smoke）：标题渲染 + @ask emit（AppLayout 接线点）。

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/todos', meta: { titleKey: 'todos.title' } }),
  useRouter: () => ({ push: vi.fn(), back: vi.fn() })
}))

describe('MobileTopBar 挂载冒烟', () => {
  it('渲染标题与 AI 问答按钮，点击 emit ask；NO_BACK_PATHS 内不显示返回按钮', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useProjectStore(pinia)
    const wrapper = mount(MobileTopBar, {
      global: { plugins: [createTestI18n(), pinia] }
    })
    await flushPromises()
    expect(wrapper.find('.mobile-topbar').exists()).toBe(true)
    expect(wrapper.text()).toContain('我的待办')
    expect(wrapper.find('.back-btn').exists()).toBe(false)
    await wrapper.find('.ask-btn').trigger('click')
    expect(wrapper.emitted('ask')).toHaveLength(1)
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
