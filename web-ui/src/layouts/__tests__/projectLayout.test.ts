import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ProjectLayout from '@/layouts/ProjectLayout.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useProjectStore } from '@/stores/project'

// ProjectLayout 组件测试（测试方案 §3.2 🟢 smoke）：挂载不崩 + 工作区头/页签/项目切换。

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([
    { id: 'proj_01', code: 'P01', name: '低温靶项目', short_name: '低温靶', description: '', status: 'active', visibility: 'internal' }
  ])
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'proj_01' }, path: '/projects/proj_01/issues' }),
  useRouter: () => ({ push: vi.fn() })
}))

describe('ProjectLayout 挂载冒烟', () => {
  it('项目工作区渲染：标题/阶段标签 + 7 个页签 + RouterView', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(ProjectLayout, {
      global: {
        plugins: [createTestI18n(), pinia],
        stubs: { RouterView: true, teleport: true, ElSelect: true }
      }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('低温靶项目')
    expect(wrapper.find('.workspace-head').exists()).toBe(true)
    // R6 §7.2：head + tabs 由吸顶容器包裹（sticky），返回按钮图标化带 aria-label
    expect(wrapper.find('.workspace-sticky').exists()).toBe(true)
    expect(wrapper.find('.back-btn').exists()).toBe(true)
    expect(wrapper.find('.back-btn').attributes('aria-label')).toBe('项目列表')
    const tabs = wrapper.findAll('.el-tabs__item').map((t) => t.text().trim())
    expect(tabs).toEqual(['概览', '日志', '问题', '批次', '数据', 'RF匹配', '装配'])
    expect(useProjectStore().currentId).toBe('proj_01')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
