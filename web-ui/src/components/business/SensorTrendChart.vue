<template>
  <div class="trend-wrap">
    <div class="chart-toolbar">
      <span class="muted zoom-hint">{{ t('sensors.chart.zoomHint') }}</span>
      <div class="y-controls">
        <el-input-number v-model="yMinInput" size="small" :controls="false" :placeholder="t('sensors.chart.yMin')" class="y-input" />
        <el-input-number v-model="yMaxInput" size="small" :controls="false" :placeholder="t('sensors.chart.yMax')" class="y-input" />
        <el-button size="small" @click="resetY">{{ t('sensors.chart.yAuto') }}</el-button>
      </div>
      <div class="tag-filter">
        <el-check-tag v-for="g in groups" :key="g.name" :checked="visibleTags.get(g.name) ?? false" @change="(checked: boolean) => setTagVisible(g.name, checked)">
          {{ g.name }}
        </el-check-tag>
        <el-link type="primary" class="show-all" @click="showAll">{{ t('sensors.chart.showAll') }}</el-link>
      </div>
      <el-button class="reset-btn" size="small" circle :icon="RefreshLeft" :title="t('sensors.chart.reset')" @click="resetView" />
    </div>
    <div class="chart-box">
      <canvas ref="chartCanvas"></canvas>
    </div>
  </div>
</template>

<script lang="ts">
/* ---------- 窗口数学纯函数（§4.2 + §4.5 可测性安排：不触碰 chart 实例，可独立导入单测） ---------- */

export type ChartPoint = { time: number; value: number }
export type ChartGroup = { name: string; color: string; points: ChartPoint[] }

/** 缩放窗口最小宽度：60s（§4.2.2 clamp 定稿） */
export const MIN_WINDOW_MS = 60_000

/** 以 anchor 为锚点缩放窗口（锚点缩放公式 §4.2.2），窗口时长 clamp 到 [60s, 数据全长] */
export function wheelWindow(
  min: number,
  max: number,
  anchor: number,
  factor: number,
  dataSpan = Number.POSITIVE_INFINITY
): { min: number; max: number } {
  const newMin = anchor - (anchor - min) * factor
  const newMax = anchor + (max - anchor) * factor
  let span = Math.min(Math.max(newMax - newMin, MIN_WINDOW_MS), dataSpan)
  if (span === newMax - newMin) return { min: newMin, max: newMax }
  return { min: anchor - span / 2, max: anchor + span / 2 }
}

/** 位移换算成时间偏移并平移窗口，平移范围 clamp 到数据两端 [dataMin, dataMax]（§4.2.2） */
export function panWindow(
  min: number,
  max: number,
  dxPx: number,
  chartW: number,
  windowMs: number,
  dataMin = Number.NEGATIVE_INFINITY,
  dataMax = Number.POSITIVE_INFINITY
): { min: number; max: number } {
  if (chartW <= 0 || windowMs <= 0) return { min, max }
  const dt = (dxPx / chartW) * windowMs
  let newMin = min - dt
  let newMax = max - dt
  const span = max - min
  if (Number.isFinite(dataMin) && Number.isFinite(dataMax) && span >= dataMax - dataMin) {
    newMin = (dataMin + dataMax - span) / 2
    newMax = newMin + span
  } else {
    if (newMin < dataMin) {
      newMin = dataMin
      newMax = dataMin + span
    }
    if (newMax > dataMax) {
      newMax = dataMax
      newMin = dataMax - span
    }
  }
  return { min: newMin, max: newMax }
}

/** X 窗口切片（含两端），datasets/tooltip/统计口径同一切片（§4.2.3 不变量） */
export function sliceWindow(points: ChartPoint[], min: number, max: number): ChartPoint[] {
  return points.filter((p) => p.time >= min && p.time <= max)
}

/** Y 轴 fit：窗口内可见点 min/max + 5% 边距；空数据返回 null（保持原值） */
export function fitY(points: ChartPoint[]): { min: number; max: number } | null {
  if (!points.length) return null
  let min = Number.POSITIVE_INFINITY
  let max = Number.NEGATIVE_INFINITY
  for (const p of points) {
    if (p.value < min) min = p.value
    if (p.value > max) max = p.value
  }
  let span = max - min
  if (span === 0) span = Math.abs(min) || 1
  const pad = span * 0.05
  return { min: min - pad, max: max + pad }
}
</script>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart,
  LineController,
  LineElement,
  PointElement,
  LinearScale,
  Legend,
  Tooltip,
  type ChartEvent,
  type LegendItem
} from 'chart.js'
import { RefreshLeft } from '@element-plus/icons-vue'
import { useMobile } from '@/composables/useMobile'

Chart.register(LineController, LineElement, PointElement, LinearScale, Legend, Tooltip)

