<template>
  <div class="gas-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">GasCell</p>
        <h1>{{ t('gasControl.title') }}</h1>
        <p class="subtitle">{{ t('gasControl.subtitle') }}</p>
      </div>
      <el-tag :type="connected ? 'success' : 'warning'">{{ connected ? t('gasControl.realtime') : t('gasControl.reconnecting') }}</el-tag>
    </header>

    <el-alert v-if="streamError" :title="streamError" type="warning" :closable="false" show-icon />
    <el-alert v-if="tripCode" :title="t('gasControl.a5Trip', { code: tripCode })" type="error" :closable="false" show-icon />

    <section v-loading="loading" class="status-grid">
      <article v-for="card in cards" :key="card.pv" class="status-card">
        <span>{{ t(card.labelKey) }}</span>
        <strong :class="{ invalid: point(card.pv).q !== 'good' }">{{ display(card.pv, card.unit) }}</strong>
        <small>{{ point(card.pv).q === 'good' ? card.pv : t('gasControl.dataInvalid') }}</small>
      </article>
    </section>

    <section class="chart-card">
      <div class="section-title">
        <div>
          <h2>{{ t('gasControl.chartTitle') }}</h2>
          <p>{{ t('gasControl.chartHint') }}</p>
        </div>
      </div>
      <div v-if="error" class="state-panel"><el-result icon="error" :title="t('gasControl.loadFailed')" :sub-title="error" /></div>
      <canvas v-else ref="chartCanvas" :aria-label="t('gasControl.chartAria')"></canvas>
    </section>

    <section v-if="canOperate" class="control-card">
      <div class="section-title">
        <div><h2>{{ t('gasControl.panel') }}</h2><p>{{ t('gasControl.panelHint') }}</p></div>
        <el-tag type="warning">maintainer / admin</el-tag>
      </div>
      <el-form class="control-grid" label-position="top" @submit.prevent>
        <el-form-item label="Setpoint (Pa)"><el-input-number v-model="form.setpoint" :min="0" :max="10000" :controls="false" /></el-form-item>
        <el-form-item label="Kp"><el-input-number v-model="form.kp" :min="0" :max="1" :controls="false" /></el-form-item>
        <el-form-item label="Ki"><el-input-number v-model="form.ki" :min="0" :max="1" :controls="false" /></el-form-item>
        <el-form-item class="control-actions"><el-button type="primary" :loading="writeBusy" @click="applyParams">{{ t('gasControl.applyParams') }}</el-button></el-form-item>
        <div class="param-display">
          <span>Kp = <strong>{{ point('GasCell:Piezo:Kp').v ?? '—' }}</strong></span>
          <span>Ki = <strong>{{ point('GasCell:Piezo:Ki').v ?? '—' }}</strong></span>
        </div>
      </el-form>
      <div class="button-row">
        <el-button v-if="!isRunning" type="success" :loading="writeBusy" @click="start">{{ t('gasControl.start') }}</el-button>
        <el-button v-else type="warning" :loading="writeBusy" @click="stop">{{ t('gasControl.stop') }}</el-button>
        <el-input-number v-model="form.valve" :min="0" :max="100" :controls="false" :placeholder="t('gasControl.manualValve')" />
        <el-button :disabled="isRunning" :loading="writeBusy" @click="setValve">{{ t('gasControl.setValve') }}</el-button>
        <el-input-number v-model="form.a5Max" :min="0.01" :max="1000" :controls="false" placeholder="A5Max Pa" />
        <el-button type="danger" plain :loading="writeBusy" @click="setA5Max">{{ t('gasControl.setA5Max') }}</el-button>
        <el-button v-if="tripCode" type="danger" :loading="writeBusy" @click="clearA5">{{ t('gasControl.clearA5') }}</el-button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart,
  type ChartDataset
} from 'chart.js'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  gasCellA5Clear,
  gasCellA5Max,
  gasCellParams,
  gasCellStart,
  gasCellStatus,
  gasCellStop,
  gasCellValve,
  type GasCellFrame,
  type GasCellPoint,
  type PVWriteResult
} from '../api/instruments'
import { useAuthStore } from '../stores/auth'
import { chartPalette } from '../utils/chartTheme'

// Chart.register 已收口到 utils/chartTheme.ts setupChartDefaults()（美术方案 §3.7，main.ts 调用一次）

const { t } = useI18n()

