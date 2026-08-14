import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Chart } from 'chart.js'
import { buildChartGroups, chartPalette, refreshDefaults, setupChartDefaults, type ChartGroupRow } from '@/utils/chartTheme'

// chartTheme 单测（测试方案 §8.4 R4-9 ①，美术 S3 自带）：
// setupChartDefaults 注册表收口 / refreshDefaults 快照与计算色实时读取 / chartPalette 解析去重 / buildChartGroups 纯函数。
// jsdom 无法计算 CSS 自定义属性 → getComputedStyle 返回空 → 走 LIGHT 兜底值（确定性快照）；
// 实时读取分支用 mock getComputedStyle 覆盖。

const COLOR_STUB: Record<string, string> = {
  '--text-1': '#111111',
  '--text-2': '#222222',
  '--text-3': '#333333',
  '--border': '#444444',
  '--surface': '#555555',
  '--chart-1': '#a1a1a1',
  '--chart-2': '#a2a2a2',
  '--chart-3': '#a3a3a3',
  '--chart-4': '#a4a4a4',
  '--chart-5': '#a5a5a5',
  '--chart-6': '#a6a6a6',
  '--chart-7': '#a7a7a7',
  '--chart-8': '#a8a8a8',
  '--font-family-base': 'mock-font'
}

let computedSpy: ReturnType<typeof vi.spyOn> | undefined

afterEach(() => {
  computedSpy?.mockRestore()
  computedSpy = undefined
})

describe('setupChartDefaults 注册表收口（唯一点）', () => {
  it('注册 Line/Scatter Controller + Linear/Category Scale + Point/Line Element + Legend/Tooltip 并集', () => {
    setupChartDefaults()
    expect(() => Chart.registry.getController('line')).not.toThrow()
    expect(() => Chart.registry.getController('scatter')).not.toThrow()
    expect(() => Chart.registry.getScale('linear')).not.toThrow()
    expect(() => Chart.registry.getScale('category')).not.toThrow()
    expect(() => Chart.registry.getElement('line')).not.toThrow()
    expect(() => Chart.registry.getElement('point')).not.toThrow()
  })

  it('重复调用幂等（main.ts 单次调用 + 测试多次调用不炸）', () => {
    expect(() => setupChartDefaults()).not.toThrow()
  })
})

describe('refreshDefaults 快照（jsdom 无计算样式 → LIGHT 兜底）', () => {
  beforeEach(() => {
    refreshDefaults()
  })

  it('字体/颜色默认值 = 令牌兜底值', () => {
    expect(Chart.defaults.font.size).toBe(12)
    expect(Chart.defaults.color).toBe('#35485e')
    expect(Chart.defaults.borderColor).toBe('#e3e9f1')
    expect(Chart.defaults.font.family).toContain('PingFang SC')
  })

  it('刻度/网格/图例/tooltip 统一样式', () => {
    expect(Chart.defaults.scales.linear.ticks.color).toBe('#5c6f82')
    expect(Chart.defaults.scales.linear.grid.color).toBe('#e3e9f1')
    expect(Chart.defaults.scales.linear.grid.lineWidth).toBe(1)
    expect(Chart.defaults.scales.category.ticks.color).toBe('#5c6f82')
    expect(Chart.defaults.plugins.legend.position).toBe('bottom')
    expect(Chart.defaults.plugins.legend.labels.usePointStyle).toBe(true)
    expect(Chart.defaults.plugins.tooltip.backgroundColor).toBe('#ffffff')
    expect(Chart.defaults.plugins.tooltip.titleColor).toBe('#12263a')
  })

  it('getComputedStyle 实时取色：mock 计算样式后 defaults 随之更新（主题联动前提）', () => {
    computedSpy = vi.spyOn(window, 'getComputedStyle').mockReturnValue({
      getPropertyValue: (name: string) => COLOR_STUB[name] ?? ''
    } as unknown as CSSStyleDeclaration)
    refreshDefaults()
    expect(Chart.defaults.color).toBe('#222222')
    expect(Chart.defaults.scales.linear.ticks.color).toBe('#333333')
    expect(Chart.defaults.plugins.tooltip.backgroundColor).toBe('#555555')
  })
})

describe('chartPalette 解析与去重', () => {
  it('jsdom 兜底返回 8 个令牌值，与 tokens.css --chart-1..8 现值一致且互不重复', () => {
    const palette = chartPalette()
    expect(palette).toHaveLength(8)
    expect(palette[0]).toBe('#1a86a2')
    expect(palette[1]).toBe('#4d9e6b')
    expect(palette[2]).toBe('#d9932c')
    expect(new Set(palette).size).toBe(8)
  })

  it('mock 计算样式后返回实时解析色（深浅主题自动正确）', () => {
    computedSpy = vi.spyOn(window, 'getComputedStyle').mockReturnValue({
      getPropertyValue: (name: string) => COLOR_STUB[name] ?? ''
    } as unknown as CSSStyleDeclaration)
    const palette = chartPalette()
    expect(palette).toEqual(['#a1a1a1', '#a2a2a2', '#a3a3a3', '#a4a4a4', '#a5a5a5', '#a6a6a6', '#a7a7a7', '#a8a8a8'])
  })
})

describe('buildChartGroups 纯函数（P15 合并双份分组逻辑）', () => {
  const palette = ['c1', 'c2', 'c3']

  it('按 key 分组 + 组内时间升序 + 色循环', () => {
    const rows: ChartGroupRow[] = [
      { key: 'b', time: 30, value: 3 },
      { key: 'a', time: 20, value: 2 },
      { key: 'a', time: 10, value: 1 },
      { key: 'c', time: 40, value: 4 },
      { key: 'd', time: 50, value: 5 }
    ]
    const groups = buildChartGroups(rows, palette)
    expect(groups.map((g) => g.name)).toEqual(['b', 'a', 'c', 'd'])
    expect(groups[0].color).toBe('c1')
    expect(groups[1].color).toBe('c2')
    expect(groups[2].color).toBe('c3')
    expect(groups[3].color).toBe('c1')
    expect(groups[1].points.map((p) => p.time)).toEqual([10, 20])
  })

  it('空输入返回空数组', () => {
    expect(buildChartGroups([], palette)).toEqual([])
  })

  it('不修改输入行数组（纯函数）', () => {
    const rows: ChartGroupRow[] = [{ key: 'a', time: 5, value: 1 }, { key: 'a', time: 1, value: 2 }]
    buildChartGroups(rows, palette)
    expect(rows[0].time).toBe(5)
    expect(rows[1].time).toBe(1)
  })
})