const props = withDefaults(
  defineProps<{
    groups: ChartGroup[]
    windowKey: string
    windowSizeMs: number
  }>(),
  { groups: () => [] }
)

const emit = defineEmits<{ 'zoom-change': [payload: { zoomed: boolean }] }>()

const { t, locale } = useI18n()
const isMobile = useMobile()

// 时间显示跟随界面语言（§3.9 P1-5，DashboardView dateLocale 模式）
const dateLocale = computed(() => (locale.value === 'zh' ? 'zh-CN' : 'en-US'))

/* ---------- 内部状态（§4.5：不外露） ---------- */

const chartCanvas = ref<HTMLCanvasElement>()
let chart: Chart<'line'> | undefined

/** X 窗口：null = 跟随模式（滑动窗口），非 null = 缩放模式（§4.2.3） */
const xWindow = ref<{ min: number; max: number } | null>(null)
/** 曲线显隐单一状态源（§4.3.3）：tag → 是否可见；与图例/筛选条双向同步；必须整体替换以保持响应式 */
const visibleTags = ref<Map<string, boolean>>(new Map())
/** Y 手动范围：null = auto；任一输入非空即进入手动模式（§4.2.1） */
const yManual = ref<{ min?: number; max?: number } | null>(null)
const yMinInput = ref<number | undefined>(undefined)
const yMaxInput = ref<number | undefined>(undefined)

let applyingY = false
/** 跟随模式窗口锚点只前进不回退（§4.2.3 lastT 回退定稿） */
let lastTRef = Number.NEGATIVE_INFINITY
let drag: { startClientX: number; startWindow: { min: number; max: number } } | null = null

/* ---------- 窗口与数据 ---------- */

function latestTime(): number {
  let last = Number.NEGATIVE_INFINITY
  for (const g of props.groups) {
    const points = g.points
    if (points.length && points[points.length - 1].time > last) last = points[points.length - 1].time
  }
  return last
}

function dataBounds(): { min: number; max: number } {
  let min = Number.POSITIVE_INFINITY
  let max = Number.NEGATIVE_INFINITY
  for (const g of props.groups) {
    for (const p of g.points) {
      if (p.time < min) min = p.time
      if (p.time > max) max = p.time
    }
  }
  if (!Number.isFinite(min)) return { min: 0, max: 1 }
  return { min, max }
}

function currentWindow(): { min: number; max: number } {
  if (xWindow.value) return xWindow.value
  const last = latestTime()
  if (Number.isFinite(last) && last > lastTRef) lastTRef = last
  if (!Number.isFinite(lastTRef)) return { min: 0, max: 1 }
  return { min: lastTRef - props.windowSizeMs, max: lastTRef }
}

function emitZoom(zoomed: boolean) {
  emit('zoom-change', { zoomed })
}

/* ---------- 图表渲染 ---------- */

function fmtValue(v: number) {
  if (v === 0) return '0'
  const abs = Math.abs(v)
  if (abs >= 10000 || abs < 0.01) return v.toExponential(3)
  return String(Number(v.toPrecision(4)))
}

// 1h/6h → HH:mm，24h/7d → MM-DD HH:mm（§3.1，参考 InstrumentMeasureView linear scale 先例）
function formatXTick(value: number | string) {
  const t = Number(value)
  if (!Number.isFinite(t)) return ''
  const style: Intl.DateTimeFormatOptions =
    props.windowSizeMs <= 6 * 3600e3
      ? { hour: '2-digit', minute: '2-digit' }
      : { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }
  return new Date(t).toLocaleString(dateLocale.value, { hour12: false, ...style })
}

function createChart() {
  if (!chartCanvas.value) return
  chart = new Chart(chartCanvas.value, {
    type: 'line',
    data: { datasets: [] },
    options: {
      animation: false,
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      plugins: {
        legend: {
          position: 'bottom',
          labels: { usePointStyle: true, boxWidth: 8, boxHeight: 8 },
          onClick: legendClick
        },
        tooltip: {
          callbacks: {
            title: (items) => {
              const first = items[0]
              if (!first) return ''
              return new Date(Number(first.parsed.x)).toLocaleString(dateLocale.value, { hour12: false })
            },
            label: (item) => `${item.dataset.label || ''}: ${fmtValue(Number(item.parsed.y))}`
          }
        }
      },
      scales: {
        x: { type: 'linear', ticks: { maxTicksLimit: 11, callback: (value) => formatXTick(value) } },
        y: { type: 'linear' }
      }
    }
  })
}

