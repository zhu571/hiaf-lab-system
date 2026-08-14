import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import RunDetailView from '@/views/RunDetailView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { ExperimentRun, RunStep } from '@/api/runs'

// RunDetailView 页面测试（测试方案 §3.2 🟡）：transitionMap 状态按钮集、
// stepTransitions 步骤流转、canEdit/canSaveTemplate 权限显隐、错误态与重试。

vi.mock('@/api/runs', () => ({
  getRun: vi.fn(),
  transitionRun: vi.fn(),
  updateRun: vi.fn(),
  deleteRun: vi.fn(),
  listRunSteps: vi.fn(),
  createRunStep: vi.fn(),
  updateRunStep: vi.fn(),
  deleteRunStep: vi.fn(),
  applyRunTemplate: vi.fn(),
  addReportLink: vi.fn(),
  removeReportLink: vi.fn()
}))

vi.mock('@/api/testdata', () => ({
  listTestData: vi.fn()
}))

vi.mock('@/api/logs', () => ({
  listReports: vi.fn()
}))

vi.mock('@/api/stepTemplates', () => ({
  generateSteps: vi.fn(),
  createTemplate: vi.fn(),
  listTemplates: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'run_01' } }),
  useRouter: () => ({ push: vi.fn(), back: vi.fn() })
}))

import { getRun, transitionRun, listRunSteps } from '@/api/runs'
import { listTestData } from '@/api/testdata'
import { listReports } from '@/api/logs'

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

function makeStep(overrides: Partial<RunStep> = {}): RunStep {
  return {
    id: 'step_01',
    run_id: 'run_01',
    name: '抽真空',
    status: 'planned',
    step_order: 1,
    created_at: '2026-01-02T10:05:00+08:00',
    updated_at: '2026-01-02T10:05:00+08:00',
    ...overrides
  }
}

async function mountView(role = 'member') {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().user = {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
  const wrapper = mount(RunDetailView, {
    global: {
      plugins: [createTestI18n(), pinia],
      stubs: { teleport: true, ElSelect: true }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(getRun).mockReset()
  vi.mocked(transitionRun).mockReset()
  vi.mocked(listRunSteps).mockReset().mockResolvedValue({ items: [], total: 0 })
  vi.mocked(listTestData).mockReset().mockResolvedValue({ items: [], total: 0, page: 1 })
  vi.mocked(listReports).mockReset().mockResolvedValue({ items: [], total: 0, page: 1 })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RunDetailView 详情', () => {
  it('member：加载详情渲染元信息，planned 状态显示「开始/终止」流转按钮与编辑/删除入口', async () => {
    vi.mocked(getRun).mockResolvedValue(makeRun())
    const wrapper = await mountView('member')
    expect(getRun).toHaveBeenCalledWith('run_01')
    expect(wrapper.text()).toContain('冷启动测试')
    expect(wrapper.text()).toContain('开始')
    expect(wrapper.text()).toContain('中止')
    expect(wrapper.text()).toContain('编辑元数据')
    expect(wrapper.text()).toContain('删除')
  })

  it('active 状态：显示「暂停/完成/终止」三按钮（transitionMap 状态机）', async () => {
    vi.mocked(getRun).mockResolvedValue(makeRun({ status: 'active' }))
    const wrapper = await mountView('member')
    expect(wrapper.text()).toContain('暂停')
    expect(wrapper.text()).toContain('完成')
    expect(wrapper.text()).toContain('中止')
  })

  it('viewer：只读——流转/编辑/删除入口全部隐藏', async () => {
    vi.mocked(getRun).mockResolvedValue(makeRun())
    const wrapper = await mountView('viewer')
    expect(wrapper.text()).toContain('冷启动测试')
    // viewer 无流转/编辑/删除/新建步骤按钮（「开始时间」等描述性文本仍存在，按按钮集断言）
    const buttonTexts = wrapper.findAll('button').map((b) => b.text().trim())
    expect(buttonTexts).not.toContain('开始')
    expect(buttonTexts).not.toContain('中止')
    expect(buttonTexts).not.toContain('编辑元数据')
    expect(buttonTexts).not.toContain('删除')
    expect(buttonTexts).not.toContain('手动创建')
  })

  it('加载失败：错误框 + 重试按钮重新拉取', async () => {
    vi.mocked(getRun)
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce(makeRun())
    const wrapper = await mountView('member')
    await flushPromises()
    expect(wrapper.find('.error-box').exists()).toBe(true)
    await wrapper.find('.error-box button').trigger('click')
    await flushPromises()
    expect(getRun).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.error-box').exists()).toBe(false)
  })

  it('状态流转：点「开始」调 transitionRun 并重新加载详情', async () => {
    vi.mocked(getRun)
      .mockResolvedValueOnce(makeRun())
      .mockResolvedValueOnce(makeRun({ status: 'active' }))
    vi.mocked(transitionRun).mockResolvedValue(makeRun({ status: 'active' }))
    const wrapper = await mountView('member')
    const startBtn = wrapper.findAll('button').find((b) => b.text().trim() === '开始')!
    await startBtn.trigger('click')
    await flushPromises()
    expect(transitionRun).toHaveBeenCalledWith('run_01', 'start')
    expect(getRun).toHaveBeenCalledTimes(2)
  })
})

describe('RunDetailView 步骤面板', () => {
  it('步骤 tab：列表渲染 + stepTransitions 按钮（planned → 开始/取消）；member 可见操作', async () => {
    vi.mocked(getRun).mockResolvedValue(makeRun())
    vi.mocked(listRunSteps).mockResolvedValue({ items: [makeStep()], total: 1 })
    const wrapper = await mountView('member')
    const stepsTab = wrapper.findAll('.el-tabs__item').find((t) => t.text().trim() === '步骤')!
    await stepsTab.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('抽真空')
    expect(wrapper.text()).toContain('开始')
    expect(wrapper.text()).toContain('取消')
  })
})
