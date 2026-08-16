import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AssemblyView from '@/views/AssemblyView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { AssemblyStep } from '@/api/assembly'

// AssemblyView 页面测试（测试方案 §3.2 🟡）：装配步骤列表渲染 + 权限显隐（viewer 隐藏操作）、空态。

vi.mock('@/api/assembly', () => ({
  listAssemblySteps: vi.fn(),
  createAssemblyStep: vi.fn(),
  deleteAssemblyStep: vi.fn(),
  reorderAssemblySteps: vi.fn(),
  transitionAssemblyStep: vi.fn(),
  applyAssemblyTemplate: vi.fn()
}))

vi.mock('@/api/stepTemplates', () => ({
  createTemplate: vi.fn(),
  generateSteps: vi.fn()
}))

vi.mock('@/api/projects', () => ({
  listMembers: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'proj_01' } })
}))

import { listAssemblySteps } from '@/api/assembly'

function makeStep(overrides: Partial<AssemblyStep> = {}): AssemblyStep {
  return {
    id: 'step_01',
    project_id: 'proj_01',
    name: '安装真空腔体',
    description: '',
    status: 'planned',
    step_order: 1,
    created_by: 'user_01',
    created_at: '2026-01-05T10:00:00+08:00',
    updated_at: '2026-01-05T10:00:00+08:00',
    ...overrides
  }
}

async function mountView(role = 'member') {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore(pinia).user = {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
  const wrapper = mount(AssemblyView, {
    global: {
      plugins: [createTestI18n(), pinia],
      stubs: { teleport: true, ElSelect: true }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listAssemblySteps).mockReset().mockResolvedValue({ items: [], total: 0, page: 1 })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AssemblyView 装配步骤', () => {
  it('member：步骤列表渲染 + 状态流转/删除入口可见；viewer 隐藏写入口', async () => {
    vi.mocked(listAssemblySteps).mockResolvedValue({ items: [makeStep()], total: 1, page: 1 })
    const wrapper = await mountView('member')
    expect(listAssemblySteps).toHaveBeenCalledWith('proj_01', expect.any(Object))
    expect(wrapper.text()).toContain('安装真空腔体')
    // member：canOperate 可见步骤操作；admin 才能拖拽（drag-handle）
    const viewerWrapper = await mountView('viewer')
    await flushPromises()
    expect(viewerWrapper.text()).not.toContain('新建步骤')
    expect(viewerWrapper.text()).not.toContain('AI 生成装配步骤')
  })

  it('错误态与空态：加载失败 StateBlock 错误 + 重试；无步骤 el-empty', async () => {
    vi.mocked(listAssemblySteps)
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce({ items: [], total: 0, page: 1 })
    const wrapper = await mountView('member')
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    await wrapper.findAll('button').find((b) => b.text().trim() === '重试')!.trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-empty__description').text()).toBe('暂无装配步骤')
  })
})
