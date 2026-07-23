<template>
  <div class="gas-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">GasCell</p>
        <h1>气压控制</h1>
        <p class="subtitle">只读实时仪表盘，控制回路与安全联锁运行在 IOC。</p>
      </div>
      <el-tag :type="connected ? 'success' : 'warning'">{{ connected ? '实时推送' : '正在重连' }}</el-tag>
    </header>

    <el-alert v-if="streamError" :title="streamError" type="warning" :closable="false" show-icon />
    <el-alert v-if="tripCode" :title="`A5 联锁已触发（代码 ${tripCode}）`" type="error" :closable="false" show-icon />

    <section v-loading="loading" class="status-grid">
      <article v-for="card in cards" :key="card.pv" class="status-card">
        <span>{{ card.label }}</span>
        <strong :class="{ invalid: point(card.pv).q !== 'good' }">{{ display(card.pv, card.unit) }}</strong>
        <small>{{ point(card.pv).q === 'good' ? card.pv : '数据失效' }}</small>
      </article>
    </section>

    <section class="param-panel">
      <div class="section-title">
        <div><h2>当前控制参数</h2><p>PID 参数与目标值实时显示</p></div>
      </div>
      <div class="param-grid">
        <div v-for="p in params" :key="p.pv" class="param-item">
          <span class="param-label">{{ p.label }}</span>
          <span class="param-value" :class="{ stale: point(p.pv).q !== 'good' }">{{ display(p.pv, p.unit) }}</span>
        </div>
      </div>
    </section>

    <section class="chart-card">
      <div class="section-title">
        <div>
          <h2>A1 / 阀位 / Setpoint</h2>
          <p>最近 120 个有效采样点</p>
        </div>
      </div>
      <div v-if="error" class="state-panel"><el-result icon="error" title="数据加载失败" :sub-title="error" /></div>
      <canvas v-else ref="chartCanvas" aria-label="GasCell 实时曲线"></canvas>
    </section>

    <section v-if="canOperate" class="control-card">
      <div class="section-title">
        <div><h2>控制面板</h2><p>每次写入均由后端校验并回读确认。</p></div>
        <el-tag type="warning">maintainer / admin</el-tag>
      </div>
      <el-form class="control-grid" label-position="top" @submit.prevent>
        <el-form-item label="Setpoint (Pa)"><el-input-number v-model="form.setpoint" :min="0" :max="10000" :controls="false" /></el-form-item>
        <el-form-item label="Kp"><el-input-number v-model="form.kp" :min="0" :max="1" :controls="false" /></el-form-item>
        <el-form-item label="Ki"><el-input-number v-model="form.ki" :min="0" :max="1" :controls="false" /></el-form-item>
        <el-form-item class="control-actions"><el-button type="primary" :loading="writeBusy" @click="applyParams">应用参数</el-button></el-form-item>
      </el-form>
      <div class="button-row">
        <el-button v-if="!isRunning" type="success" :loading="writeBusy" @click="start">启动</el-button>
        <el-button v-else type="warning" :loading="writeBusy" @click="stop">停止</el-button>
        <el-input-number v-model="form.valve" :min="0" :max="100" :controls="false" placeholder="手动阀位 %" />
        <el-button :disabled="isRunning" :loading="writeBusy" @click="setValve">设置阀位</el-button>
        <el-input-number v-model="form.a5Max" :min="0.01" :max="1000" :controls="false" placeholder="A5Max Pa" />
        <el-button type="danger" plain :loading="writeBusy" @click="setA5Max">修改 A5Max</el-button>
        <el-button v-if="tripCode" type="danger" :loading="writeBusy" @click="clearA5">清除 A5 联锁</el-button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  Chart,
  LineController,
  LineElement,
  PointElement,
  LinearScale,
  CategoryScale,
  Legend,
  Tooltip,
  type ChartDataset
} from 'chart.js'
import { ElMessage, ElMessageBox } from 'element-plus'
import { showApiError } from '../composables/useNotify'
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

Chart.register(LineController, LineElement, PointElement, LinearScale, CategoryScale, Legend, Tooltip)

const A1 = 'GasCell:Piezo:A1'
const VALVE = 'GasCell:Piezo:ValveSP'
const SETPOINT = 'GasCell:Piezo:Setpoint'
const RUNNING = 'GasCell:Piezo:Running'
const Kp = 'GasCell:Piezo:Kp'
const Ki = 'GasCell:Piezo:Ki'
const KdPV = 'GasCell:Piezo:Kd'
const ERROR = 'GasCell:Piezo:Error'
const CYCLE = 'GasCell:Piezo:Cycle'
const TRIP = 'GasCell:Safety:A5Trip'

const cards = [
  { label: 'A1 气压', pv: A1, unit: 'Pa' },
  { label: '设定值', pv: SETPOINT, unit: 'Pa' },
  { label: '阀门开度', pv: VALVE, unit: '%' },
  { label: '控制误差', pv: ERROR, unit: '' },
  { label: '运行状态', pv: RUNNING, unit: '' },
  { label: 'Cycle', pv: CYCLE, unit: '' }
]

