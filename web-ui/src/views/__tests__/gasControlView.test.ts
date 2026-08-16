import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import GasControlView from '@/views/GasControlView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'

// GasControlView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 状态卡片渲染 +
// 权限区显隐（stub EventSource 与 chart.js，§5.2）。

vi.mock('@/api/instruments', () => ({
  gasCellStatus: vi.fn(),
  gasCellParams: vi.fn(),
  gasCellStart: vi.fn(),
  gasCellStop: vi.fn(),
  gasCellValve: vi.fn(),
  gasCellA5Max: vi.fn(),
  gasCellA5Clear: vi.fn()
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
      elements: { line: {} },
      scales: { linear: {}, category: {} },
      plugins: { legend: { position: '', labels: {} }, tooltip: {} }
    }
  })
  return { Chart: ChartMock }
})

import { gasCellStatus } from '@/api/instruments'

class EventSourceStub {
  static instances: EventSourceStub[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  close = vi.fn()
  constructor(_url: string) {
    EventSourceStub.instances.push(this)
  }
}

describe('GasControlView 挂载冒烟', () => {
  it('状态卡片渲染 + admin 显示控制区；EventSource 连接建立', async () => {
    vi.stubGlobal('EventSource', EventSourceStub)
    vi.mocked(gasCellStatus).mockResolvedValue({
      data: {
        'GasCell:A1:Pressure': { q: 'good', v: 101325 },
        'GasCell:Valve:Opening': { q: 'good', v: 50 }
      }
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
    const wrapper = mount(GasControlView, {
      global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true, ElInputNumber: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('气压控制')
    expect(wrapper.find('.status-card').exists()).toBe(true)
    expect(wrapper.find('.control-card').exists()).toBe(true)
    expect(EventSourceStub.instances.length).toBe(1)
    wrapper.unmount()
  })
})

beforeEach(() => {
  EventSourceStub.instances = []
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
