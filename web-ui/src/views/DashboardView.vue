<template>
  <div class="page dashboard">
    <div class="toolbar dash-head">
      <div class="dash-title">
        <h2>{{ t('dashboard.title') }}</h2>
        <p class="dash-sub">{{ t('dashboard.subtitle') }}</p>
      </div>
      <div class="dash-date">
        <el-icon><Calendar /></el-icon>
        <span>{{ todayText }}</span>
      </div>
    </div>

    <section class="panel todo-panel">
      <div class="panel-head">
        <span class="panel-icon"><el-icon><Tickets /></el-icon></span>
        <h3>{{ t('dashboard.todayTodos') }}</h3>
        <span class="panel-meta">{{ t('dashboard.todosCount', { n: todos.length }) }}</span>
        <el-button class="todo-more" size="small" text type="primary" @click="router.push('/todos')">
          {{ t('dashboard.todosMore') }}
        </el-button>
      </div>
      <div v-loading="loadingTodos" class="todo-list">
        <el-empty v-if="!loadingTodos && !todos.length" :description="t('dashboard.noTodos')" :image-size="60" />
        <div v-for="item in todos" :key="item.id" class="todo-row" :class="{ done: item.status === 'done' }">
          <el-checkbox :model-value="item.status === 'done'" @change="toggleTodo(item)" />
          <span class="todo-priority" :class="item.priority">{{ todoPriorityLabel(item.priority) }}</span>
          <span class="todo-title">{{ item.title }}</span>
          <span class="todo-source">{{ todoSourceLabel(item) }}</span>
          <el-button v-if="item.status === 'pending'" size="small" text type="warning" @click="deferTodoItem(item)">
            {{ t('dashboard.defer') }}
          </el-button>
        </div>
      </div>
      <div class="todo-add-row">
        <el-input v-model="manualTitle" :placeholder="t('dashboard.todoAddPlaceholder')" maxlength="256" clearable @keyup.enter="addManualTodo" />
        <el-button type="primary" :loading="addingTodo" @click="addManualTodo">{{ t('dashboard.todoAdd') }}</el-button>
      </div>
      <div class="todo-add-row">
        <el-input v-model="llmText" :placeholder="t('dashboard.todoLLMPlaceholder')" maxlength="2000" clearable @keyup.enter="parseLLMTodo" />
        <el-button type="success" plain :loading="parsingLLM" @click="parseLLMTodo">{{ t('dashboard.todoLLM') }}</el-button>
      </div>
    </section>

    <div class="dashboard-grid">
      <!-- 左列：设备状态 -->
      <section class="panel column">
        <div class="panel-head">
          <span class="panel-icon"><el-icon><Odometer /></el-icon></span>
          <h3>{{ t('dashboard.deviceStatus') }}</h3>
          <span class="panel-meta">{{ t('dashboard.onlineCount', { online: onlineCount, total: instruments.length + 1 }) }}</span>
        </div>
        <div v-loading="loadingInstruments" class="card-list">
          <el-empty v-if="!loadingInstruments && !instruments.length" :description="t('dashboard.noDevices')" />
          <div
            v-for="(inst, i) in instruments"
            :key="inst.id"
            class="device-card"
            :style="stagger(i)"
            @click="router.push('/instrument-measure')"
          >
            <span class="status-dot" :class="{ online: isOnline(inst.state) }"></span>
            <span class="device-name">{{ inst.name }}</span>
            <span class="device-state" :class="{ online: isOnline(inst.state) }">
              {{ isOnline(inst.state) ? t('common.online') : t('common.offline') }}
            </span>
            <el-icon class="card-chev"><ArrowRight /></el-icon>
          </div>

          <div class="device-card gas-card" :style="stagger(instruments.length)" @click="router.push('/gas-control')">
            <div class="device-row">
              <span class="status-dot" :class="{ online: gasOnline }"></span>
              <span class="device-name">{{ t('dashboard.gasControl') }}</span>
              <span class="device-state" :class="{ online: gasOnline }">
                {{ gasOnline ? t('common.online') : t('common.offline') }}
              </span>
              <el-icon class="card-chev"><ArrowRight /></el-icon>
            </div>
            <div class="gas-stats" :class="{ offline: !gasOnline }">
              <div class="gas-stat">
                <span class="gas-label">{{ t('dashboard.runningState') }}</span>
                <span class="gas-value">{{ gasRunningText }}</span>
              </div>
              <div class="gas-stat">
                <span class="gas-label">{{ t('dashboard.a1Pressure') }}</span>
                <span class="gas-value">{{ gasA1Text }}</span>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- 中列：综合简报 -->
      <section class="panel column">
        <div class="panel-head">
          <span class="panel-icon"><el-icon><DataAnalysis /></el-icon></span>
          <h3>{{ t('dashboard.brief') }}</h3>
          <span class="panel-meta">{{ t('dashboard.last7Days') }}</span>
        </div>
        <div v-loading="loadingReports" class="brief-strip">
          <div
            v-for="(day, i) in briefDays"
            :key="day.date"
            class="brief-card"
            :class="{ active: day.date === selectedDate }"
            :style="stagger(i)"
            @click="selectDate(day.date)"
          >
            <div class="brief-top">
              <span class="brief-date">{{ briefDayLabel(day.date) }}</span>
              <span class="brief-count">{{ t('dashboard.peopleCount', { n: day.reports.length }) }}</span>
            </div>
            <span class="brief-week">{{ weekdayLabel(day.date) }}</span>
            <p class="brief-summary" :class="{ empty: !day.summary }">{{ day.summary || t('dashboard.noReport') }}</p>
          </div>
        </div>
      </section>

      <!-- 右列：团队成员日报 -->
      <section class="panel column">
        <div class="panel-head">
          <span class="panel-icon"><el-icon><Avatar /></el-icon></span>
          <h3>{{ t('dashboard.teamReports') }}</h3>
          <span class="panel-meta">{{ t('dashboard.reportsCount', { n: dayReports.length }) }}</span>
        </div>
        <div class="date-bar">
          <el-button :icon="ArrowLeft" circle size="small" @click="shiftDate(-1)" />
          <el-date-picker v-model="selectedDate" type="date" value-format="YYYY-MM-DD" :clearable="false" />
          <el-button :icon="ArrowRight" circle size="small" @click="shiftDate(1)" />
        </div>
        <div v-loading="loadingReports" class="card-list">
          <el-empty v-if="!loadingReports && !dayReports.length" :description="t('dashboard.noReportToday')" />
          <div
            v-for="(report, i) in dayReports"
            :key="report.id"
            class="member-card"
            :style="stagger(i)"
            @click="router.push('/daily-reports/' + report.id)"
          >
            <div class="member-row">
              <span class="avatar">{{ initial(report) }}</span>
              <span class="member-name">{{ report.author_name || report.author_id }}</span>
              <el-icon class="card-chev"><ArrowRight /></el-icon>
            </div>
            <p class="member-summary" :class="{ empty: !report.summary }">
              {{ truncate(report.summary) || t('dashboard.noSummary') }}
            </p>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, ArrowRight, Avatar, Calendar, DataAnalysis, Odometer, Tickets } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listInstruments, gasCellStatus, type InstrumentSummary, type GasCellPoint } from '../api/instruments'