const A1 = 'GasCell:Piezo:A1'
const VALVE = 'GasCell:Piezo:ValveSP'
const SETPOINT = 'GasCell:Piezo:Setpoint'
const RUNNING = 'GasCell:Piezo:Running'
const ERROR = 'GasCell:Piezo:Error'
const CYCLE = 'GasCell:Piezo:Cycle'
const TRIP = 'GasCell:Safety:A5Trip'

const cards = [
  { labelKey: 'gasControl.a1Pressure', pv: A1, unit: 'Pa' },
  { labelKey: 'gasControl.setpoint', pv: SETPOINT, unit: 'Pa' },
  { labelKey: 'gasControl.valveOpening', pv: VALVE, unit: '%' },
  { labelKey: 'gasControl.controlError', pv: ERROR, unit: '' },
  { labelKey: 'gasControl.runningState', pv: RUNNING, unit: '' },
  { labelKey: 'gasControl.cycle', pv: CYCLE, unit: '' }
]

const data = reactive<Record<string, GasCellPoint>>({})
const form = reactive<{ setpoint?: number; kp?: number; ki?: number; valve?: number; a5Max?: number }>({})
const auth = useAuthStore()
const canOperate = computed(() => ['maintainer', 'admin'].includes(auth.user?.role || ''))
const writeBusy = ref(false)
const loading = ref(true)
const error = ref('')
const streamError = ref('')
const connected = ref(false)
const chartCanvas = ref<HTMLCanvasElement>()
let chart: Chart | undefined
let source: EventSource | undefined
let epoch: number | undefined
let lastSeq: number | undefined

const tripCode = computed(() => Number(point(TRIP).v || 0))
const isRunning = computed(() => Number(point(RUNNING).v || 0) !== 0)

onMounted(async () => {
  await refreshSnapshot()
  await nextTick()
  createChart()
  connect()
})

onBeforeUnmount(() => {
  source?.close()
  chart?.destroy()
})

function point(pv: string): GasCellPoint {
  return data[pv] || { q: 'disconnected' }
}

function display(pv: string, unit: string) {
  const current = point(pv)
  if (current.q !== 'good' || current.v === undefined || current.v === null) return '—'
  if (pv === RUNNING) return Number(current.v) ? t('gasControl.running') : t('gasControl.stopped')
  const value = typeof current.v === 'number' ? Number(current.v.toPrecision(6)) : current.v
  return `${value}${unit ? ` ${unit}` : ''}`
}

async function refreshSnapshot() {
  loading.value = true
  try {
    Object.assign(data, (await gasCellStatus()).data)
    error.value = ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('gasControl.snapshotFailed')
  } finally {
    loading.value = false
  }
}

function connect() {
  source = new EventSource('/api/v1/ws/gascell')
  source.onopen = () => {
    connected.value = true
    streamError.value = ''
  }
  source.onerror = () => {
    connected.value = false
    streamError.value = t('gasControl.streamInterrupted')
  }
  source.onmessage = (event) => applyFrame(JSON.parse(event.data) as GasCellFrame)
}

function applyFrame(frame: GasCellFrame) {
  if ((epoch !== undefined && frame.epoch !== epoch) || (lastSeq !== undefined && frame.seq !== lastSeq + 1)) refreshSnapshot()
  epoch = frame.epoch
  lastSeq = frame.seq
  Object.assign(data, frame.data)
  appendChartPoint()
}

function createChart() {
  if (!chartCanvas.value) return
  // 系列语义色（美术方案 §3.7）：气压主系列 --chart-1、setpoint --chart-3（虚线 [6,4] 保留）、阀门 --chart-2
  const p = chartPalette()
  const datasets: ChartDataset<'line'>[] = [
    { label: 'A1 (Pa)', data: [], borderColor: p[0], yAxisID: 'pressure', pointRadius: 0 },
    { label: 'Setpoint (Pa)', data: [], borderColor: p[2], borderDash: [6, 4], yAxisID: 'pressure', pointRadius: 0 },
    { label: t('gasControl.chartValve'), data: [], borderColor: p[1], yAxisID: 'valve', pointRadius: 0 }
  ]
  chart = new Chart(chartCanvas.value, {
    type: 'line',
    data: { labels: [], datasets },
    options: {
      animation: false,
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      scales: {
        pressure: { type: 'linear', position: 'left', title: { display: true, text: 'Pa' } },
        valve: { type: 'linear', position: 'right', min: 0, max: 100, grid: { drawOnChartArea: false }, title: { display: true, text: '%' } }
      }
    }
  })
  appendChartPoint()
}

