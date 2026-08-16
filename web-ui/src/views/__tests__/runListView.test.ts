import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import RunListView from '@/views/RunListView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { ExperimentRun } from '@/api/runs'

// RunListView 页面测试（测试方案 §3.2 🟡）：批次卡片列表、状态过滤触发重查、
// viewer 无新建/AI 生成入口、错误态与空态。

vi.mock('@/api/runs', () => ({
  listRuns: vi.fn(),
  createRun: vi.fn(),
  applyRunTemplate: vi.fn()
}))

vi.mock('@/api/stepTemplates', () => ({
  generateSteps: vi.fn(),
  createTemplate: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'proj_01' } }),
  useRouter: () => ({ push: vi.fn() })
}))

import { listRuns } from '@/api/runs'

function makeRun(overrides: Partial<ExperimentRun> = {}): ExperimentRun {
  return {
    id: 'run_01',
    project_id: 'proj_01',
    name: '冷启动测试',
    run_type: 'cooldown',
    status: 'planned',
    gas_type: 'Ar',
    pressure_unit: 'mbar',
    has_beam: false,
    created_at: '2026-01-02T10:00:00+08:00',
    updated_at: '2026-01-02T10:00:00+08:00',
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
  const wrapper = mount(RunListView, {
    global: {
      plugins: [createTestI18n(), pinia],
      stubs: { teleport: true, ElSelect: true, ElInputNumber: true }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listRuns).mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RunListView 列表', () => {
  it('member：批次卡片列表渲染（名称/状态/标签），创建入口可见', async () => {
    vi.mocked(listRuns).mockResolvedValue({
      items: [
        makeRun({ id: 'run_01', name: '冷启动测试', status: 'planned' }),
        makeRun({ id: 'run_02', name: '束流调试', status: 'active' })
      ],
      total: 2,
      page: 1
    })
    const wrapper = await mountView('member')
    expect(listRuns).toHaveBeenCalledWith('proj_01', expect.objectContaining({ page: 1 }))
    const cards = wrapper.findAll('.run-card')
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('冷启动测试')
    expect(cards[1].text()).toContain('束流调试')
    expect(wrapper.text()).toContain('新建批次')
    expect(wrapper.text()).toContain('AI 生成步骤')
  })

  it('空态：无批次时 el-empty 提示（状态过滤 el-select 走真实下拉，jsdom stub 下仅断空态）', async () => {
    vi.mocked(listRuns).mockResolvedValue({ items: [], total: 0, page: 1 })
    const wrapper = await mountView('member')
    expect(wrapper.find('.el-empty__description').text()).toBe('暂无实验批次')
  })

  it('错误态与空态：加载失败 StateBlock 错误 + 重试；viewer 隐藏创建/AI 入口', async () => {
    vi.mocked(listRuns)
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce({ items: [makeRun()], total: 1, page: 1 })
    const wrapper = await mountView('viewer')
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    await wrapper.findAll('button').find((b) => b.text().trim() === '重试')!.trigger('click')
    await flushPromises()
    expect(listRuns).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.run-card').exists()).toBe(true)
    // viewer 无写入口
    expect(wrapper.text()).not.toContain('新建批次')
    expect(wrapper.text()).not.toContain('AI 生成步骤')
  })
})