import { listReports, type DailyReport } from '../api/logs'
import { listTodos, createTodo, doneTodo, deferTodo, llmParse, llmAdd, type Todo } from '../api/todos'
import { showApiError } from '../composables/useNotify'

const router = useRouter()
const { t, locale } = useI18n()

const RUNNING = 'GasCell:Piezo:Running'
const A1 = 'GasCell:Piezo:A1'

const instruments = ref<InstrumentSummary[]>([])
const gasData = reactive<Record<string, GasCellPoint>>({})
const reports = ref<DailyReport[]>([])
const loadingInstruments = ref(false)
const loadingReports = ref(false)
const todos = ref<Todo[]>([])
const loadingTodos = ref(false)
const manualTitle = ref('')
const llmText = ref('')
const addingTodo = ref(false)
const parsingLLM = ref(false)
const llmDraft = ref<{ title: string; priority: Todo['priority']; reason?: string | null } | null>(null)
// 默认显示昨天
const selectedDate = ref(localDate(new Date(Date.now() - 86400000)))

// 本地时区日期格式化，不用 toISOString（UTC 会差一天）
function localDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

onMounted(() => {
  loadInstruments()
  loadGasCell()
  loadReports()
  loadTodos()
})

async function loadInstruments() {
  loadingInstruments.value = true
  try {
    instruments.value = await listInstruments()
  } catch (err) {
    showApiError(err, t('dashboard.loadDevicesFailed'))
  } finally {
    loadingInstruments.value = false
  }
}

