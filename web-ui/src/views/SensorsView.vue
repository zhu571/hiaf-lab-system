<template>
  <div class="page">
    <div class="toolbar">
      <h2>传感器数据</h2>
      <div class="toolbar-right">
        <span class="muted">自动刷新</span>
        <el-switch v-model="autoRefresh" />
        <el-button :icon="Refresh" circle title="刷新" @click="loadAll" />
      </div>
    </div>

    <!-- 最新读数 -->
    <section class="panel">
      <div class="panel-head">
        <h3 class="panel-title">最新读数</h3>
        <el-select
          v-model="selectedMeasurements"
          multiple
          collapse-tags
          collapse-tags-tooltip
          placeholder="全部测量项"
          class="measure-select"
          @change="loadLatest"
        >
          <el-option v-for="m in MEASUREMENTS" :key="m.value" :label="m.label" :value="m.value" />
        </el-select>
      </div>
      <el-alert v-if="latestError" :title="latestError" type="error" show-icon :closable="false">
        <el-button size="small" @click="loadLatest">重试</el-button>
      </el-alert>
      <template v-else>
        <div v-loading="latestLoading" class="reading-grid">
          <div v-for="point in latestPoints" :key="point.tag || point.time" class="reading-card">
            <span class="reading-tag">{{ point.tag || '—' }}</span>
            <strong class="reading-value">{{ fmtValue(point.value) }}</strong>
            <span class="muted reading-time">{{ formatTime(point.time) }}</span>
            <div v-if="point.meta && Object.keys(point.meta).length" class="reading-meta">
              <el-tag v-for="(v, k) in point.meta" :key="k" size="small" effect="plain">{{ k }}: {{ v }}</el-tag>
            </div>
          </div>
          <el-empty v-if="!latestLoading && !latestPoints.length" description="暂无读数" class="grid-empty" />
        </div>
      </template>
    </section>

    <!-- 历史趋势 -->
    <section class="panel chart-panel">
      <div class="panel-head">
        <h3 class="panel-title">历史趋势 <span class="muted hint">各序列独立归一化</span></h3>
        <div class="chart-controls">
          <el-select v-model="historyMeasurement" class="chart-measure" @change="loadHistory">
            <el-option v-for="m in MEASUREMENTS" :key="m.value" :label="m.label" :value="m.value" />
          </el-select>
          <el-select v-model="historyRange" class="chart-range" @change="loadHistory">
            <el-option v-for="r in RANGES" :key="r.from" :label="r.label" :value="r.from" />
          </el-select>
        </div>
      </div>
      <el-alert v-if="historyError" :title="historyError" type="error" show-icon :closable="false">
        <el-button size="small" @click="loadHistory">重试</el-button>
      </el-alert>
      <div v-else v-loading="historyLoading">
        <template v-if="chartGroups.length">
          <svg class="trend-chart" :viewBox="`0 0 ${CHART_W} ${CHART_H}`" preserveAspectRatio="xMidYMid meet">
            <line class="axis" :x1="PAD_X" :y1="CHART_H - PAD_Y" :x2="CHART_W - PAD_X" :y2="CHART_H - PAD_Y" />
            <line class="axis" :x1="PAD_X" :y1="PAD_Y" :x2="PAD_X" :y2="CHART_H - PAD_Y" />
            <g v-for="group in chartGroups" :key="group.name">
              <polyline
                v-if="group.points.length >= 2"
                :points="polyline(group)"
                :stroke="group.color"
                fill="none"
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
              />
              <circle v-for="(c, i) in chartCoords(group)" :key="i" :cx="c.x" :cy="c.y" :fill="group.color" r="2.5" />
            </g>
          </svg>
          <div class="legend">
            <span v-for="group in chartGroups" :key="group.name" class="legend-item">
              <i class="legend-dot" :style="{ background: group.color }" />
              {{ group.name }}
              <span class="muted">{{ group.points.length ? fmtValue(group.points[group.points.length - 1].value) : '—' }}</span>
            </span>
          </div>
        </template>
        <el-empty v-else-if="!historyLoading" description="所选时间范围内暂无数据" />
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { getHistory, getLatest, type SensorPoint } from '../api/sensors'
import { showApiError } from '../composables/useNotify'

// 与后端 INFLUXDB_MEASUREMENTS 默认值一致（go-server/sensors/service.go）
const MEASUREMENTS = [
  { value: 'pressure', label: '压力 pressure' },
  { value: 'vacuum', label: '真空 vacuum' },
  { value: 'control', label: '控制 control' },
  { value: 'temperature', label: '温度 temperature' },
  { value: 'pump', label: '泵 pump' }
]

// history 的 from 是 Flux range 表达式，interval 用于 aggregateWindow 降采样
const RANGES = [
  { label: '最近 1 小时', from: '-1h', interval: '30s' },
  { label: '最近 6 小时', from: '-6h', interval: '2m' },
  { label: '最近 24 小时', from: '-24h', interval: '10m' },
  { label: '最近 7 天', from: '-7d', interval: '1h' }
]

const REFRESH_MS = 5000

const selectedMeasurements = ref<string[]>([])
const latestPoints = ref<SensorPoint[]>([])
const latestLoading = ref(false)
const latestError = ref('')

