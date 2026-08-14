import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ProjectsView from '@/views/ProjectsView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'

// ProjectsView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 项目侧栏渲染 + canCreate 权限。

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([
    { id: 'proj_01', code: 'P01', name: '低温靶项目', short_name: '低温靶', description: '', status: 'active', visibility: 'internal' }
  ]),
  createProject: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

import { useProjectStore } from '@/stores/project'

describe('ProjectsView 挂载冒烟', () => {
  it('项目侧栏渲染项目项；viewer 隐藏新建项目按钮', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore(pinia).user = {
      id: 'user_01',
      username: 'viewer',
      display_name: 'Viewer',
      role: 'viewer',
      must_change_password: false,
      created_at: '2026-01-01T00:00:00+08:00',
      disabled: false,
      language: 'zh'
    }
    const wrapper = mount(ProjectsView, {
      global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true } }
    })
    await flushPromises()
    expect(useProjectStore().projects).toHaveLength(1)
    expect(wrapper.find('.project-item').text()).toContain('低温靶')
    expect(wrapper.text()).not.toContain('新建项目')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