const params = [
  { label: 'Kp', pv: Kp, unit: '' },
  { label: 'Ki', pv: Ki, unit: '' },
  { label: 'Kd', pv: KdPV, unit: '' },
  { label: 'Setpoint', pv: SETPOINT, unit: 'Pa' },
  { label: 'ValveSP', pv: VALVE, unit: '%' },
  { label: 'Error', pv: ERROR, unit: '' }
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
  if (pv === RUNNING) return Number(current.v) ? '运行中' : '已停止'
  const value = typeof current.v === 'number' ? Number(current.v.toPrecision(6)) : current.v
  return `${value}${unit ? ` ${unit}` : ''}`
}

async function refreshSnapshot() {
  loading.value = true
  try {
    Object.assign(data, (await gasCellStatus()).data)
    error.value = ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : '快照加载失败'
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
    streamError.value = '实时连接中断，浏览器正在自动重连。'
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
  const datasets: ChartDataset<'line'>[] = [
    { label: 'A1 (Pa)', data: [], borderColor: '#167d9a', yAxisID: 'pressure', pointRadius: 0 },
    { label: 'Setpoint (Pa)', data: [], borderColor: '#e6a23c', borderDash: [6, 4], yAxisID: 'pressure', pointRadius: 0 },
    { label: '阀位 (%)', data: [], borderColor: '#67c23a', yAxisID: 'valve', pointRadius: 0 }
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
    warnings.length ? ElMessage.warning(warnings.join('；')) : ElMessage.success(success)
    await refreshSnapshot()
  } catch (err) {
    showApiError(err, '写入失败')
  } finally {
    writeBusy.value = false
  }
}

function applyParams() {
  const params = Object.fromEntries(Object.entries({ setpoint: form.setpoint, kp: form.kp, ki: form.ki }).filter(([, value]) => value !== undefined))
  if (!Object.keys(params).length) return ElMessage.warning('请至少填写一个参数')
  return write(() => gasCellParams(params), '参数已写入并回读确认')
}

function start() { return write(gasCellStart, '控制已启动') }
function stop() { return write(gasCellStop, '控制已停止') }
function setValve() {
  if (form.valve === undefined) return ElMessage.warning('请填写手动阀位')
  return write(() => gasCellValve(form.valve!), '阀位已写入并回读确认')
}

async function setA5Max() {
  if (form.a5Max === undefined) return ElMessage.warning('请填写 A5Max')
  try {
    await ElMessageBox.confirm('修改安全阈值会影响 A5 超压联锁，确认继续？', '安全阈值确认', { type: 'warning' })
  } catch { return }
  return write(() => gasCellA5Max(form.a5Max!), 'A5Max 已写入并回读确认')
}

async function clearA5() {
  try {
    await ElMessageBox.confirm('请确认现场条件已安全。清除报警并解锁？', '清除 A5 联锁', { type: 'error' })
  } catch { return }
  return write(gasCellA5Clear, 'A5 联锁已清除')
}
</script>

<style scoped>
.gas-page { display: grid; gap: 20px; }
.page-header, .section-title { align-items: center; display: flex; justify-content: space-between; }
.page-header h1, .section-title h2 { margin: 0; }
.eyebrow { color: var(--brand-600); font-size: 12px; font-weight: 700; letter-spacing: .12em; margin: 0 0 4px; text-transform: uppercase; }
.subtitle, .section-title p { color: var(--text-secondary); margin: 5px 0 0; }
.status-grid { display: grid; gap: 14px; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); min-height: 130px; }
.status-card, .chart-card { background: #fff; border: 1px solid var(--border-color); border-radius: 14px; box-shadow: var(--shadow-sm); }
.status-card { display: grid; gap: 8px; padding: 18px; }
.status-card span, .status-card small { color: var(--text-secondary); }
.status-card strong { color: var(--navy-800); font-size: 24px; }
.status-card strong.invalid { color: var(--text-secondary); }
.status-card small { font-size: 11px; overflow: hidden; text-overflow: ellipsis; }
.chart-card { height: 460px; padding: 20px; }
.chart-card canvas { height: 385px !important; width: 100% !important; }
.state-panel { min-height: 340px; }
.param-panel { background: #fff; border: 1px solid var(--border-color); border-radius: 14px; box-shadow: var(--shadow-sm); padding: 20px; }
.param-grid { display: grid; gap: 12px; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); margin-top: 14px; }
.param-item { align-items: center; background: var(--bg-subtle); border-radius: 10px; display: flex; flex-direction: column; gap: 4px; padding: 14px 12px; }
.param-label { color: var(--text-secondary); font-size: 13px; font-weight: 600; }
.param-value { color: var(--navy-800); font-family: var(--font-mono); font-size: 22px; font-weight: 700; }
.param-value.stale { color: var(--text-secondary); }
.control-card { background: #fff; border: 1px solid var(--border); border-radius: 14px; padding: 20px; }
.control-grid { align-items: end; display: grid; gap: 14px; grid-template-columns: repeat(4, minmax(130px, 1fr)); margin-top: 18px; }
.control-grid :deep(.el-input-number) { width: 100%; }
.control-actions :deep(.el-form-item__content) { align-items: flex-end; }
.button-row { align-items: center; border-top: 1px solid var(--border); display: flex; flex-wrap: wrap; gap: 10px; padding-top: 16px; }
@media (max-width: 700px) { .chart-card { height: 400px; padding: 14px; } .chart-card canvas { height: 325px !important; } }
@media (max-width: 900px) { .control-grid { grid-template-columns: 1fr 1fr; } }
</style>