const historyMeasurement = ref('pressure')
const historyRange = ref('-1h')
const historyPoints = ref<SensorPoint[]>([])
const historyLoading = ref(false)
const historyError = ref('')

const autoRefresh = ref(true)
let timer: number | undefined

// 趋势图：viewBox 坐标与调色板
const CHART_W = 640
const CHART_H = 260
const PAD_X = 28
const PAD_Y = 20
const palette = ['var(--brand-500)', 'var(--ok)', 'var(--warn)', 'var(--danger)', '#7a5af8', '#0d5a70', '#c2477e', '#5a8a3c']

type ChartPoint = { time: number; value: number }
type ChartGroup = { name: string; color: string; points: ChartPoint[] }

const chartGroups = computed<ChartGroup[]>(() => {
  const groups = new Map<string, ChartPoint[]>()
  for (const p of historyPoints.value) {
    const time = new Date(p.time).getTime()
    if (Number.isNaN(time)) continue
    const key = p.tag || historyMeasurement.value
    const list = groups.get(key) || []
    list.push({ time, value: p.value })
    groups.set(key, list)
  }
  return Array.from(groups.entries()).map(([name, points], index) => ({
    name,
    color: palette[index % palette.length],
    points: points.sort((a, b) => a.time - b.time)
  }))
})

function chartCoords(group: ChartGroup) {
  const n = group.points.length
  const values = group.points.map((p) => p.value)
  const min = Math.min(...values)
  const max = Math.max(...values)
  return group.points.map((p, i) => ({
    x: n === 1 ? CHART_W / 2 : PAD_X + (i / (n - 1)) * (CHART_W - 2 * PAD_X),
    y: max === min ? CHART_H / 2 : CHART_H - PAD_Y - ((p.value - min) / (max - min)) * (CHART_H - 2 * PAD_Y)
  }))
}

function polyline(group: ChartGroup) {
  return chartCoords(group)
    .map((c) => `${c.x.toFixed(1)},${c.y.toFixed(1)}`)
    .join(' ')
}

onMounted(() => {
  loadAll()
  timer = window.setInterval(() => {
    if (autoRefresh.value) loadAll()
  }, REFRESH_MS)
})

onBeforeUnmount(() => {
  window.clearInterval(timer)
})

watch(autoRefresh, (on) => {
  if (on) loadAll()
})

function loadAll() {
  loadLatest()
  loadHistory()
}

async function loadLatest() {
  latestLoading.value = true
  latestError.value = ''
  try {
    const data = await getLatest(selectedMeasurements.value)
    latestPoints.value = [...data.points].sort((a, b) => a.tag.localeCompare(b.tag))
  } catch (err) {
    latestError.value = err instanceof Error ? err.message : '最新读数加载失败'
    // 自动刷新期间的失败不打 toast，避免刷屏；手动刷新（点击）时提示
    if (!autoRefresh.value) showApiError(err, '最新读数加载失败')
  } finally {
    latestLoading.value = false
  }
}

async function loadHistory() {
  if (!historyMeasurement.value) return
  historyLoading.value = true
  historyError.value = ''
  const range = RANGES.find((r) => r.from === historyRange.value) || RANGES[0]
  try {
    const data = await getHistory(historyMeasurement.value, range.from, '', range.interval)
    historyPoints.value = data.points
  } catch (err) {
    historyError.value = err instanceof Error ? err.message : '历史数据加载失败'
    if (!autoRefresh.value) showApiError(err, '历史数据加载失败')
  } finally {
    historyLoading.value = false
  }
}

function fmtValue(v: number) {
  if (v === 0) return '0'
  const abs = Math.abs(v)
  if (abs >= 10000 || abs < 0.01) return v.toExponential(3)
  return String(Number(v.toPrecision(4)))
}

function formatTime(v?: string) {
  return v ? new Date(v).toLocaleString('zh-CN', { hour12: false }) : '—'
}
</script>

<style scoped>
.toolbar-right {
  align-items: center;
  display: flex;
  gap: 10px;
}

.panel-head {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: space-between;
  margin-bottom: 14px;
}

.panel-title {
  font-size: 15px;
}

.hint {
  font-size: 12px;
  font-weight: 400;
}

.measure-select {
  min-width: 220px;
}

.reading-grid {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
}

.grid-empty {
  grid-column: 1 / -1;
}

.reading-card {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  display: grid;
  gap: 2px;
  padding: 12px 14px;
}

.reading-tag {
  color: var(--text-3);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.reading-value {
  color: var(--text-1);
  font-size: 20px;
  font-variant-numeric: tabular-nums;
}

.reading-time {
  font-size: 11px;
}

.reading-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 4px;
}

.chart-panel {
  display: grid;
}

.chart-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.chart-measure {
  width: 170px;
}

.chart-range {
  width: 140px;
}

.trend-chart {
  display: block;
  height: auto;
  width: 100%;
}

.axis {
  stroke: var(--border-strong);
  stroke-width: 1;
}

.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  margin-top: 10px;
}

.legend-item {
  align-items: center;
  color: var(--text-2);
  display: inline-flex;
  font-size: 12px;
  gap: 6px;
}

.legend-dot {
  border-radius: 50%;
  display: inline-block;
  height: 8px;
  width: 8px;
}

@media (max-width: 768px) {
  .measure-select,
  .chart-measure,
  .chart-range {
    width: 100%;
  }
}
</style>
