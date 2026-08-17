import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MobileTopBar from '@/layouts/MobileTopBar.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useProjectStore } from '@/stores/project'

// MobileTopBar 组件测试：一级页菜单/子页返回互斥 + @ask emit（AppLayout 接线点）。

const routeState = vi.hoisted(() => ({ current: { path: '/todos', meta: { titleKey: 'todos.title' } } }))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState.current,
  useRouter: () => ({ push: vi.fn(), back: vi.fn() })
}))

describe('MobileTopBar 挂载冒烟', () => {
  it('一级页渲染菜单键而非返回键，并分别 emit menu/ask', async () => {
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
    expect(wrapper.find('.menu-btn').exists()).toBe(true)
    await wrapper.find('.menu-btn').trigger('click')
    expect(wrapper.emitted('menu')).toHaveLength(1)
    await wrapper.find('.ask-btn').trigger('click')
    expect(wrapper.emitted('ask')).toHaveLength(1)
  })

  it('子页渲染返回键而非菜单键', () => {
    routeState.current = { path: '/manual', meta: { titleKey: 'nav.manual' } }
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(MobileTopBar, { global: { plugins: [createTestI18n(), pinia] } })
    expect(wrapper.find('.back-btn').exists()).toBe(true)
    expect(wrapper.find('.menu-btn').exists()).toBe(false)
  })
})

afterEach(() => {
  routeState.current = { path: '/todos', meta: { titleKey: 'todos.title' } }
  vi.restoreAllMocks()
})
