<template>
  <div class="page">
    <div class="toolbar">
      <div class="dash-title">
        <h2>{{ t('sensors.title') }}</h2>
        <p class="dash-sub">{{ t('sensors.subtitle') }}</p>
      </div>
      <div class="toolbar-right">
        <span class="muted">{{ t('sensors.autoRefresh') }}</span>
        <el-switch v-model="autoRefresh" />
        <el-button :icon="Refresh" circle :loading="refreshing" :title="t('sensors.refresh')" @click="manualRefresh" />
      </div>
    </div>

    <!-- 最新读数 -->
    <section class="panel">
      <div class="panel-head">
        <span class="panel-icon"><el-icon><Odometer /></el-icon></span>
        <h3 class="panel-title">{{ t('sensors.latest') }}</h3>
        <span class="panel-meta">{{ t('sensors.countInfo', { n: latestPoints.length }) }}</span>
        <span class="panel-meta">{{ t('sensors.lastUpdated', { time: lastUpdatedText }) }}</span>
        <span class="muted measurement-label">{{ t('sensors.measurementLabel') }}</span>
        <el-select
          v-model="selectedMeasurements"
          multiple
          collapse-tags
          collapse-tags-tooltip
          :placeholder="t('sensors.selectMeasurements')"
          class="measure-select"
          @change="onLatestSelectionChange"
        >
          <el-option v-for="m in MEASUREMENTS" :key="m.value" :label="m.label" :value="m.value" />
        </el-select>
      </div>
      <el-alert v-if="latestError" :title="latestError" type="error" show-icon :closable="false">
        <el-button size="small" @click="loadLatest()">{{ t('sensors.retry') }}</el-button>
      </el-alert>
      <div v-loading="latestLoading" class="reading-grid">
        <div v-for="point in latestPoints" :key="point.tag || point.time" class="reading-card">
          <div class="reading-row">
            <el-tag size="small" effect="plain" class="reading-badge">{{ measurementLabel(point.tag) }}</el-tag>
            <span class="reading-tag">{{ point.tag || '—' }}</span>
            <el-tag v-if="staleLevel(point.time)" size="small" effect="plain" :type="staleLevel(point.time) ?? 'warning'">
              {{ t('sensors.stale') }}
            </el-tag>
          </div>
          <strong class="reading-value">{{ fmtValue(point.value) }}<span v-if="unitOf(point.tag)" class="reading-unit">{{ unitOf(point.tag) }}</span></strong>
          <span class="muted reading-time">{{ formatTime(point.time) }}</span>
        </div>
        <el-empty v-if="!latestLoading && !latestPoints.length" :description="t('sensors.noReadings')" class="grid-empty" />
      </div>
    </section>

    <!-- 历史趋势 -->
    <section class="panel chart-panel">
      <div class="panel-head">
        <span class="panel-icon"><el-icon><TrendCharts /></el-icon></span>
        <h3 class="panel-title">{{ t('sensors.history') }}</h3>
        <span class="panel-meta hint-meta">{{ t('sensors.historyHint') }}</span>
        <div class="chart-controls">
          <el-select v-model="historyMeasurement" class="chart-measure" @change="onMeasurementChange">
            <el-option v-for="m in MEASUREMENTS" :key="m.value" :label="m.label" :value="m.value" />
          </el-select>
          <el-select
            :model-value="rangeZoomed ? undefined : historyRange"
            class="chart-range"
            :placeholder="t('sensors.chart.customRange')"
            @change="onRangeChange"
          >
            <el-option v-for="r in RANGES" :key="r.from" :label="r.label" :value="r.from" />
          </el-select>
        </div>
      </div>
      <el-alert v-if="historyError" :title="historyError" :type="historyErrorType" show-icon :closable="false">
        <el-button v-if="historyErrorType === 'error'" size="small" @click="loadHistory()">{{ t('sensors.retry') }}</el-button>
      </el-alert>
      <div v-loading="historyLoading" class="chart-body" :class="{ syncing: historySyncing && !historyLoading }">
        <template v-if="chartGroups.length">
          <SensorTrendChart :groups="chartGroups" :window-key="windowKey" :window-size-ms="windowSizeMs" @zoom-change="onZoomChange" />
          <div class="legend">
            <span v-for="group in chartGroups" :key="group.name" class="legend-item">
              <i class="legend-dot" :style="{ background: group.color }" />
              {{ group.name }}
              <span class="muted">{{ group.points.length ? fmtValue(group.points[group.points.length - 1].value) : '—' }}</span>
            </span>
          </div>
        </template>
        <el-empty v-else-if="!historyLoading && !historyError" :description="t('sensors.noDataInRange')" />
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Odometer, Refresh, TrendCharts } from '@element-plus/icons-vue'
import { getHistory, getLatest, type SensorPoint } from '../api/sensors'
import { showApiError } from '../composables/useNotify'
import { usePolling } from '../composables/usePolling'
import SensorTrendChart, { type ChartGroup, type ChartPoint } from '@/components/business/SensorTrendChart.vue'

