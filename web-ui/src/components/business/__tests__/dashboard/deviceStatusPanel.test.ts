import { describe, it, expect, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DeviceStatusPanel from '@/components/business/dashboard/DeviceStatusPanel.vue'
import { createTestI18n } from '@/test-utils/setup'
import { gasCellStatus, listInstruments } from '@/api/instruments'

// DeviceStatusPanel 组件测试（R6 §7.1 拆分，方案附录 D）：设备卡/气压卡渲染、
// 在线判定与角标口径，逻辑从 DashboardView 等价平移后不变。

vi.mock('@/api/instruments', () => ({
  listInstruments: vi.fn(),
  gasCellStatus: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

const mockedListInstruments = vi.mocked(listInstruments)
const mockedGasCellStatus = vi.mocked(gasCellStatus)

async function mountPanel() {
  const wrapper = mount(DeviceStatusPanel, {
    global: { plugins: [createTestI18n()], stubs: { teleport: true } }
  })
  await flushPromises()
  return wrapper
}

describe('DeviceStatusPanel', () => {
  it('渲染仪器卡 + 气压控制卡，在线/离线状态正确', async () => {
    mockedListInstruments.mockResolvedValueOnce([
      { id: 'inst_1', name: '束流诊断', state: 'running' },
      { id: 'inst_2', name: '真空计', state: 'error' }
    ])
    mockedGasCellStatus.mockResolvedValueOnce({
      data: { 'GasCell:Piezo:Running': { q: 'good', v: 1 }, 'GasCell:Piezo:A1': { q: 'good', v: 1.02 } }
    })
    const wrapper = await mountPanel()
    const cards = wrapper.findAll('.device-card')
    expect(cards).toHaveLength(3)
    expect(wrapper.text()).toContain('束流诊断')
    expect(wrapper.text()).toContain('真空计')
    expect(wrapper.text()).toContain('气压控制')
    // 在线统计：仪器 1 台在线 + 气压卡在线 = 2/3
    expect(wrapper.text()).toContain('2/3 在线')
    expect(wrapper.text()).toContain('1.02 Pa')
  })

  it('空态显示 el-empty，气压卡仍渲染', async () => {
    mockedListInstruments.mockResolvedValueOnce([])
    mockedGasCellStatus.mockResolvedValueOnce({ data: {} })
    const wrapper = await mountPanel()
    expect(wrapper.find('.el-empty').exists()).toBe(true)
    expect(wrapper.find('.gas-card').exists()).toBe(true)
  })
})