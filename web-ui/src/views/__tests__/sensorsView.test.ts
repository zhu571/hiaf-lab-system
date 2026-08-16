import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SensorsView from '@/views/SensorsView.vue'
import StateBlock from '@/components/base/StateBlock.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { SensorPoint } from '@/api/sensors'

// SensorsView 页面测试（测试方案 §3.2 🟡）：轮询（5s/30s fake timers）、
// latestError/historyError 警示、历史空态、图表渲染。
// 注意 1：usePolling 的 interval 由本视图 autoRefresh watch 创建，fake timers 须在挂载前启用。
// 注意 2：本视图的 StateBlock 未显式 import（依赖 vite unplugin-vue-components 自动导入，
// vitest 不加载该插件），测试经 global.components 注入真实 StateBlock 组件。

vi.mock('@/api/sensors', () => ({
  getLatest: vi.fn(),
  getHistory: vi.fn()
}))

vi.mock('chart.js', () => {
  function ChartImpl(this: unknown, ..._args: unknown[]) {
    return {
      data: { datasets: [] },
      options: { scales: { x: { min: 0, max: 1 }, y: {} } },
      scales: { x: { getValueForPixel: () => 100 } },
      chartArea: { width: 800 },
      update: vi.fn(),
      destroy: vi.fn()
    }
  }
  const ChartMock = vi.fn(ChartImpl)
  Object.assign(ChartMock, {
    defaults: {
      color: '',
      borderColor: '',
      font: { family: '', size: 12 },
      elements: { line: {} },
      scales: { linear: {}, category: {} },
      plugins: { legend: { position: '', labels: {} }, tooltip: {} }
    }
  })
  return { Chart: ChartMock }
})

import { getLatest, getHistory } from '@/api/sensors'

function makePoint(overrides: Partial<SensorPoint> = {}): SensorPoint {
  return {
    time: '2026-01-03T09:00:00+08:00',
    tag: 'pressure_cell_1',
    value: 101325,
    ...overrides
  }
}

async function mountView(fakeTimers = false) {
  if (fakeTimers) vi.useFakeTimers()
  const wrapper = mount(SensorsView, {
    global: {
      plugins: [createTestI18n()],
      components: { StateBlock },
      stubs: { ElSelect: true }
    }
  })
  if (fakeTimers) {
    await vi.advanceTimersByTimeAsync(0)
  } else {
    await flushPromises()
  }
  return wrapper
}

beforeEach(() => {
  vi.mocked(getLatest).mockReset().mockResolvedValue({ points: [] })
  vi.mocked(getHistory).mockReset().mockResolvedValue({ points: [] })
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('SensorsView 最新读数', () => {
  it('加载最新读数：reading-card 渲染测量项徽标/读数/单位', async () => {
    vi.mocked(getLatest).mockResolvedValue({
      points: [makePoint({ tag: 'pressure_cell_1', value: 1013.25 })]
    })
    const wrapper = await mountView()
    expect(getLatest).toHaveBeenCalledWith(expect.arrayContaining(['pressure', 'vacuum']))
    const cards = wrapper.findAll('.reading-card')
    expect(cards).toHaveLength(1)
    expect(cards[0].text()).toContain('压力')
    expect(cards[0].text()).toContain('1013')
    expect(cards[0].text()).toContain('Pa')
  })

  it('latest 错误态：StateBlock 错误警示 + 重试重新加载', async () => {
    vi.mocked(getLatest)
      .mockRejectedValueOnce(new Error('influx unavailable'))
      .mockResolvedValueOnce({ points: [makePoint()] })
    const wrapper = await mountView()
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    await wrapper.find('.state-block-retry').trigger('click')
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(false)
  })

  it('读数空态：无数据时 el-empty 提示', async () => {
    const wrapper = await mountView()
    expect(wrapper.find('.grid-empty').exists()).toBe(true)
    expect(wrapper.find('.el-empty__description').text()).toBe('暂无读数')
  })

  it('历史错误态：el-alert 警示 + 重试按钮触发 loadHistory', async () => {
    vi.mocked(getHistory)
      .mockRejectedValueOnce(new Error('history boom'))
      .mockResolvedValueOnce({ points: [] })
    const wrapper = await mountView()
    await flushPromises()
    const alert = wrapper.find('.chart-panel .el-alert')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toContain('history boom')
    await alert.find('button').trigger('click')
    await flushPromises()
    expect(getHistory).toHaveBeenCalledTimes(2)
  })
})

describe('SensorsView 轮询与图表', () => {
  it('轮询：autoRefresh 开启时 5s 触发 latest 刷新；关闭开关后不再轮询', async () => {
    const wrapper = await mountView(true)
    expect(getLatest).toHaveBeenCalledTimes(1)
    expect(getHistory).toHaveBeenCalledTimes(1)
    // 5s tick → latest 再次轮询（30s 未到，history 不触发）
    await vi.advanceTimersByTimeAsync(5000)
    expect(getLatest).toHaveBeenCalledTimes(2)
    expect(getHistory).toHaveBeenCalledTimes(1)
    // 关闭 autoRefresh → 轮询停止
    const switches = wrapper.findAll('.el-switch')
    await switches[0].trigger('click')
    await vi.advanceTimersByTimeAsync(15000)
    expect(getLatest).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('历史数据渲染图表：SensorTrendChart 挂载 + 图例分组显示', async () => {
    vi.mocked(getHistory).mockResolvedValue({
      points: [
        makePoint({ tag: 'pressure_cell_1', time: '2026-01-03T08:00:00+08:00', value: 100 }),
        makePoint({ tag: 'pressure_cell_1', time: '2026-01-03T09:00:00+08:00', value: 101 }),
        makePoint({ tag: 'temperature_cell', time: '2026-01-03T09:00:00+08:00', value: 300 })
      ]
    })
    const wrapper = await mountView()
    expect(wrapper.find('.trend-wrap').exists()).toBe(true)
    const legends = wrapper.findAll('.legend-item')
    expect(legends.map((l) => l.text())).toEqual(
      expect.arrayContaining([expect.stringContaining('pressure_cell_1')])
    )
    expect(wrapper.find('.chart-body .el-empty').exists()).toBe(false)
  })
})
