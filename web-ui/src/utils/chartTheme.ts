// 图表配置单文件（M3 定稿：重构方案 §3.7 = 美术方案 §3.7，两方案同一文件、同一事实源）。
// 吸收 buildChartGroups/chartPalette 职责并收口 Chart.register 为 setupChartDefaults 唯一点，
// 不建 chartPalette.ts（v5 及以前规划作废）。
//
// 职责：
//   setupChartDefaults() —— 唯一 Chart.register 点（Line/Scatter Controller + Point/Line Element +
//                            Linear/Category Scale + Legend/Tooltip，取三处现有注册并集），
//                           并在 main.ts createApp 前调用一次（组件内不再各自注册）；内部顺带 refreshDefaults()
//   refreshDefaults()    —— 经 getComputedStyle 读计算色写入 Chart.defaults（网格/刻度/图例/tooltip/字体），
//                           主题切换与初始化共用此函数（美术 §3.6：切换时先 refreshDefaults 再 destroy+重建实例）
//   chartPalette()       —— 经 getComputedStyle 实时读 --chart-1..8（深浅主题自动正确），
//                           替代 SensorsView/TestDataView 两份硬编码数组（P15）
//   buildChartGroups()   —— 合并 SensorsView/TestDataView 双份图表分组/排序逻辑为单一纯函数（P15）
//
// 兜底策略：jsdom / 初始化早期（CSS 未加载）getComputedStyle 取不到自定义属性时，
// 回退到 LIGHT 令牌值（与 tokens.css 当前值逐字一致）——这同时是单测快照的确定性来源。
// 主题切换后的取色走计算样式（html.dark 已落位），不受兜底影响（美术 S5 验证项）。

import {
  CategoryScale,
  Chart,
  Legend,
  LineController,
  LineElement,
  LinearScale,
  PointElement,
  ScatterController,
  Tooltip
} from 'chart.js'

export type ChartPoint = { time: number; value: number }
export type ChartGroup = { name: string; color: string; points: ChartPoint[] }
/** buildChartGroups 的输入行：key = 分组键（测量项/标签），time/value = 折线坐标 */
export type ChartGroupRow = { key: string; time: number; value: number }

const FONT_FALLBACK =
  '"PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "Noto Sans CJK SC", ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif'

/** LIGHT 令牌兜底值（styles/tokens.css 现值逐字一致，2026-08-15） */
const LIGHT_FALLBACK: Record<string, string> = {
  '--text-1': '#12263a',
  '--text-2': '#35485e',
  '--text-3': '#5c6f82',
  '--border': '#e4e9f1',
  '--surface': '#ffffff',
  // --chart-1..8：独立图表色，不随 brand ramp（brand-500 已换 #168ca9，--chart-1 保持 #1a86a2）
  '--chart-1': '#1a86a2',
  '--chart-2': '#4d9e6b',
  '--chart-3': '#d9932c',
  '--chart-4': '#cd4a45',
  '--chart-5': '#7a5af8',
  '--chart-6': '#0d5a70',
  '--chart-7': '#c2477e',
  '--chart-8': '#5a8a3c',
  '--font-family-base': FONT_FALLBACK
}

function cssVar(name: string): string {
  if (typeof window === 'undefined' || !window.getComputedStyle) {
    return LIGHT_FALLBACK[name] ?? ''
  }
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || (LIGHT_FALLBACK[name] ?? '')
}

/** 图表系列色：--chart-1..8 计算色（JS 侧取色，深浅主题自动正确） */
export function chartPalette(): string[] {
  return [1, 2, 3, 4, 5, 6, 7, 8].map((n) => cssVar(`--chart-${n}`))
}

/** 重读计算色写入 Chart.defaults：网格/刻度/图例/tooltip/字体统一（美术 §3.7 定稿值） */
export function refreshDefaults(): void {
  Chart.defaults.color = cssVar('--text-2')
  Chart.defaults.borderColor = cssVar('--border')
  Chart.defaults.font.family = cssVar('--font-family-base')
  Chart.defaults.font.size = 12

  const grid = {
    color: cssVar('--border'),
    borderColor: cssVar('--border'),
    borderWidth: 1
  }
  // 运行时 defaults.scales.{linear,category} 的 grid/ticks 为惰性合并对象，需先确保存在；
  // GridLineOptions 类型未声明 scale 边框属性（borderColor/borderWidth 为运行时支持项），以窄类型展开
  type GridWithBorder = { color?: string; lineWidth?: number; borderColor?: string; borderWidth?: number }
  function setScaleDefaults(scale: unknown, tickColor: string) {
    const s = scale as { ticks?: { color?: string }; grid?: GridWithBorder }
    s.ticks = s.ticks || {}
    s.grid = s.grid || {}
    s.ticks.color = tickColor
    s.grid.color = grid.color
    s.grid.lineWidth = 1
    s.grid.borderColor = grid.borderColor
    s.grid.borderWidth = grid.borderWidth
  }
  setScaleDefaults(Chart.defaults.scales.linear, cssVar('--text-3'))
  setScaleDefaults(Chart.defaults.scales.category, cssVar('--text-3'))

  // S4 全局曲线化：折线 tension 0.3 / borderWidth 2（开关量阶梯序列由组件侧显式 tension: 0 豁免）
  Chart.defaults.elements.line.tension = 0.3
  Chart.defaults.elements.line.borderWidth = 2

  // 图例 bottom + pointStyle（沿用 SensorTrendChart.vue:222-226 现风格）
  Chart.defaults.plugins.legend.position = 'bottom'
  Chart.defaults.plugins.legend.labels.usePointStyle = true
  Chart.defaults.plugins.legend.labels.boxWidth = 8
  Chart.defaults.plugins.legend.labels.boxHeight = 8

  // tooltip 底 surface、文字 text-1、边框 border（S4：圆角 8 / 内边距 10）
  Chart.defaults.plugins.tooltip.backgroundColor = cssVar('--surface')
  Chart.defaults.plugins.tooltip.titleColor = cssVar('--text-1')
  Chart.defaults.plugins.tooltip.bodyColor = cssVar('--text-1')
  Chart.defaults.plugins.tooltip.borderColor = cssVar('--border')
  Chart.defaults.plugins.tooltip.borderWidth = 1
  Chart.defaults.plugins.tooltip.cornerRadius = 8
  Chart.defaults.plugins.tooltip.padding = 10
}

/**
 * 唯一 Chart.register 收口点（美术 §3.7）：
 * 注册并集 = SensorTrendChart.vue:117 + GasControlView.vue:91 + InstrumentMeasureView.vue:301。
 * main.ts 中 createApp 前调用一次；组件内不再各自 Chart.register。
 */
export function setupChartDefaults(): void {
  Chart.register(LineController, ScatterController, LineElement, PointElement, LinearScale, CategoryScale, Legend, Tooltip)
  refreshDefaults()
}

/** 图表分组/排序纯函数（P15 合并 SensorsView:163-178 与 TestDataView:169-182）：按 key 分组、色循环、时间升序 */
export function buildChartGroups(rows: ChartGroupRow[], palette: string[]): ChartGroup[] {
  const groups = new Map<string, ChartGroupRow[]>()
  for (const row of rows) {
    const list = groups.get(row.key) || []
    list.push(row)
    groups.set(row.key, list)
  }
  return Array.from(groups.entries()).map(([name, points], index) => ({
    name,
    color: palette[index % palette.length],
    points: points.sort((a, b) => a.time - b.time)
  }))
}