const { t, locale } = useI18n()

// 时间显示跟随界面语言（§3.9 P1-5，DashboardView dateLocale 模式）
const dateLocale = computed(() => (locale.value === 'zh' ? 'zh-CN' : 'en-US'))

// 与后端 INFLUXDB_MEASUREMENTS 默认值一致（go-server/sensors/service.go）
const MEASUREMENTS = computed(() => [
    { value: 'pressure', label: t('sensors.measurement.pressure') },
    { value: 'vacuum', label: t('sensors.measurement.vacuum') },
    { value: 'control', label: t('sensors.measurement.control') },
    { value: 'temperature', label: t('sensors.measurement.temperature') },
    { value: 'pump', label: t('sensors.measurement.pump') }
  ])

// history 的 from 是 Flux range 表达式，interval 用于 aggregateWindow 降采样
const RANGES = computed(() => [
    { label: t('sensors.range.1h'), from: '-1h', interval: '30s' },
    { label: t('sensors.range.6h'), from: '-6h', interval: '2m' },
    { label: t('sensors.range.24h'), from: '-24h', interval: '10m' },
    { label: t('sensors.range.7d'), from: '-7d', interval: '1h' }
  ])

// 显示层静态单位约定：后端暂无 unit 字段，后续后端返回 unit 时替换为动态值（§3.6 P1-2）
const UNIT_MAP: Record<string, string> = {
  pressure: 'Pa',
  vacuum: 'Pa',
  control: '%',
  temperature: 'K',
  pump: '%'
}

const REFRESH_MS = 5000
const HISTORY_REFRESH_MS = 30000

// P0-3：默认显式选中 5 个已知测量项，消除「空选择 = 后端整桶查询」歧义
const selectedMeasurements = ref<string[]>(MEASUREMENTS.value.map((m) => m.value))
const latestPoints = ref<SensorPoint[]>([])
const latestLoading = ref(false)
const latestError = ref('')

const historyMeasurement = ref('pressure')
const historyRange = ref('-1h')
const historyPoints = ref<SensorPoint[]>([])
const historyLoading = ref(false)
const historyError = ref('')
const historyErrorType = ref<'error' | 'warning'>('error')
const historySyncing = ref(false)
// 自定义态哨兵：唯一由组件 zoom-change 事件驱动（§4.2.2 定稿）
const rangeZoomed = ref(false)

const autoRefresh = ref(true)
const refreshing = ref(false)
const lastUpdatedAt = ref<Date | null>(null)
let historySeq = 0

// 轮询（重构方案 §3.5）：latest 5s + history 30s 两个独立轮询，由 autoRefresh 开关统一启停；
// 页面隐藏自动暂停、恢复可见立即刷 latest（history 不立即刷，保持现状语义）
const latestPolling = usePolling(() => loadLatest({ silent: true }), REFRESH_MS)
const historyPolling = usePolling(() => loadHistory({ silent: true }), HISTORY_REFRESH_MS, { resumeImmediate: false })

// 趋势图调色板
const palette = ['var(--brand-500)', 'var(--ok)', 'var(--warn)', 'var(--danger)', '#7a5af8', '#0d5a70', '#c2477e', '#5a8a3c']

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

// windowKey 变化 → SensorTrendChart 复位 xWindow（§4.5）。
// rangeNonce 保证缩放态下重新点击当前已选档也触发复位（§4.2.2「点击任一档」语义）。
const rangeNonce = ref(0)
const windowKey = computed(() => `${historyMeasurement.value}:${historyRange.value}:${rangeNonce.value}`)
const windowSizeMs = computed(() => {
  const map: Record<string, number> = { '-1h': 3600e3, '-6h': 21600e3, '-24h': 86400e3, '-7d': 604800e3 }
  return map[historyRange.value] || 3600e3
})

/* ---------- 刷新机制（§3.2 P0-2） ---------- */

// autoRefresh 开关同时管辖 latest 5s + history 30s 两个轮询（§3.2 定稿）；
// unmount 清理由 usePolling 内部 onBeforeUnmount 完成
watch(
  autoRefresh,
  (on) => {
    if (on) {
      latestPolling.start()
      historyPolling.start()
      loadAll()
    } else {
      latestPolling.stop()
      historyPolling.stop()
    }
  },
  { immediate: true }
)

async function loadAll(opts: { silent?: boolean } = {}) {
  await Promise.all([loadLatest(opts), loadHistory(opts)])
}

// 手动刷新：el-button loading 态天然防重（§3.2 定稿），走静默入口不闪遮罩
async function manualRefresh() {
  refreshing.value = true
  try {
    await loadAll({ silent: true })
  } finally {
    refreshing.value = false
  }
}

