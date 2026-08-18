import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import ProjectDashboard from '@/components/business/ProjectDashboard.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import { getMembers } from '@/api/projects'
import { listProjectLogs } from '@/api/logs'
import { listProjectIssues } from '@/api/issues'

// ProjectDashboard 组件测试（测试方案 §3.2 🟢 smoke）：挂载不崩 + 阶段流程/指标/成员区块。

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([]),
  listMembers: vi.fn().mockResolvedValue([
    { project_id: 'proj_01', user_id: 'user_01', username: 'haofan', role: 'owner', status: 'active' }
  ]),
  getMembers: vi.fn().mockResolvedValue([
    { project_id: 'proj_01', user_id: 'user_01', username: 'haofan', role: 'owner', status: 'active' }
  ]),
  transitionProject: vi.fn()
}))

vi.mock('@/api/logs', () => ({
  listProjectLogs: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1 }),
  listReports: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1 })
}))

vi.mock('@/api/issues', () => ({
  listIssues: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1 }),
  listProjectIssues: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1 })
}))

const route = reactive({ params: { id: 'proj_01' } })

vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn() })
}))

describe('ProjectDashboard 挂载冒烟', () => {
  it('项目总览渲染：阶段流程 + 指标 + 成员列表', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useProjectStore(pinia).projects = [
      { id: 'proj_01', code: 'P01', name: '低温靶项目', short_name: '低温靶', description: '', status: 'active', visibility: 'internal' }
    ]
    useProjectStore(pinia).currentId = 'proj_01'
    useAuthStore(pinia).user = {
      id: 'user_01',
      username: 'admin',
      display_name: 'Admin',
      role: 'admin',
      must_change_password: false,
      created_at: '2026-01-01T00:00:00+08:00',
      disabled: false,
      language: 'zh'
    }
    const wrapper = mount(ProjectDashboard, {
      global: {
        plugins: [createTestI18n(), pinia],
        stubs: { teleport: true, RouterLink: { template: '<a><slot /></a>' }, ElSelect: true }
      }
    })
    await flushPromises()
    expect(wrapper.find('.stage-flow').exists()).toBe(true)
    expect(wrapper.find('.metric-grid').exists()).toBe(true)
    expect(wrapper.text()).toContain('项目成员')
    expect(wrapper.text()).toContain('haofan')
  })

  it('路由切换先于 store 同步时按路由项目加载', async () => {
    route.params.id = 'proj_02'
    const pinia = createPinia()
    setActivePinia(pinia)
    const store = useProjectStore(pinia)
    store.projects = [
      { id: 'proj_01', code: 'P01', name: '项目一', short_name: '项目一', description: '', status: 'active', visibility: 'internal' },
      { id: 'proj_02', code: 'P02', name: '项目二', short_name: '项目二', description: '', status: 'active', visibility: 'internal' }
    ]
    store.currentId = 'proj_01'

    mount(ProjectDashboard, {
      global: {
        plugins: [createTestI18n(), pinia],
        stubs: { teleport: true, RouterLink: { template: '<a><slot /></a>' }, ElSelect: true }
      }
    })
    await flushPromises()

    expect(vi.mocked(getMembers)).toHaveBeenLastCalledWith('proj_02')
    expect(vi.mocked(listProjectLogs)).toHaveBeenLastCalledWith('proj_02', { per_page: 5 })
    expect(vi.mocked(listProjectIssues)).toHaveBeenLastCalledWith('proj_02', { per_page: 5, sort: 'created', order: 'desc' })
  })
})

afterEach(() => {
  route.params.id = 'proj_01'
  vi.restoreAllMocks()
})
