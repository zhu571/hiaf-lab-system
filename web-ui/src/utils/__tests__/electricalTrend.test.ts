import { describe, expect, it } from 'vitest'
import type { ElectricalSample } from '@/api/electrical'
import {
  buildSparklinePaths,
  electricalSampleKey,
  electricalSeriesKey,
  mergeElectricalTrend,
  mestekArcState
} from '../electricalTrend'

function sample(time: string, value: number, channel = 'CH1'): ElectricalSample {
  return {
    time,
    source: 'mestek',
    channel,
    metric: 'I_A',
    mapping: 'request_response',
    value,
    quality: 'good',
    seq: 1
  }
}

describe('最新样本稳定身份', () => {
  it('与数组位置无关，并保留 mapping 和 signature 边界', () => {
    const original = sample('2026-01-01T00:00:00Z', 0.02)
    expect(electricalSampleKey(original)).toBe(electricalSampleKey({ ...original }))
    expect(electricalSampleKey({ ...original, signature: 'abc123' })).not.toBe(electricalSampleKey(original))
    expect(electricalSampleKey({ ...original, mapping: 'unmapped' })).not.toBe(electricalSampleKey(original))
  })
})

describe('MESTEK 打火阈值', () => {
  it('仅严格大于 0.1 A 判红，缺数据不假装安全', () => {
    expect(mestekArcState(0.101)).toBe('danger')
    expect(mestekArcState(0.1)).toBe('safe')
    expect(mestekArcState(0)).toBe('safe')
    expect(mestekArcState(undefined)).toBe('unknown')
    expect(mestekArcState(Number.NaN)).toBe('unknown')
  })
})

describe('电器趋势合并', () => {
  it('按时间 dedupe、保留最近点并隔离未映射样本', () => {
    const key = electricalSeriesKey('mestek', 'CH1', 'I_A')
    let history = mergeElectricalTrend({}, [sample('2026-01-01T00:00:00Z', 0.02)])
    history = mergeElectricalTrend(history, [
      sample('2026-01-01T00:00:00Z', 0.03),
      sample('2026-01-01T00:00:02Z', 0.04),
      sample('2026-01-01T00:00:04Z', 0.2, 'UNMAPPED')
    ])
    expect(history[key]).toEqual([
      { time: Date.parse('2026-01-01T00:00:00Z'), value: 0.03 },
      { time: Date.parse('2026-01-01T00:00:02Z'), value: 0.04 }
    ])
    history = mergeElectricalTrend(history, [sample('2026-01-01T00:00:04Z', 0.05)], 2)
    expect(history[key]).toHaveLength(2)
    expect(history[key][1].value).toBe(0.05)
  })
})

describe('轻量趋势路径', () => {
  it('支持单点、常量和普通时间序列且坐标有限', () => {
    expect(buildSparklinePaths([]).line).toBe('')
    expect(buildSparklinePaths([{ time: 1, value: 2 }]).line).toContain('L')
    const result = buildSparklinePaths([
      { time: 1000, value: 1 },
      { time: 2000, value: 3 },
      { time: 4000, value: 2 }
    ])
    expect(result.line.startsWith('M')).toBe(true)
    expect(result.area.endsWith('Z')).toBe(true)
    expect(Number.isFinite(result.endX)).toBe(true)
    expect(Number.isFinite(result.endY)).toBe(true)
  })
})