async function loadGasCell() {
  try {
    Object.assign(gasData, (await gasCellStatus()).data)
  } catch (err) {
    showApiError(err, t('dashboard.loadGasFailed'))
  }
}

async function loadReports() {
  loadingReports.value = true
  try {
    reports.value = (await listReports({ per_page: 100 })).items ?? []
  } catch (err) {
    showApiError(err, t('dashboard.loadReportsFailed'))
  } finally {
    loadingReports.value = false
  }
}

async function loadTodos() {
  loadingTodos.value = true
  try {
    todos.value = await listTodos({ status: 'open' })
  } catch (err) {
    showApiError(err, t('dashboard.todosLoadFailed'))
  } finally {
    loadingTodos.value = false
  }
}

async function toggleTodo(item: Todo) {
  try {
    await doneTodo(item.id)
    ElMessage.success(t('dashboard.todoDone'))
    loadTodos()
  } catch (err) {
    showApiError(err, t('dashboard.todoDoneFailed'))
  }
}

async function deferTodoItem(item: Todo) {
  try {
    await deferTodo(item.id)
    ElMessage.success(t('dashboard.todoDeferred'))
    loadTodos()
  } catch (err) {
    showApiError(err, t('dashboard.todoDeferFailed'))
  }
}

async function addManualTodo() {
  const title = manualTitle.value.trim()
  if (!title) return
  addingTodo.value = true
  try {
    await createTodo({ title })
    ElMessage.success(t('dashboard.todoAdded'))
    manualTitle.value = ''
    loadTodos()
  } catch (err) {
    showApiError(err, t('dashboard.todoAddFailed'))
  } finally {
    addingTodo.value = false
  }
}

async function parseLLMTodo() {
  const text = llmText.value.trim()
  if (!text) return
  parsingLLM.value = true
  try {
    const draft = await llmParse(text)
    if (draft.status === 'rejected') {
      ElMessage.info(draft.reason || t('dashboard.todoLLMRejected', { reason: '' }))
      return
    }
    llmDraft.value = { title: draft.title, priority: draft.priority, reason: draft.reason }
    if (draft.reason) ElMessage.info(t('dashboard.todoLLMRejected', { reason: draft.reason }))
    const confirmed = await ElMessageBox.confirm(
      `${t('dashboard.todoDraftTitle')}：${draft.title}`,
      t('dashboard.todoDraftConfirm'),
      { confirmButtonText: t('dashboard.todoDraftSave'), cancelButtonText: t('common.cancel'), type: 'info' }
    )
    if (confirmed !== 'confirm') return
    await llmAdd({ title: draft.title, priority: draft.priority })
    ElMessage.success(t('dashboard.todoAdded'))
    llmText.value = ''
    llmDraft.value = null
    loadTodos()
  } catch (err) {
    if (err === 'cancel' || err === 'close') return
    showApiError(err, t('dashboard.todoLLMFailed'))
  } finally {
    parsingLLM.value = false
  }
}

