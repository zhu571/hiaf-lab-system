import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia, getActivePinia } from 'pinia'
import InstrumentMeasureView from '@/views/InstrumentMeasureView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { InstrumentSummary, WhitelistCommand } from '@/api/instruments'

// InstrumentMeasureView 页面测试（测试方案 §3.2 🟡）：executableCommands 过滤 red+去重、
// canOperate 权限显隐（控制台与白名单执行入口）、错误态与空态。

vi.mock('@/api/instruments', () => ({
  listInstruments: vi.fn(),
  getWhitelist: vi.fn(),
  getStatus: vi.fn(),
  executeCommand: vi.fn(),
  executeCommandWithMeta: vi.fn(),
  interpretCommand: vi.fn(),
  emergencyStop: vi.fn(),
  parseResult: vi.fn()
}))

vi.mock('@/api/testdata', () => ({
  createTestData: vi.fn()
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn()
}))

vi.mock('@/api/runs', () => ({
  listRuns: vi.fn()
}))

vi.mock('chart.js', () => {
  function ChartImpl(this: unknown, ..._args: unknown[]) {
    return { data: { datasets: [] }, options: {}, destroy: vi.fn(), update: vi.fn() }
  }
  const ChartMock = vi.fn(ChartImpl)
  Object.assign(ChartMock, {
    defaults: {
      color: '',
      borderColor: '',
      font: { family: '', size: 12 },
      scales: { linear: {}, category: {} },
      plugins: { legend: { position: '', labels: {} }, tooltip: {} }
    }
  })
  return { Chart: ChartMock }
})

import { listInstruments, getWhitelist, getStatus, executeCommand, parseResult } from '@/api/instruments'

function makeInstrument(overrides: Partial<InstrumentSummary> = {}): InstrumentSummary {
  return { id: 'ins_01', name: '射频电源', state: 'running', ...overrides }
}

function makeCommand(overrides: Partial<WhitelistCommand> = {}): WhitelistCommand {
  return {
    name: 'SET_VOLTAGE',
    description: '设置电压',
    risk: 'yellow',
    scpi: ':VOLT 5',
    timeout_ms: 5000,
    ...overrides
  }
}

async function mountView(role = 'maintainer') {
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
  const wrapper = mount(InstrumentMeasureView, {
    global: {
      plugins: [createTestI18n(), pinia],
      // ElSelect 真实渲染在 jsdom 触发递归更新；透传插槽 stub 使 option 文本仍可断言
      stubs: { teleport: true, ElInputNumber: true, ElSelect: { template: '<div class="el-select-stub"><slot /></div>' }, ElOption: { template: '<span class="el-option-stub">{{ $attrs.label }}</span>' } }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listInstruments).mockReset().mockResolvedValue([])
  vi.mocked(getWhitelist).mockReset().mockResolvedValue([])
  vi.mocked(getStatus).mockReset().mockResolvedValue({ instrument_id: 'ins_01', state: 'running', rate_limited: false })
  vi.mocked(executeCommand).mockReset()
  vi.mocked(parseResult).mockReset().mockResolvedValue(null)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('InstrumentMeasureView 仪器列表', () => {
  it('挂载加载仪器卡片：名称/ID/状态徽标渲染；错误态 el-alert + 重试', async () => {
    vi.mocked(listInstruments)
      .mockRejectedValueOnce(new Error('gateway down'))
      .mockResolvedValueOnce([makeInstrument()])
    const wrapper = await mountView('maintainer')
    await flushPromises()
    expect(wrapper.find('.el-alert').exists()).toBe(true)
    await wrapper.findAll('button').find((b) => b.text().trim() === '重试')!.trigger('click')
    await flushPromises()
    expect(listInstruments).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.ins-card').exists()).toBe(true)
    expect(wrapper.text()).toContain('射频电源')
    expect(wrapper.text()).toContain('运行中')

    // 空态：无仪器时 el-empty 提示
    vi.mocked(listInstruments).mockResolvedValue([])
    const emptyWrapper = await mountView('maintainer')
    expect(emptyWrapper.find('.el-empty__description').text()).toBe('暂无仪器')
  })

  it('展开详情：getStatus 加载 worker 状态；canOperate（maintainer）显示命令执行区', async () => {
    vi.mocked(listInstruments).mockResolvedValue([makeInstrument()])
    const wrapper = await mountView('maintainer')
    const detailBtn = wrapper.findAll('button').find((b) => b.text().trim() === '详情')!
    await detailBtn.trigger('click')
    await flushPromises()
    expect(getStatus).toHaveBeenCalledWith('ins_01')
    expect(wrapper.text()).toContain('执行命令')
  })
})

describe('InstrumentMeasureView 权限与白名单', () => {
  it('executableCommands：过滤 red 命令 + 同名去重，仅可操作角色可见执行区', async () => {
    vi.mocked(listInstruments).mockResolvedValue([makeInstrument()])
    vi.mocked(getWhitelist).mockResolvedValue([
      makeCommand({ name: 'SET_VOLTAGE', risk: 'yellow' }),
      makeCommand({ name: 'SET_VOLTAGE', risk: 'green' }), // 同名去重（后者重复）
      makeCommand({ name: 'EMERGENCY_HIGH', risk: 'red' }), // red 被过滤
      makeCommand({ name: 'READ_STATUS', risk: 'green' })
    ])
    const wrapper = await mountView('maintainer')
    const detailBtn = wrapper.findAll('button').find((b) => b.text().trim() === '详情')!
    await detailBtn.trigger('click')
    await flushPromises()
    const optionTexts = wrapper.findAll('.el-option-stub').map((c) => c.text())
    expect(optionTexts.join()).toContain('SET_VOLTAGE')
    expect(optionTexts.join()).toContain('READ_STATUS')
    // red 命令不进入可执行清单
    expect(optionTexts.join()).not.toContain('EMERGENCY_HIGH')
    // 展开时拉取白名单
    expect(getWhitelist).toHaveBeenCalled()
  })

  it('viewer：展开详情不渲染执行区，显示无权限提示', async () => {
    vi.mocked(listInstruments).mockResolvedValue([makeInstrument()])
    vi.mocked(getWhitelist).mockResolvedValue([makeCommand()])
    const wrapper = await mountView('viewer')
    const detailBtn = wrapper.findAll('button').find((b) => b.text().trim() === '详情')!
    await detailBtn.trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain('执行命令')
    expect(wrapper.text()).toContain('命令执行需要 maintainer 或 admin 权限')
  })
})