/** 单一渲染入口：重建 datasets（窗口切片）+ X 窗口 + Y 范围（§4.2.3 不变量） */
function applyView() {
  if (!chart) return
  const win = currentWindow()
  chart.data.datasets = props.groups.map((g) => ({
    label: g.name,
    data: sliceWindow(g.points, win.min, win.max).map((p) => ({ x: p.time, y: p.value })),
    borderColor: g.color,
    pointRadius: 0,
    borderWidth: 2,
    hidden: visibleTags.value.get(g.name) === false
  }))
  const xScale = chart.options.scales!.x as unknown as { min: number; max: number }
  xScale.min = win.min
  xScale.max = win.max
  applyY(win.min, win.max)
  chart.update('none')
}

/** Y 自动重算触发全集（§4.2.1 定稿）：auto 模式仅由此单一入口收口；手动模式跳过 */
function applyY(winMin: number, winMax: number) {
  if (!chart) return
  const yScale = chart.options.scales!.y as unknown as { min?: number; max?: number }
  if (yManual.value) {
    yScale.min = yManual.value.min
    yScale.max = yManual.value.max
    return
  }
  const points = props.groups
    .filter((g) => visibleTags.value.get(g.name) !== false)
    .flatMap((g) => sliceWindow(g.points, winMin, winMax))
  const fit = fitY(points)
  if (fit) {
    yScale.min = fit.min
    yScale.max = fit.max
  }
}

/* ---------- 曲线筛选（§4.3） ---------- */

function rebuildVisibleTags() {
  if (!chart) return
  const map = new Map<string, boolean>()
  chart.data.datasets.forEach((ds, index) => {
    map.set(String(ds.label ?? index), !ds.hidden)
  })
  visibleTags.value = map
}

function setTagVisible(name: string, visible: boolean) {
  const next = new Map(visibleTags.value)
  next.set(name, visible)
  visibleTags.value = next
  applyView()
}

function showAll() {
  const next = new Map(visibleTags.value)
  props.groups.forEach((g) => next.set(g.name, true))
  visibleTags.value = next
  applyView()
}

// 图例：单击 toggle、Shift/Ctrl+点击只看该条（已隐藏项 = 全部恢复）（§4.3.1）
function legendClick(e: ChartEvent, legendItem: LegendItem) {
  if (!chart) return
  const index = legendItem.datasetIndex
  if (index === undefined || index < 0 || index >= chart.data.datasets.length) return
  const native = e.native as MouseEvent | null
  if (native && (native.shiftKey || native.ctrlKey)) {
    const target = chart.data.datasets[index]
    if (target.hidden) {
      chart.data.datasets.forEach((ds) => {
        ds.hidden = false
      })
    } else {
      chart.data.datasets.forEach((ds, i) => {
        ds.hidden = i !== index
      })
    }
  } else {
    chart.data.datasets[index].hidden = !chart.data.datasets[index].hidden
  }
  rebuildVisibleTags()
  applyView()
}

/* ---------- 手势：Ctrl+滚轮缩放 / 拖拽平移 / 双击复位（§4.2.2） ---------- */

function onWheel(e: WheelEvent) {
  if (!chart || isMobile.value || !e.ctrlKey) return
  e.preventDefault()
  const bounds = dataBounds()
  const span = bounds.max - bounds.min
  if (span <= 0) return
  let win = xWindow.value
  if (!win) {
    win = currentWindow()
    xWindow.value = win
    emitZoom(true)
  }
  const xScale = chart.scales.x
  const pixelAnchor = xScale.getValueForPixel(e.offsetX)
  const anchor = pixelAnchor !== undefined && Number.isFinite(pixelAnchor) ? pixelAnchor : (win.min + win.max) / 2
  const clampedAnchor = Math.min(Math.max(anchor, bounds.min), bounds.max)
  const factor = e.deltaY > 0 ? 1.1 : 1 / 1.1
  xWindow.value = wheelWindow(win.min, win.max, clampedAnchor, factor, span)
  applyView()
}

function onPointerDown(e: PointerEvent) {
  if (!chart || isMobile.value || e.button !== 0) return
  let win = xWindow.value
  if (!win) {
    win = currentWindow()
    xWindow.value = win
    emitZoom(true)
  }
  drag = { startClientX: e.clientX, startWindow: { ...win } }
  chartCanvas.value?.classList.add('dragging')
  chartCanvas.value?.setPointerCapture(e.pointerId)
}

function onPointerMove(e: PointerEvent) {
  if (!drag || !chart) return
  const chartW = chart.chartArea.width
  if (chartW <= 0) return
  const windowMs = drag.startWindow.max - drag.startWindow.min
  const bounds = dataBounds()
  xWindow.value = panWindow(drag.startWindow.min, drag.startWindow.max, e.clientX - drag.startClientX, chartW, windowMs, bounds.min, bounds.max)
  applyView()
}

function onPointerUp() {
  if (!drag) return
  drag = null
  chartCanvas.value?.classList.remove('dragging')
}

