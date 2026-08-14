import { describe, it, expect } from 'vitest'
import { wheelWindow, panWindow, sliceWindow, fitY, MIN_WINDOW_MS } from '../SensorTrendChart.vue'
import type { ChartPoint } from '../SensorTrendChart.vue'

// 窗口数学纯函数（SensorTrendChart.vue script 块 :24-98）：不触碰 chart 实例，可独立导入单测。
// 时间单位一律 ms（与源码一致）。
describe('wheelWindow 锚点缩放', () => {
  it('锚点缩放公式：factor=2 放大窗口减半、factor=0.5 缩小窗口加倍，锚点均保持不动', () => {
    const zoomIn = wheelWindow(0, 100000, 50000, 2)
    expect(zoomIn).toEqual({ min: -50000, max: 150000 })

    const zoomOut = wheelWindow(0, 100000, 50000, 0.5)
    // span=50000 < MIN_WINDOW_MS 会被 clamp（见下一用例），这里用大于 MIN 的窗口验证公式本身
    const wide = wheelWindow(0, 200000, 50000, 0.5)
    expect(wide).toEqual({ min: 25000, max: 125000 })
  })

  it('下界 clamp：窗口时长低于 MIN_WINDOW_MS 时 clamp 到 60s，并以 anchor 为中心', () => {
    const result = wheelWindow(0, 100000, 50000, 0.5)
    expect(result.max - result.min).toBe(MIN_WINDOW_MS)
    expect((result.min + result.max) / 2).toBe(50000)
    expect(result).toEqual({ min: 20000, max: 80000 })
  })

  it('上界 clamp：窗口时长超过数据全长 dataSpan 时 clamp 到 dataSpan，并以 anchor 为中心', () => {
    const result = wheelWindow(0, 100000, 50000, 1, 70000)
    expect(result.max - result.min).toBe(70000)
    expect((result.min + result.max) / 2).toBe(50000)
    expect(result).toEqual({ min: 15000, max: 85000 })
  })
})

describe('panWindow 平移', () => {
  it('位移换算：dt = dxPx / chartW * windowMs，右移窗口时间轴正向、左移负向', () => {
    expect(panWindow(0, 60000, 100, 1000, 60000)).toEqual({ min: -6000, max: 54000 })
    expect(panWindow(0, 60000, -100, 1000, 60000)).toEqual({ min: 6000, max: 66000 })
  })

  it('左端 clamp：平移越过 dataMin 时窗口贴住 dataMin，span 保持', () => {
    const result = panWindow(0, 60000, 100, 1000, 60000, 0)
    expect(result).toEqual({ min: 0, max: 60000 })
  })

  it('右端 clamp：平移越过 dataMax 时窗口贴住 dataMax，span 保持', () => {
    const result = panWindow(40000, 100000, -100, 1000, 60000, Number.NEGATIVE_INFINITY, 90000)
    expect(result).toEqual({ min: 30000, max: 90000 })
  })

  it('窗口≥数据全长：两端 clamp 失效，窗口居中于数据区间', () => {
    const result = panWindow(0, 120000, 100, 1000, 120000, 0, 100000)
    expect(result).toEqual({ min: -10000, max: 110000 })
  })

  it('非法输入：chartW<=0 或 windowMs<=0 时原样返回，不做位移', () => {
    expect(panWindow(0, 60000, 100, 0, 60000)).toEqual({ min: 0, max: 60000 })
    expect(panWindow(0, 60000, 100, 1000, 0)).toEqual({ min: 0, max: 60000 })
  })
})

describe('sliceWindow 切片', () => {
  it('包含两端：time 等于 min/max 的点保留，区间外剔除', () => {
    const points: ChartPoint[] = [
      { time: 0, value: 1 },
      { time: 100, value: 2 },
      { time: 200, value: 3 },
      { time: 300, value: 4 }
    ]
    expect(sliceWindow(points, 100, 200)).toEqual([
      { time: 100, value: 2 },
      { time: 200, value: 3 }
    ])
  })
})

describe('fitY 自适应 Y 轴', () => {
  it('空数据返回 null（保持原值）', () => {
    expect(fitY([])).toBeNull()
  })

  it('正常数据：取窗口内 min/max 并加 5% 边距', () => {
    const result = fitY([
      { time: 0, value: 10 },
      { time: 1, value: 1 },
      { time: 2, value: 3 }
    ])
    expect(result).not.toBeNull()
    expect(result!.min).toBeCloseTo(1 - (10 - 1) * 0.05)
    expect(result!.max).toBeCloseTo(10 + (10 - 1) * 0.05)
  })

  it('零跨度兜底：span=0 时以 |min|（min=0 时以 1）为基准加边距', () => {
    const flat = fitY([
      { time: 0, value: 5 },
      { time: 1, value: 5 }
    ])
    expect(flat!.min).toBeCloseTo(5 - 5 * 0.05)
    expect(flat!.max).toBeCloseTo(5 + 5 * 0.05)

    const zero = fitY([
      { time: 0, value: 0 },
      { time: 1, value: 0 }
    ])
    expect(zero!.min).toBeCloseTo(-0.05)
    expect(zero!.max).toBeCloseTo(0.05)
  })
})
