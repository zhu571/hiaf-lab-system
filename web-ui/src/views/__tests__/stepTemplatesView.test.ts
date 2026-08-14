import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import StepTemplatesView from '@/views/StepTemplatesView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'

// StepTemplatesView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 模板列表 + 空态 +
// admin 新建模板入口（StepItemsEditor 由 T3 深测覆盖，此处仅挂载冒烟）。

vi.mock('@/api/stepTemplates', () => ({
  listTemplates: vi.fn(),
  getTemplate: vi.fn(),
  createTemplate: vi.fn(),
  updateTemplate: vi.fn(),
  replaceTemplateItems: vi.fn(),
  deleteTemplate: vi.fn(),
  generateSteps: vi.fn()
}))

vi.mock('@/api/assembly', () => ({
  applyAssemblyTemplate: vi.fn()
}))

vi.mock('@/api/runs', () => ({
  applyRunTemplate: vi.fn(),
  listRuns: vi.fn()
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn(),
  listMembers: vi.fn()
}))

import { listTemplates } from '@/api/stepTemplates'

describe('StepTemplatesView 挂载冒烟', () => {
  it('模板列表渲染 + 空态；admin 显示新建模板入口', async () => {
    vi.mocked(listTemplates).mockResolvedValueOnce({
      items: [
        { id: 'tpl_1', name: '抽真空流程', kind: 'experiment', ai_generated: false, created_at: '2026-08-01T00:00:00+08:00', updated_at: '2026-08-01T00:00:00+08:00' }
      ],
      total: 1,
      page: 1,
      per_page: 20
    })
    const pinia = createPinia()
    setActivePinia(pinia)
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
    const wrapper = mount(StepTemplatesView, {
      global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true, ElSelect: true, ElInputNumber: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('步骤模板')
    expect(wrapper.text()).toContain('抽真空流程')
    expect(wrapper.text()).toContain('新建模板')

    vi.mocked(listTemplates).mockResolvedValueOnce({ items: [], total: 0, page: 1, per_page: 20 })
    const emptyWrapper = mount(StepTemplatesView, {
      global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true, ElSelect: true, ElInputNumber: true } }
    })
    await flushPromises()
    expect(emptyWrapper.find('.el-empty__description').text()).toBe('暂无步骤模板')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