function onDblClick(e: MouseEvent) {
  if (!chart) return
  if (xWindow.value === null && yManual.value === null) return
  const legend = chart.legend as unknown as { left?: number; top?: number; width?: number; height?: number } | undefined
  if (
    legend &&
    legend.left !== undefined &&
    legend.top !== undefined &&
    legend.width !== undefined &&
    legend.height !== undefined &&
    e.offsetX >= legend.left &&
    e.offsetX <= legend.left + legend.width &&
    e.offsetY >= legend.top &&
    e.offsetY <= legend.top + legend.height
  ) {
    return
  }
  resetView()
}

/* ---------- 复位与 Y 手动输入 ---------- */

function resetY() {
  yManual.value = null
  applyingY = true
  yMinInput.value = undefined
  yMaxInput.value = undefined
  applyingY = false
  applyView()
}

// ↺：复位 X + Y（§4.2.2：双击的鼠标友好等价物，一并复位）
function resetView() {
  xWindow.value = null
  yManual.value = null
  applyingY = true
  yMinInput.value = undefined
  yMaxInput.value = undefined
  applyingY = false
  emitZoom(false)
  applyView()
}

function normalizeNumber(v: number | null | undefined): number | undefined {
  return typeof v === 'number' && Number.isFinite(v) ? v : undefined
}

// 任一输入非空即进入手动模式；非法输入（NaN / min≥max）忽略并回退当前值（§4.2.1）
watch([yMinInput, yMaxInput], () => {
  if (applyingY) return
  const mn = normalizeNumber(yMinInput.value)
  const mx = normalizeNumber(yMaxInput.value)
  if (mn === undefined && mx === undefined) {
    yManual.value = null
    applyView()
    return
  }
  if (mn !== undefined && mx !== undefined && mx <= mn) {
    applyingY = true
    yMinInput.value = yManual.value?.min
    yMaxInput.value = yManual.value?.max
    applyingY = false
    return
  }
  yManual.value = { min: mn, max: mx }
  applyView()
})

/* ---------- props 联动 ---------- */

// groups 变化：差集清理（消失的 tag 移除、新增默认可见、存续保持用户上次选择）（§4.3.3）
watch(
  () => props.groups,
  (groups) => {
    const names = new Set(groups.map((g) => g.name))
    const next = new Map(visibleTags.value)
    for (const name of next.keys()) {
      if (!names.has(name)) next.delete(name)
    }
    for (const g of groups) {
      if (!next.has(g.name)) next.set(g.name, true)
    }
    visibleTags.value = next
    applyView()
  },
  { immediate: true, deep: true }
)

// windowKey 变化（切 measurement / range）：视图上下文变化，复位为跟随模式（§4.2.2/§4.2.3）
watch(
  () => props.windowKey,
  () => {
    xWindow.value = null
    lastTRef = Number.NEGATIVE_INFINITY
    emitZoom(false)
    applyView()
  }
)

/* ---------- 生命周期（§4.2.2 手势生命周期定稿） ---------- */

onMounted(() => {
  createChart()
  applyView()
  const canvas = chartCanvas.value
  if (!canvas) return
  canvas.addEventListener('wheel', onWheel, { passive: false })
  canvas.addEventListener('pointerdown', onPointerDown)
  canvas.addEventListener('pointermove', onPointerMove)
  canvas.addEventListener('pointerup', onPointerUp)
  canvas.addEventListener('pointercancel', onPointerUp)
  canvas.addEventListener('dblclick', onDblClick)
})

onBeforeUnmount(() => {
  const canvas = chartCanvas.value
  if (canvas) {
    canvas.removeEventListener('wheel', onWheel)
    canvas.removeEventListener('pointerdown', onPointerDown)
    canvas.removeEventListener('pointermove', onPointerMove)
    canvas.removeEventListener('pointerup', onPointerUp)
    canvas.removeEventListener('pointercancel', onPointerUp)
    canvas.removeEventListener('dblclick', onDblClick)
  }
  chart?.destroy()
  chart = undefined
})
</script>

<style scoped>
.trend-wrap {
  display: grid;
  gap: 10px;
}

.chart-toolbar {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.zoom-hint {
  font-size: 12px;
}

.y-controls {
  align-items: center;
  display: flex;
  gap: 6px;
}

.y-input {
  width: 90px;
}

.tag-filter {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.show-all {
  margin-left: 4px;
}

.reset-btn {
  margin-left: auto;
}

.chart-box {
  height: 320px;
  position: relative;
}

.chart-box canvas {
  cursor: grab;
  user-select: none;
}

.chart-box canvas.dragging {
  cursor: grabbing;
}

@media (max-width: 768px) {
  .chart-box {
    height: 240px;
  }

  .chart-box canvas {
    cursor: default;
  }
}
</style>
