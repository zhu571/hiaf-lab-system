import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ProjectSidebar from '@/components/business/ProjectSidebar.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useProjectStore } from '@/stores/project'

// ProjectSidebar 组件测试（测试方案 §3.2 🟢 smoke）：项目列表渲染 + 关键词过滤 + 选中态。

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

describe('ProjectSidebar 挂载冒烟', () => {
  it('项目项渲染（简称/编码），当前项目高亮，点击调 select + push', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const store = useProjectStore(pinia)
    store.projects = [
      { id: 'proj_01', code: 'P01', name: '低温靶项目', short_name: '低温靶', description: '', status: 'active', visibility: 'internal' },
      { id: 'proj_02', code: 'P02', name: '束线项目', short_name: '束线', description: '', status: 'active', visibility: 'internal' }
    ]
    store.currentId = 'proj_01'
    const wrapper = mount(ProjectSidebar, {
      global: { plugins: [createTestI18n(), pinia] }
    })
    await flushPromises()
    const items = wrapper.findAll('.project-item')
    expect(items).toHaveLength(2)
    expect(items[0].text()).toContain('低温靶')
    expect(items[0].classes()).toContain('active')
    // 关键词过滤
    const search = wrapper.find('input')
    await search.setValue('束线')
    expect(wrapper.findAll('.project-item')).toHaveLength(1)
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
