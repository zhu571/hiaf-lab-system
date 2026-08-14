import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DashboardView from '@/views/DashboardView.vue'
import { createTestI18n } from '@/test-utils/setup'

// DashboardView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 关键区块 + 空态。

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

describe('DashboardView 挂载冒烟', () => {
  it('仪表盘四区块渲染：待办空态/设备空态/简报/团队日报空态', async () => {
    const wrapper = mount(DashboardView, {
      global: { plugins: [createTestI18n()], stubs: { ElSelect: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('实验室仪表盘')
    expect(wrapper.text()).toContain('今日待办')
    expect(wrapper.text()).toContain('设备状态')
    expect(wrapper.text()).toContain('综合简报')
    expect(wrapper.text()).toContain('团队成员日报')
    // 空态（三处 StateBlock el-empty）
    expect(wrapper.findAll('.el-empty')).toHaveLength(3)
  })
})