function todoPriorityLabel(p: string) {
  return t(`todos.priority${p.charAt(0).toUpperCase()}${p.slice(1)}`)
}

function todoSourceLabel(item: Todo) {
  if (item.source === 'issue') return t('todos.sourceIssue')
  if (item.source === 'daily_llm') return t('todos.sourceDaily')
  if (item.source === 'llm') return t('todos.sourceLLM')
  return t('todos.sourceManual')
}

function isOnline(state: string) {
  return state === 'running'
}

function point(pv: string): GasCellPoint {
  return gasData[pv] || { q: 'disconnected' }
}

// snapshot q !== 'good' 时视为离线（灰色展示）
const gasOnline = computed(() => point(RUNNING).q === 'good' && point(A1).q === 'good')

const gasRunningText = computed(() => {
  if (point(RUNNING).q !== 'good') return '—'
  return Number(point(RUNNING).v) ? t('dashboard.running') : t('dashboard.stopped')
})

const gasA1Text = computed(() => {
  const p = point(A1)
  if (p.q !== 'good' || p.v === undefined || p.v === null) return '—'
  const value = typeof p.v === 'number' ? Number(p.v.toPrecision(6)) : p.v
  return `${value} Pa`
})

// 客户端按 report_date 分组
const reportsByDate = computed(() => {
  const grouped: Record<string, DailyReport[]> = {}
  for (const r of reports.value) {
    ;(grouped[r.report_date] ||= []).push(r)
  }
  return grouped
})

// 最近 7 天（今天往前），摘要拼接后截断 200 字
const briefDays = computed(() =>
  Array.from({ length: 7 }, (_, i) => {
    const date = localDate(new Date(Date.now() - i * 86400000))
    const dayReports = reportsByDate.value[date] || []
    return {
      date,
      reports: dayReports,
      summary: truncate(dayReports.map((r) => r.summary).filter(Boolean).join('；'), 200)
    }
  })
)

const dayReports = computed(() => reportsByDate.value[selectedDate.value] || [])

// 日期被清空时回退到昨天
watch(selectedDate, (val) => {
  if (!val) selectedDate.value = localDate(new Date(Date.now() - 86400000))
})

function shiftDate(delta: number) {
  const base = selectedDate.value ? new Date(`${selectedDate.value}T00:00:00`) : new Date()
  selectedDate.value = localDate(new Date(base.getTime() + delta * 86400000))
}

function selectDate(date: string) {
  selectedDate.value = date
}

function truncate(text: string | undefined, max = 120) {
  if (!text) return ''
  return text.length > max ? `${text.slice(0, max)}…` : text
}

function initial(report: DailyReport) {
  const name = (report.author_name || report.author_id || '?').trim()
  return name.charAt(0).toUpperCase()
}

/* ---------- 纯展示辅助 ---------- */

// 日期显示跟随界面语言（zh → zh-CN，en → en-US）
const dateLocale = computed(() => (locale.value === 'zh' ? 'zh-CN' : 'en-US'))

// 顶栏日期与简报卡片文案，仅用于显示，不参与数据逻辑
const todayStr = localDate(new Date())
const yesterdayStr = localDate(new Date(Date.now() - 86400000))
const todayText = computed(() =>
  new Date().toLocaleDateString(dateLocale.value, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    weekday: 'long'
  })
)

// 设备列角标：在线数（仪器 + 气压控制）
const onlineCount = computed(
  () => instruments.value.filter((i) => isOnline(i.state)).length + (gasOnline.value ? 1 : 0)
)

function briefDayLabel(date: string) {
  if (date === todayStr) return t('dashboard.today')
  if (date === yesterdayStr) return t('dashboard.yesterday')
  return date.slice(5) // MM-DD
}

function weekdayLabel(date: string) {
  return new Date(`${date}T00:00:00`).toLocaleDateString(dateLocale.value, { weekday: 'short' })
}

