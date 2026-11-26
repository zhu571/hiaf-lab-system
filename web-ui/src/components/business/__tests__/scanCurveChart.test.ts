import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ScanCurveChart from '@/components/business/ScanCurveChart.vue'
import { createTestI18n } from '@/test-utils/setup'

const { chartDestroy, ChartMock } = vi.hoisted(() => {
  const destroy = vi.fn()
  const ctor = vi.fn(function (this: { destroy: typeof destroy }) {
    this.destroy = destroy
  })
  return { chartDestroy: destroy, ChartMock: ctor }
})

vi.mock('chart.js', () => ({ Chart: ChartMock }))
vi.mock('@/utils/chartTheme', () => ({
  refreshDefaults: vi.fn(),
  chartPalette: () => ['#1a86a2', '#4d9e6b'],
  LINE_DASHES: [[], [6, 4]]
}))
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))

import { api } from '@/api/client'

function mountChart() {
  return mount(ScanCurveChart, {
    props: { scanId: 'scan_01', columns: ['time', 'p', 't'] },
    global: {
      plugins: [createTestI18n()],
      stubs: {
        'el-empty': { props: ['description'], template: '<div class="empty">{{ description }}</div>' }
      }
    }
  })
}

const SAMPLE = 'time\tp\tt\n0\t1.0\t2.0\n1\t1.5\t2.5\n'

describe('ScanCurveChart', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue({ data: SAMPLE })
  })

  it('loads txt and renders a chart', async () => {
    const wrapper = mountChart()
    await flushPromises()
    expect(api.get).toHaveBeenCalledWith('/scans/scan_01/file/txt', { responseType: 'text' })
    expect(ChartMock).toHaveBeenCalled()
    expect(wrapper.find('canvas').exists()).toBe(true)
  })

  it('shows an error state when the file cannot be loaded', async () => {
    ;(api.get as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('boom'))
    const wrapper = mountChart()
    await flushPromises()
    expect(wrapper.find('.empty').text()).toContain('曲线加载失败')
  })

  it('destroys the chart on unmount', async () => {
    const wrapper = mountChart()
    await flushPromises()
    wrapper.unmount()
    expect(chartDestroy).toHaveBeenCalled()
  })
})
