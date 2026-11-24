import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElectricalMonitorView from '@/views/ElectricalMonitorView.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { ElectricalLatest, ElectricalSample, MESTEKUnitHealth } from '@/api/electrical'

vi.mock('@/api/electrical', () => ({
  getElectricalLatest: vi.fn(),
  getElectricalHistory: vi.fn(),
  singleShotElectrical: vi.fn()
}))

import { getElectricalHistory, getElectricalLatest } from '@/api/electrical'

const NOW = '2026-11-17T08:00:00Z'
const definitions = [
  ['mestek', 'modbus_active'],
  ['keithley6485', 'passive'],
  ['rigol_dho814', 'scpi'],
  ['tek_mdo3034', 'scpi'],
  ['agilent_33512b', 'scpi'],
  ['agilent_33210a', 'scpi']
] as const

function sample(source: string, channel: string, metric: string, value: number | string, unit = ''): ElectricalSample {
  const result: ElectricalSample = {
    time: NOW,
    source,
    channel,
    metric,
    mapping: source === 'mestek' ? 'request_response' : 'direct',
    quality: 'good',
    seq: 1
  }
  if (typeof value === 'number') {
    result.value = value
    if (unit) result.unit = unit
  } else {
    result.text_value = value
  }
  return result
}

function makeSamples() {
  const samples: ElectricalSample[] = []
  for (let channel = 1; channel <= 13; channel++) {
    const name = `CH${channel}`
    const current = channel === 1 ? 0.101 : channel === 2 ? 0.1 : channel === 3 ? 0 : 0.02
    samples.push(
      sample('mestek', name, 'output_enabled', 1),
      sample('mestek', name, 'I_A', current, 'A'),
      sample('mestek', name, 'U_V', 10 + channel, 'V'),
      sample('mestek', name, 'setU_V', 10 + channel, 'V'),
      sample('mestek', name, 'setI_A', 3, 'A')
    )
  }
  samples.push(sample('keithley6485', 'CH1', 'current_A', 1.2e-9, 'A'))
  for (let channel = 1; channel <= 4; channel++) {
    samples.push(
      sample('rigol_dho814', `CH${channel}`, 'vpp_V', channel + 0.25, 'V'),
      sample('tek_mdo3034', `CH${channel}`, 'measurement_value', channel + 0.75, 'V'),
      sample('tek_mdo3034', `CH${channel}`, 'type', 'PK2PK')
    )
  }
  for (const [source, count] of [['agilent_33512b', 2], ['agilent_33210a', 1]] as const) {
    for (let channel = 1; channel <= count; channel++) {
      samples.push(
        sample(source, `CH${channel}`, 'output_enabled', 1),
        sample(source, `CH${channel}`, 'waveform', 'SINE'),
        sample(source, `CH${channel}`, 'frequency_Hz', 1000 * channel, 'Hz'),
        sample(source, `CH${channel}`, 'amplitude_V', 2, 'V'),
        sample(source, `CH${channel}`, 'offset_V', 0, 'V')
      )
    }
  }
  samples.push(sample('mestek', 'UNMAPPED', 'frame', '01 02 03'))
  return samples
}

function mestekUnits(connected: [boolean, boolean] = [true, true]): MESTEKUnitHealth[] {
  return [
    { unit: 20, mapped: connected[0] ? 3 : 0, total: 3, connected: connected[0], status: connected[0] ? 'ok' : 'degraded' },
    { unit: 21, mapped: connected[1] ? 10 : 0, total: 10, connected: connected[1], status: connected[1] ? 'ok' : 'degraded' }
  ]
}

function makeLatest(): ElectricalLatest {
  return {
    continuous_enabled: true,
    updated_at: NOW,
    write_backlog: 0,
    sources: definitions.map(([source, mode]) => ({
      source,
      mode,
      status: 'ok',
      connected: true,
      last_sample_at: NOW,
      sample_count: 1,
      error_count: 0,
      write_errors: 0,
      max_gap_ms: 0,
      crc_errors: 0,
      partial_bytes: 0,
      pairing_errors: 0,
      request_frames: 0,
      response_frames: 0,
      mapped_frames: 0,
      ...(source === 'mestek' ? { units: mestekUnits() } : {})
    })),
    samples: makeSamples()
  }
}