// 卡片入场动画的交错延迟
function stagger(i: number) {
  return { animationDelay: `${i * 45}ms` }
}
</script>

<style scoped>
/* ---------- 页头 ---------- */

.dash-title h2 {
  font-size: 22px;
}

.dash-sub {
  color: var(--text-3);
  font-size: 13px;
  margin-top: 2px;
}

.dash-date {
  align-items: center;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-full);
  box-shadow: var(--shadow-sm);
  color: var(--text-2);
  display: inline-flex;
  font-size: 13px;
  gap: 8px;
  padding: 7px 14px;
  white-space: nowrap;
}

.dash-date .el-icon {
  color: var(--brand-600);
}

/* ---------- 三列布局 ---------- */

.dashboard-grid {
  align-items: start;
  display: grid;
  gap: 20px;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.panel-head {
  border-bottom: 1px solid var(--border);
  padding-bottom: 12px;
}

.panel-meta {
  margin-left: auto;
}

.card-list {
  align-content: start;
  display: grid;
  gap: var(--space-3);
  min-height: 80px;
}

/* ---------- 今日待办 ---------- */

.todo-panel {
  margin-bottom: 12px;
}

.todo-more {
  margin-left: auto;
}

.todo-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-height: 48px;
}

.todo-row {
  align-items: center;
  border-bottom: 1px solid var(--border);
  display: flex;
  gap: 10px;
  padding: 6px 8px;
}

.todo-row.done .todo-title {
  color: var(--text-3);
  text-decoration: line-through;
}

.todo-priority {
  border-radius: 4px;
  font-size: 12px;
  padding: 1px 6px;
  white-space: nowrap;
}

.todo-priority.high {
  background: #fde2e2;
  color: #c45656;
}

.todo-priority.medium {
  background: #fff3cd;
  color: #b58a1d;
}

.todo-priority.low {
  background: #e3f2fd;
  color: #3a7dc2;
}

.todo-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.todo-source {
  color: var(--text-3);
  font-size: 12px;
  white-space: nowrap;
}

.todo-add-row {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.todo-add-row .el-input {
  flex: 1;
}

/* ---------- 卡片基座 ---------- */

.device-card,
.brief-card,
.member-card {
  animation: card-in 0.4s cubic-bezier(0.21, 0.61, 0.35, 1) both;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-sm);
  cursor: pointer;
  padding: 14px 16px;
  transition:
    border-color 0.18s ease,
    box-shadow 0.18s ease,
    transform 0.18s ease;
}

.device-card:hover,
.brief-card:hover,
.member-card:hover {
  border-color: var(--brand-500);
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
}

@keyframes card-in {
  from {
    opacity: 0;
    translate: 0 10px;
  }
  to {
    opacity: 1;
    translate: 0 0;
  }
}

/* ---------- 设备卡片 ---------- */

.device-card {
  align-items: center;
  display: flex;
  gap: 10px;
}

