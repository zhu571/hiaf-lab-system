import { describe, it, expect, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import DashboardView from '@/views/DashboardView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import type { UserInfo } from '@/api/auth'
import type { Project } from '@/api/projects'

// DashboardView 页面测试（测试方案 §3.2 🟢 smoke）：R6 工作台化后断言随迁——
// 问候头条 + 快捷操作角色过滤 + 四块面板 + 三处空态。面板数据经子组件各自的
// useAsyncData/useDashboardReports 取数（模块级 mock 覆盖，拆分后口径不变）。

vi.mock('@/api/instruments', () => ({
  listInstruments: vi.fn().mockResolvedValue([]),
  gasCellStatus: vi.fn().mockResolvedValue({ data: {} })
}))

vi.mock('@/api/logs', () => ({
  listReports: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1 })
}))

vi.mock('@/api/todos', () => ({
  listTodos: vi.fn().mockResolvedValue([]),
  createTodo: vi.fn(),
  doneTodo: vi.fn(),
  deferTodo: vi.fn(),
  llmParse: vi.fn(),
  llmAdd: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

function makeUser(role: string): UserInfo {
  return {
    id: 'user_01',
    username: 'testuser',
    display_name: '测试员',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
}

function makeProject(id: string, name: string): Project {
  return { id, code: id.toUpperCase(), name, short_name: '', description: '', status: 'active', visibility: 'private' }
}

async function mountDashboard(role: string, projectList: Project[] = []) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().user = makeUser(role)
  useProjectStore().projects = projectList
  const wrapper = mount(DashboardView, {
    global: { plugins: [createTestI18n(), pinia], stubs: { ElSelect: true, teleport: true } }
  })
  await flushPromises()
  return wrapper
}

describe('DashboardView 工作台冒烟', () => {
  it('工作台渲染：问候头条 + 四块面板 + 三处空态', async () => {
    const wrapper = await mountDashboard('maintainer', [makeProject('proj_01', '低温靶')])
    // 问候头条：displayName 随问候语渲染（时段 key 由当前小时决定，只断言姓名）
    expect(wrapper.find('.workspace-banner').exists()).toBe(true)
    expect(wrapper.find('.greeting-title').exists()).toBe(true)
    expect(wrapper.text()).toContain('测试员')
    // 四块面板
    expect(wrapper.text()).toContain('今日待办')
    expect(wrapper.text()).toContain('设备状态')
    expect(wrapper.text()).toContain('综合简报')
    expect(wrapper.text()).toContain('团队成员日报')
    // 空态（三处 StateBlock el-empty：待办/设备/成员日报；简报恒渲染 7 天卡片）
    expect(wrapper.findAll('.el-empty')).toHaveLength(3)
  })

  it('快捷操作按角色过滤：viewer 隐藏写日报/新建批次，maintainer 且有项目时全量可见', async () => {
    const viewer = await mountDashboard('viewer')
    expect(viewer.text()).not.toContain('写日报')
    expect(viewer.text()).not.toContain('新建批次')
    expect(viewer.text()).toContain('新建待办')
    expect(viewer.text()).toContain('AI 问答')

    const maintainer = await mountDashboard('maintainer', [makeProject('proj_01', '低温靶')])
    expect(maintainer.text()).toContain('写日报')
    expect(maintainer.text()).toContain('新建批次')
  })

  it('项目列表为空时隐藏新建批次快捷操作', async () => {
    const wrapper = await mountDashboard('maintainer')
    expect(wrapper.text()).toContain('写日报')
    expect(wrapper.text()).not.toContain('新建批次')
  })
})