async function loadLatest(opts: { silent?: boolean } = {}) {
  if (!opts.silent) latestLoading.value = true
  latestError.value = ''
  try {
    const data = await getLatest(selectedMeasurements.value)
    latestPoints.value = data.points ? [...data.points].sort((a, b) => a.tag.localeCompare(b.tag)) : []
    lastUpdatedAt.value = new Date()
  } catch (err) {
    latestError.value = err instanceof Error ? err.message : t('sensors.latestFailed')
    // 轮询期失败静默降级到面板内警示条，不打 toast 刷屏（§3.2）
    if (!opts.silent) showApiError(err, t('sensors.latestFailed'))
  } finally {
    latestLoading.value = false
  }
}

async function loadHistory(opts: { silent?: boolean } = {}) {
  if (!historyMeasurement.value) return
  const seq = ++historySeq
  if (!opts.silent) historyLoading.value = true
  if (opts.silent) historySyncing.value = true
  historyError.value = ''
  const range = RANGES.value.find((r) => r.from === historyRange.value) || RANGES.value[0]
  try {
    const data = await getHistory(historyMeasurement.value, range.from, '', range.interval)
    if (seq !== historySeq) return
    historyPoints.value = data.points || []
  } catch (err) {
    if (seq !== historySeq) return
    historyError.value = err instanceof Error ? err.message : t('sensors.historyFailed')
    historyErrorType.value = opts.silent ? 'warning' : 'error'
    if (!opts.silent) showApiError(err, t('sensors.historyFailed'))
  } finally {
    if (seq === historySeq) {
      historyLoading.value = false
      historySyncing.value = false
    }
  }
}

function onLatestSelectionChange() {
  loadLatest()
}

function onMeasurementChange() {
  loadHistory()
}

// 快捷档点击：切 range + loadHistory + 复位为跟随模式（§4.2.2）
function onRangeChange(value: string) {
  historyRange.value = value
  rangeNonce.value++
  loadHistory()
}

function onZoomChange(e: { zoomed: boolean }) {
  rangeZoomed.value = e.zoomed
}

/* ---------- 展示辅助 ---------- */

function fmtValue(v: number) {
  if (v === 0) return '0'
  const abs = Math.abs(v)
  if (abs >= 10000 || abs < 0.01) return v.toExponential(3)
  return String(Number(v.toPrecision(4)))
}

function formatTime(v?: string) {
  return v ? new Date(v).toLocaleString(dateLocale.value, { hour12: false }) : '—'
}

const lastUpdatedText = computed(() => (lastUpdatedAt.value ? formatTime(lastUpdatedAt.value.toISOString()) : '—'))

// 卡片归属：已知测量项前缀匹配（大小写不敏感）→ 未知徽标兜底（§3.3 定稿）
function measurementOf(tag: string): string | null {
  const lower = tag.toLowerCase()
  for (const m of MEASUREMENTS.value) {
    if (lower.startsWith(m.value)) return m.value
  }
  return null
}

function measurementLabel(tag: string) {
  const m = measurementOf(tag)
  return m ? t(`sensors.measurement.${m}`) : t('sensors.unknown')
}

function unitOf(tag: string) {
  const m = measurementOf(tag)
  return m && UNIT_MAP[m] ? UNIT_MAP[m] : ''
}

// 新鲜度：>60s warning、>10min danger（§3.4 P0-4）
function ageSeconds(time?: string) {
  if (!time) return Number.POSITIVE_INFINITY
  const t = new Date(time).getTime()
  return Number.isNaN(t) ? Number.POSITIVE_INFINITY : (Date.now() - t) / 1000
}

function staleLevel(time?: string): 'warning' | 'danger' | null {
  const age = ageSeconds(time)
  if (!Number.isFinite(age) || age <= 60) return null
  return age > 600 ? 'danger' : 'warning'
}
</script>

<style scoped>
.toolbar-right {
  align-items: center;
  display: flex;
  gap: 10px;
}

.dash-title h2 {
  font-size: 22px;
}

.dash-sub {
  color: var(--text-3);
  font-size: 13px;
  margin-top: 2px;
}

.panel-head {
  flex-wrap: wrap;
}

.hint-meta {
  font-weight: 400;
}

.measure-select {
  margin-left: auto;
  min-width: 220px;
}

.measurement-label {
  font-size: 12px;
}

.reading-grid {
  display: grid;
  gap: var(--space-3);
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

.reading-row {
  align-items: center;
  display: flex;
  gap: 6px;
  min-width: 0;
}

.reading-badge {
  flex-shrink: 0;
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

.reading-unit {
  color: var(--text-3);
  font-size: 13px;
  font-weight: 400;
  margin-left: 4px;
}

.reading-time {
  font-size: 11px;
}

.chart-panel {
  display: grid;
}

.chart-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-left: auto;
}

.chart-measure {
  width: 170px;
}

.chart-range {
  width: 140px;
}

.chart-body {
  transition: opacity 0.2s;
}

.chart-body.syncing {
  opacity: 0.6;
}

@media (max-width: 768px) {
  .measure-select,
  .chart-measure,
  .chart-range {
    width: 100%;
  }

  .measure-select,
  .chart-controls {
    margin-left: 0;
  }
}

@media (max-width: 480px) {
  .reading-grid {
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  }
}
</style>