.device-name {
  color: var(--text-1);
  font-weight: 600;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-dot {
  background: #b9c6d2;
  border-radius: 50%;
  flex-shrink: 0;
  height: 8px;
  position: relative;
  width: 8px;
}

.status-dot.online {
  background: var(--ok);
}

.status-dot.online::after {
  animation: dot-pulse 2.2s ease-out infinite;
  border: 2px solid var(--ok);
  border-radius: 50%;
  content: '';
  inset: -4px;
  position: absolute;
}

@keyframes dot-pulse {
  0% {
    opacity: 0.8;
    transform: scale(0.6);
  }
  70%,
  100% {
    opacity: 0;
    transform: scale(1.15);
  }
}

.device-state {
  color: var(--text-3);
  flex-shrink: 0;
  font-size: 12px;
  margin-left: auto;
}

.device-state.online {
  color: var(--ok);
  font-weight: 600;
}

.card-chev {
  color: var(--text-3);
  flex-shrink: 0;
  font-size: 14px;
  transition:
    color 0.18s ease,
    translate 0.18s ease;
}

.device-card:hover .card-chev,
.member-card:hover .card-chev {
  color: var(--brand-600);
  translate: 2px 0;
}

/* 气压控制卡：淡品牌色底，与仪器卡区分 */

.gas-card {
  background: linear-gradient(150deg, var(--brand-050) 0%, var(--surface) 70%);
  border-color: var(--brand-100);
  display: block;
}

.device-row {
  align-items: center;
  display: flex;
  gap: 10px;
}

.gas-stats {
  display: grid;
  gap: 10px;
  grid-template-columns: 1fr 1fr;
  margin-top: 12px;
}

.gas-stat {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  display: grid;
  gap: 1px;
  padding: 8px 12px;
}

.gas-label {
  color: var(--text-3);
  font-size: 12px;
}

.gas-value {
  color: var(--text-1);
  font-size: 14px;
  font-variant-numeric: tabular-nums;
  font-weight: 650;
}

.gas-stats.offline .gas-value {
  color: var(--text-3);
}

/* ---------- 简报卡片（横向滚动条） ---------- */

.brief-strip {
  display: flex;
  gap: var(--space-3);
  margin: -4px -4px -12px;
  overflow-x: auto;
  padding: 4px 4px 16px;
}

.brief-card {
  display: flex;
  flex: 0 0 210px;
  flex-direction: column;
  height: 158px;
}

.brief-card.active {
  background: var(--brand-050);
  border-color: var(--brand-500);
  box-shadow: 0 6px 16px -8px rgba(18, 112, 138, 0.35);
}

.brief-top {
  align-items: center;
  display: flex;
  justify-content: space-between;
}

.brief-date {
  color: var(--text-1);
  font-size: 15px;
  font-weight: 650;
}

.brief-count {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-full);
  color: var(--text-3);
  font-size: 11px;
  padding: 1px 8px;
  transition:
    background 0.18s ease,
    color 0.18s ease;
  white-space: nowrap;
}

.brief-card.active .brief-count {
  background: var(--brand-600);
  border-color: var(--brand-600);
  color: #fff;
}

.brief-week {
  color: var(--text-3);
  font-size: 12px;
  margin-top: 2px;
}

.brief-summary {
  color: var(--text-2);
  display: -webkit-box;
  font-size: 13px;
  line-height: 1.55;
  margin: 8px 0 0;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 4;
}

.brief-summary.empty {
  color: var(--text-3);
}

/* ---------- 日报卡片 ---------- */

.date-bar {
  align-items: center;
  display: flex;
  gap: 8px;
  margin-bottom: 14px;
}

.date-bar .el-date-editor {
  flex: 1;
}

.member-row {
  align-items: center;
  display: flex;
  gap: 10px;
}

.avatar {
  align-items: center;
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: 10px;
  box-shadow: 0 2px 6px -2px rgba(18, 112, 138, 0.5);
  color: #fff;
  display: inline-flex;
  flex-shrink: 0;
  font-size: 14px;
  font-weight: 700;
  height: 34px;
  justify-content: center;
  width: 34px;
}

.member-name {
  color: var(--text-1);
  font-weight: 600;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.member-row .card-chev {
  margin-left: auto;
}

.member-summary {
  color: var(--text-2);
  display: -webkit-box;
  font-size: 13px;
  margin: 10px 0 0;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}

.member-summary.empty {
  color: var(--text-3);
}

/* ---------- 响应式与动效偏好 ---------- */

@media (max-width: 768px) {
  .dashboard-grid {
    grid-template-columns: 1fr;
  }

  .brief-card {
    flex-basis: 186px;
  }

  .gas-stats {
    grid-template-columns: 1fr;
  }

  .dash-date {
    white-space: normal;
  }
}

@media (max-width: 480px) {
  .gas-stats {
    grid-template-columns: 1fr;
  }
}
</style>
