<template>
  <div class="page dashboard">
    <div class="toolbar">
      <h2>实验室仪表盘</h2>
    </div>

    <div class="dashboard-grid">
      <!-- 左列：设备状态 -->
      <section class="panel column">
        <h3>设备状态</h3>
        <div v-loading="loadingInstruments" class="card-list">
          <el-empty v-if="!loadingInstruments && !instruments.length" description="暂无设备" />
          <el-card
            v-for="inst in instruments"
            :key="inst.id"
            shadow="hover"
            class="device-card"
            @click="router.push('/instrument-measure')"
          >
            <div class="device-row">
              <span class="device-name">{{ inst.name }}</span>
              <el-tag :type="isOnline(inst.state) ? 'success' : 'info'" size="small">
                {{ isOnline(inst.state) ? '在线' : '离线' }}
              </el-tag>
            </div>
          </el-card>

          <el-card shadow="hover" class="device-card" @click="router.push('/gas-control')">
            <div class="device-row">
              <span class="device-name">气压控制</span>
              <el-tag :type="gasOnline ? 'success' : 'info'" size="small">
                {{ gasOnline ? '在线' : '离线' }}
              </el-tag>
            </div>
            <div class="gas-values" :class="{ offline: !gasOnline }">
              <span>状态：{{ gasRunningText }}</span>
              <span>A1：{{ gasA1Text }}</span>
            </div>
          </el-card>
        </div>
      </section>

      <!-- 中列：综合简报 -->
      <section class="panel column">
        <h3>综合简报</h3>
        <div v-loading="loadingReports" class="brief-strip">
          <el-card
            v-for="day in briefDays"
            :key="day.date"
            shadow="hover"
            class="brief-card"
            :class="{ active: day.date === selectedDate }"
            @click="selectDate(day.date)"
          >
            <div class="brief-date">{{ day.date }}</div>
            <div class="brief-count">{{ day.reports.length }} 人</div>
            <p class="brief-summary">{{ day.summary || '暂无日报' }}</p>
          </el-card>
        </div>
      </section>

      <!-- 右列：团队成员日报 -->
      <section class="panel column">
        <h3>团队成员日报</h3>
        <div class="date-bar">
          <el-button :icon="ArrowLeft" circle size="small" @click="shiftDate(-1)" />
          <el-date-picker v-model="selectedDate" type="date" value-format="YYYY-MM-DD" :clearable="false" />
          <el-button :icon="ArrowRight" circle size="small" @click="shiftDate(1)" />
        </div>
        <div v-loading="loadingReports" class="card-list">
          <el-empty v-if="!loadingReports && !dayReports.length" description="当天暂无日报" />
          <el-card
            v-for="report in dayReports"
            :key="report.id"
            shadow="hover"
            class="member-card"
            @click="router.push('/daily-reports/' + report.id)"
          >
            <div class="member-row">
              <span class="avatar">{{ initial(report) }}</span>
              <span class="member-name">{{ report.author_name || report.author_id }}</span>
            </div>
            <p class="member-summary">{{ truncate(report.summary) || '暂无摘要' }}</p>
          </el-card>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight } from '@element-plus/icons-vue'
import { listInstruments, gasCellStatus, type InstrumentSummary, type GasCellPoint } from '../api/instruments'
import { listReports, type DailyReport } from '../api/logs'
import { showApiError } from '../composables/useNotify'

const router = useRouter()

const RUNNING = 'GasCell:Piezo:Running'
const A1 = 'GasCell:Piezo:A1'

const instruments = ref<InstrumentSummary[]>([])
const gasData = reactive<Record<string, GasCellPoint>>({})
const reports = ref<DailyReport[]>([])
const loadingInstruments = ref(false)
const loadingReports = ref(false)
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
})

async function loadInstruments() {
  loadingInstruments.value = true
  try {
    instruments.value = await listInstruments()
  } catch (err) {
    showApiError(err, '设备列表加载失败')
  } finally {
    loadingInstruments.value = false
  }
}

async function loadGasCell() {
  try {
    Object.assign(gasData, (await gasCellStatus()).data)
  } catch (err) {
    showApiError(err, '气压状态加载失败')
  }
}

async function loadReports() {
  loadingReports.value = true
  try {
    reports.value = (await listReports({ per_page: 100 })).items ?? []
  } catch (err) {
    showApiError(err, '日报加载失败')
  } finally {
    loadingReports.value = false
  }
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
  return Number(point(RUNNING).v) ? '运行中' : '已停止'
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
</script>

<style scoped>
.dashboard-grid {
  align-items: start;
  display: grid;
  gap: 20px;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.column h3 {
  font-size: 16px;
  margin: 0 0 14px;
}

.card-list {
  align-content: start;
  display: grid;
  gap: 12px;
  min-height: 80px;
}

.device-card,
.brief-card,
.member-card {
  cursor: pointer;
}

.device-row {
  align-items: center;
  display: flex;
  gap: 8px;
  justify-content: space-between;
}

.device-name {
  font-weight: 600;
}

.gas-values {
  color: var(--text-2);
  display: flex;
  font-size: 13px;
  gap: 16px;
  margin-top: 10px;
}

.gas-values.offline {
  color: var(--text-3);
}

.brief-strip {
  display: flex;
  gap: 12px;
  overflow-x: auto;
  padding-bottom: 6px;
}

.brief-card {
  flex: 0 0 220px;
  width: 220px;
}

.brief-card.active {
  border-color: var(--el-color-primary);
}

.brief-date {
  font-weight: 600;
}

.brief-count {
  color: var(--text-3);
  font-size: 12px;
  margin: 4px 0;
}

.brief-summary {
  color: var(--text-2);
  display: -webkit-box;
  font-size: 13px;
  margin: 0;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 6;
}

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
  background: var(--el-color-primary);
  border-radius: 50%;
  color: #fff;
  display: inline-flex;
  flex-shrink: 0;
  font-size: 14px;
  height: 32px;
  justify-content: center;
  width: 32px;
}

.member-name {
  font-weight: 600;
}

.member-summary {
  color: var(--text-2);
  display: -webkit-box;
  font-size: 13px;
  margin: 8px 0 0;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}

@media (max-width: 768px) {
  .dashboard-grid {
    grid-template-columns: 1fr;
  }
}
</style>