async function mountView() {
  const wrapper = mount(ElectricalMonitorView, { global: { plugins: [createTestI18n()] } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(getElectricalLatest).mockReset().mockResolvedValue(makeLatest())
  vi.mocked(getElectricalHistory).mockReset().mockResolvedValue({ points: [] })
})

describe('ElectricalMonitorView 四卡片与重点读数', () => {
  it('显示 MESTEK 13×5、三路信号源、六个示波器读数，且只有皮安计有趋势', async () => {
    const wrapper = await mountView()
    expect(wrapper.findAll('.source-card')).toHaveLength(4)
    expect(wrapper.findAll('.connection-dot')).toHaveLength(7)
    for (const source of ['keithley6485', 'rigol_dho814', 'tek_mdo3034', 'agilent_33512b', 'agilent_33210a']) {
      const dot = wrapper.find(`.connection-dot[data-connection-source="${source}"]`)
      expect(dot.exists()).toBe(true)
      expect(dot.classes()).toContain('online')
      expect(dot.attributes('aria-label')).toBe('在线')
    }
    for (const [source, label] of [['mestek-0x14', '单元 0x14（CH1–CH3）'], ['mestek-0x15', '单元 0x15（CH4–CH13）']] as const) {
      const dot = wrapper.find(`.connection-dot[data-connection-source="${source}"]`)
      expect(dot.exists()).toBe(true)
      expect(dot.classes()).toContain('online')
      expect(dot.attributes('aria-label')).toBe(`${label} · 在线`)
    }
    expect(wrapper.findAll('.mestek-card .mestek-row')).toHaveLength(13)
    expect(wrapper.findAll('.mestek-card .mestek-row td')).toHaveLength(65)
    expect(wrapper.find('.mestek-card [data-channel="CH1"] [data-metric="setI_A"]').text()).toBe('3')
    expect(wrapper.findAll('.generator-card .generator-row')).toHaveLength(3)
    expect(wrapper.findAll('.generator-card [data-metric="amplitude_V"]')).toHaveLength(3)
    expect(wrapper.findAll('.generator-card [data-metric="frequency_Hz"]')).toHaveLength(3)
    expect(wrapper.findAll('.scopes-card .scope-reading')).toHaveLength(6)
    expect(wrapper.findAll('.scopes-card [data-source="rigol_dho814"]')).toHaveLength(4)
    expect(wrapper.findAll('.scopes-card [data-source="tek_mdo3034"]')).toHaveLength(2)
    expect(wrapper.find('.primary-grid svg').exists()).toBe(false)
    expect(wrapper.find('.keithley6485-card .metric-panel').exists()).toBe(true)
    expect(wrapper.find('.mestek-card').text()).toContain('13/13 通道有读数')
    expect(wrapper.find('.mestek-card .source-title').text()).toContain('主动 FC03 只读')
    expect(wrapper.find('.mestek-card').text()).toContain('待现场标定')
    expect(wrapper.find('.mestek-card').text()).not.toContain('01 02 03')
    expect(getElectricalHistory).toHaveBeenCalledTimes(1)
    expect(getElectricalHistory).toHaveBeenCalledWith({
      source: 'keithley6485', channel: 'CH1', metric: 'current_A', from: '-30m', limit: 1000
    })
    wrapper.unmount()
  })

  it('设备断开时指示灯显示红色离线状态', async () => {
    const latest = makeLatest()
    latest.sources = latest.sources.map((item) =>
      item.source === 'agilent_33512b' ? { ...item, connected: false, status: 'degraded' } : item
    )
    vi.mocked(getElectricalLatest).mockResolvedValue(latest)
    const wrapper = await mountView()
    const offline = wrapper.find('.connection-dot[data-connection-source="agilent_33512b"]')
    expect(offline.classes()).not.toContain('online')
    expect(offline.attributes('aria-label')).toBe('离线')
    expect(wrapper.find('.connection-dot[data-connection-source="agilent_33210a"]').classes()).toContain('online')
    wrapper.unmount()
  })

  it('MESTEK 两个单元指示灯分别跟随 units 的 connected 状态', async () => {
    const latest = makeLatest()
    latest.sources = latest.sources.map((item) =>
      item.source === 'mestek' ? { ...item, units: mestekUnits([true, false]) } : item
    )
    vi.mocked(getElectricalLatest).mockResolvedValue(latest)
    const wrapper = await mountView()
    const unit14 = wrapper.find('.connection-dot[data-connection-source="mestek-0x14"]')
    const unit15 = wrapper.find('.connection-dot[data-connection-source="mestek-0x15"]')
    expect(unit14.classes()).toContain('online')
    expect(unit14.attributes('aria-label')).toBe('单元 0x14（CH1–CH3） · 在线')
    expect(unit15.classes()).not.toContain('online')
    expect(unit15.attributes('aria-label')).toBe('单元 0x15（CH4–CH13） · 离线')
    wrapper.unmount()
  })

  it('打火灯严格按阈值显示红色、绿色和无电流，缺数据为未知', async () => {
    const latest = makeLatest()
    latest.samples = latest.samples.filter((item) => !(item.source === 'mestek' && item.channel === 'CH4' && item.metric === 'I_A'))
    vi.mocked(getElectricalLatest).mockResolvedValue(latest)
    const wrapper = await mountView()
    const channel = (number: number) => wrapper.find(`.mestek-card [data-channel="CH${number}"] .arc-status`)
    expect(channel(1).attributes('data-state')).toBe('danger')
    expect(channel(1).text()).toBe('打火')
    expect(channel(2).attributes('data-state')).toBe('safe')
    expect(channel(2).text()).toBe('正常')
    expect(channel(3).attributes('data-state')).toBe('safe')
    expect(channel(3).text()).toBe('无电流')
    expect(channel(4).attributes('data-state')).toBe('unknown')
    expect(channel(4).text()).toBe('未知')
    wrapper.unmount()
  })

  it('只将皮安计历史样本生成趋势，未映射数据不会进入读数或图', async () => {
    const latest = makeLatest()
    latest.samples = []
    vi.mocked(getElectricalLatest).mockResolvedValue(latest)
    vi.mocked(getElectricalHistory).mockResolvedValue({
      points: [
        { ...sample('keithley6485', 'CH1', 'current_A', 1.2e-9, 'A'), time: '2026-11-17T07:59:00Z' },
        { ...sample('mestek', 'UNMAPPED', 'I_A', 9, 'A'), time: '2026-11-17T07:59:00Z' }
      ]
    })
    const wrapper = await mountView()
    expect(wrapper.find('.keithley6485-card svg').exists()).toBe(true)
    expect(wrapper.find('.primary-grid svg').exists()).toBe(false)
    expect(wrapper.find('.mestek-card').text()).not.toContain('01 02 03')
    wrapper.unmount()
  })
})