function appendChartPoint() {
  if (!chart) return
  const values = [A1, SETPOINT, VALVE].map((pv) => {
    const current = point(pv)
    return current.q === 'good' && typeof current.v === 'number' && Number.isFinite(current.v) ? current.v : null
  })
  chart.data.labels?.push(new Date().toLocaleTimeString())
  chart.data.datasets.forEach((dataset, index) => dataset.data.push(values[index]))
  if ((chart.data.labels?.length || 0) > 120) {
    chart.data.labels?.shift()
    chart.data.datasets.forEach((dataset) => dataset.data.shift())
  }
  chart.update('none')
}

async function write(action: () => Promise<PVWriteResult | PVWriteResult[]>, success: string) {
  writeBusy.value = true
  try {
    const result = await action()
    const warnings = (Array.isArray(result) ? result : [result]).map((item) => item.warning).filter(Boolean)
    warnings.length ? ElMessage.warning(warnings.join('; ')) : ElMessage.success(success)
    await refreshSnapshot()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('gasControl.writeFailed'))
  } finally {
    writeBusy.value = false
  }
}

function applyParams() {
  const params = Object.fromEntries(Object.entries({ setpoint: form.setpoint, kp: form.kp, ki: form.ki }).filter(([, value]) => value !== undefined))
  if (!Object.keys(params).length) return ElMessage.warning(t('gasControl.paramRequired'))
  return write(() => gasCellParams(params), t('gasControl.paramsWritten'))
}

function start() { return write(gasCellStart, t('gasControl.startSuccess')) }
function stop() { return write(gasCellStop, t('gasControl.stopSuccess')) }
function setValve() {
  if (form.valve === undefined) return ElMessage.warning(t('gasControl.valveRequired'))
  return write(() => gasCellValve(form.valve!), t('gasControl.valveWritten'))
}

async function setA5Max() {
  if (form.a5Max === undefined) return ElMessage.warning(t('gasControl.a5MaxRequired'))
  try {
    await ElMessageBox.confirm(t('gasControl.a5MaxConfirm'), t('gasControl.a5MaxConfirmTitle'), { type: 'warning' })
  } catch { return }
  return write(() => gasCellA5Max(form.a5Max!), t('gasControl.a5MaxWritten'))
}

async function clearA5() {
  try {
    await ElMessageBox.confirm(t('gasControl.clearA5Confirm'), t('gasControl.clearA5'), { type: 'error' })
  } catch { return }
  return write(gasCellA5Clear, t('gasControl.a5Cleared'))
}
</script>

<style scoped>
.gas-page { display: grid; gap: 20px; }
.page-header, .section-title { align-items: center; display: flex; justify-content: space-between; }
.page-header h1, .section-title h2 { margin: 0; }
.eyebrow { color: var(--brand-600); font-size: 12px; font-weight: 700; letter-spacing: .12em; margin: 0 0 4px; text-transform: uppercase; }
.subtitle, .section-title p { color: var(--text-3); margin: 5px 0 0; }
.status-grid { display: grid; gap: 14px; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); min-height: 130px; }
.status-card, .chart-card { background: var(--surface); border: 1px solid var(--border); border-radius: 14px; box-shadow: var(--shadow-sm); }
.status-card { display: grid; gap: 8px; padding: 18px; }
.status-card span, .status-card small { color: var(--text-3); }
.status-card strong { color: var(--navy-800); font-size: 24px; }
.status-card strong.invalid { color: var(--text-3); }
.status-card small { font-size: 11px; overflow: hidden; text-overflow: ellipsis; }
.chart-card { height: 460px; padding: 20px; }
.chart-card canvas { height: 385px !important; width: 100% !important; }
.state-panel { min-height: 340px; }
.control-card { background: var(--surface); border: 1px solid var(--border); border-radius: 14px; padding: 20px; }
.control-grid { align-items: end; display: grid; gap: 14px; grid-template-columns: repeat(4, minmax(130px, 1fr)); margin-top: 18px; }
.control-grid :deep(.el-input-number) { width: 100%; }
.control-actions :deep(.el-form-item__content) { align-items: flex-end; }
.button-row { align-items: center; border-top: 1px solid var(--border); display: flex; flex-wrap: wrap; gap: 10px; padding-top: 16px; }
@media (max-width: 768px) {
  .chart-card {
    height: 400px;
    padding: 14px;
  }

  .chart-card canvas {
    height: 320px !important;
  }

  .control-grid {
    grid-template-columns: 1fr 1fr;
  }
}
</style>
