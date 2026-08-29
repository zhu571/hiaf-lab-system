import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import SensorTrendChart from '@/components/business/SensorTrendChart.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { ChartGroup } from '@/components/business/SensorTrendChart.vue'

// SensorTrendChart 挂载测试（测试方案 §3.2 🔴 深测）：mock chart.js 断言
// props.groups → Chart data、windowKey 变化触发视图重置、zoom 事件 emit。
// 纯函数（wheelWindow/panWindow/sliceWindow/fitY）已有 T1 单测（sensorTrendChart.test.ts），不重复。
// 注意：Chart.register 已收口 utils/chartTheme.ts，本测试用 mock Chart.defaults 让真实 refreshDefaults 可运行。

vi.mock('chart.js', () => {
  // 组件以 new Chart() 构造：mock 实现必须是可构造的普通函数（箭头函数不可 new）
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
  // 与真实 chartTheme.refreshDefaults 写入结构对齐（Chart.defaults.scales/plugins 惰性对象）
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

import { Chart } from 'chart.js'

const chartMock = Chart as unknown as ReturnType<typeof vi.fn>
type MockChartInstance = {
  data: { datasets: Array<Record<string, unknown>> }
  options: { scales: { x: { min: number; max: number }; y: { min?: number; max?: number } } }
  scales: { x: { getValueForPixel: (px: number) => number } }
  chartArea: { width: number }
  update: ReturnType<typeof vi.fn>
  destroy: ReturnType<typeof vi.fn>
}

function makeGroup(name: string, values: Array<[number, number]>, dash: number[] = []): ChartGroup {
  return { name, color: '#1a86a2', dash, points: values.map(([time, value]) => ({ time, value })) }
}

async function mountChart(groups: ChartGroup[], windowKey = 'p:1h', windowSizeMs = 3600e3) {
  const wrapper = mount(SensorTrendChart, {
    props: { groups, windowKey, windowSizeMs },
    global: { plugins: [createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

function chartInstance(): MockChartInstance {
  return chartMock.mock.results[chartMock.mock.results.length - 1].value
}

beforeEach(() => {
  chartMock.mockClear()
  // jsdom 未实现 canvas.setPointerCapture，拖拽手势用例需 stub
  Element.prototype.setPointerCapture = vi.fn()
  Element.prototype.releasePointerCapture = vi.fn()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SensorTrendChart 挂载渲染', () => {
  it('props.groups 传入 Chart data：datasets 含各分组切片数据与颜色', async () => {
    const groups = [
      makeGroup('pressure', [
        [0, 1],
        [1000, 2],
        [2000, 3]
      ]),
      makeGroup('temperature', [
        [0, 10],
        [500, 20]
      ], [6, 4])
    ]
    await mountChart(groups)
    const chart = chartInstance()
    expect(chartMock).toHaveBeenCalledTimes(1)
    expect(chart.data.datasets).toHaveLength(2)
    expect(chart.data.datasets[0].label).toBe('pressure')
    expect(chart.data.datasets[0].data).toHaveLength(3)
    expect(chart.data.datasets[0].borderColor).toBe('#1a86a2')
    expect(chart.data.datasets[0].borderDash).toEqual([])
    expect(chart.data.datasets[1].label).toBe('temperature')
    expect(chart.data.datasets[1].borderDash).toEqual([6, 4])
  })

  it('windowKey 变化：视图复位为跟随模式，emit zoom-change {zoomed:false}', async () => {
    const wrapper = await mountChart([makeGroup('pressure', [[0, 1], [1000, 2]])])
    // 先进入缩放态（Ctrl+wheel 触发 zoom）
    const canvas = wrapper.find('canvas').element
    canvas.dispatchEvent(new WheelEvent('wheel', { ctrlKey: true, deltaY: 100, bubbles: true }))
    expect(wrapper.emitted('zoom-change')![0][0]).toEqual({ zoomed: true })
    // windowKey 变化 → 复位跟随模式
    await wrapper.setProps({ windowKey: 'p:6h' })
    await flushPromises()
    expect(wrapper.emitted('zoom-change')![1][0]).toEqual({ zoomed: false })
    expect(chartInstance().update).toHaveBeenCalled()
  })

  it('Ctrl+wheel 进入缩放模式：emit zoom-change {zoomed:true}，多次滚动窗口更新', async () => {
    const wrapper = await mountChart([makeGroup('pressure', [[0, 1], [1000, 2]])])
    const canvas = wrapper.find('canvas').element
    canvas.dispatchEvent(new WheelEvent('wheel', { ctrlKey: true, deltaY: 100, bubbles: true }))
    canvas.dispatchEvent(new WheelEvent('wheel', { ctrlKey: true, deltaY: 100, bubbles: true }))
    expect(wrapper.emitted('zoom-change')).toHaveLength(1)
    expect(wrapper.emitted('zoom-change')![0][0]).toEqual({ zoomed: true })
    const chart = chartInstance()
    expect(chart.options.scales.x.min).toBeLessThan(0)
    expect(chart.update).toHaveBeenCalled()
  })

  it('复位按钮点击：退出缩放态 emit zoom-change {zoomed:false}，X 窗口回跟随模式', async () => {
    const wrapper = await mountChart([makeGroup('pressure', [[0, 1], [1000, 2]])])
    const canvas = wrapper.find('canvas').element
    canvas.dispatchEvent(new WheelEvent('wheel', { ctrlKey: true, deltaY: 100, bubbles: true }))
    const before = chartInstance().options.scales.x.min
    await wrapper.find('.reset-btn').trigger('click')
    expect(wrapper.emitted('zoom-change')![1][0]).toEqual({ zoomed: false })
    // 复位后回到跟随模式窗口（lastT - windowSizeMs, lastT）
    const win = chartInstance().options.scales.x
    expect(win.min).not.toBe(before)
    expect(win.max - win.min).toBe(3600e3)
  })
})

describe('SensorTrendChart 数据联动', () => {
  it('groups 变化：差集清理 + 新增分组默认可见，datasets 重建', async () => {
    const wrapper = await mountChart([makeGroup('pressure', [[0, 1]])])
    const chart = chartInstance()
    expect(chart.data.datasets[0].hidden).toBeFalsy()
    await wrapper.setProps({
      groups: [
        makeGroup('pressure', [[0, 1]]),
        makeGroup('vacuum', [
          [0, 5],
          [1000, 6]
        ])
      ]
    })
    await flushPromises()
    expect(chart.data.datasets).toHaveLength(2)
    expect(chart.data.datasets[0].hidden).toBeFalsy()
    expect(chart.data.datasets[1].hidden).toBeFalsy()
    expect(chart.update).toHaveBeenCalled()
  })
})
