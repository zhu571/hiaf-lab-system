import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError, isApiError } from '@/api/client'
import { listReports, type DailyReport } from '@/api/logs'

// 首页简报与成员日报共享的日报数据单例（结构改版 R6 §7.1 拆分）：拆分前 DashboardView
// 单组件内两块面板共用同一份 listReports 数据与 selectedDate 日期状态；拆分后经本模块级
// 单例保持「单次取数、互相同步」的既有口径（对齐 useAskDialog/useCommandPalette 先例）。
// useAsyncData 内含 onBeforeUnmount 需组件实例，模块作用域无法使用，故按同等语义
// （seq 竞态保护 + 不自动 toast）在此手写等价实现；页面访问级刷新由面板 onMounted 的
// activeMounts 首挂载判定驱动（两面板同访一次取数，再次访问重新取数）。

const reportsData = ref<DailyReport[] | null>(null)
const loading = ref(false)
const error = ref<ApiError | null>(null)
let seq = 0
let activeMounts = 0

// 本地时区日期格式化，不用 toISOString（UTC 会差一天）
function localDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function yesterdayDate() {
  return localDate(new Date(Date.now() - 86400000))
}

// 默认显示昨天
const selectedDate = ref(yesterdayDate())

// 日期被清空时回退到昨天（模块级 watch，无需组件实例）
watch(selectedDate, (val) => {
  if (!val) selectedDate.value = yesterdayDate()
})

const reports = computed(() => reportsData.value ?? [])

// 客户端按 report_date 分组
const reportsByDate = computed(() => {
  const grouped: Record<string, DailyReport[]> = {}
  for (const r of reports.value) {
    ;(grouped[r.report_date] ||= []).push(r)
  }
  return grouped
})

const dayReports = computed(() => reportsByDate.value[selectedDate.value] || [])

async function run() {
  const current = ++seq
  error.value = null
  loading.value = true
  try {
    const result = (await listReports({ per_page: 100 })).items ?? []
    if (current === seq) reportsData.value = result
  } catch (err) {
    if (current === seq) error.value = isApiError(err) ? err : new ApiError('请求失败', 'unknown')
  } finally {
    if (current === seq) loading.value = false
  }
}

function selectDate(date: string) {
  selectedDate.value = date
}

function shiftDate(delta: number) {
  const base = selectedDate.value ? new Date(`${selectedDate.value}T00:00:00`) : new Date()
  selectedDate.value = localDate(new Date(base.getTime() + delta * 86400000))
}

function truncate(text: string | undefined, max = 120) {
  if (!text) return ''
  return text.length > max ? `${text.slice(0, max)}…` : text
}

/** 测试与重置场景（对齐 useAsyncData.reset 语义）：清空缓存回到初始态。
 *  独立导出：调用方无需进入 setup 作用域（useDashboardReports 内含 useI18n）。 */
export function resetDashboardReports() {
  seq++
  reportsData.value = null
  loading.value = false
  error.value = null
  selectedDate.value = yesterdayDate()
}

export function useDashboardReports() {
  const { t, locale } = useI18n()

  // 日期显示跟随界面语言（zh → zh-CN，en → en-US）
  const dateLocale = computed(() => (locale.value === 'zh' ? 'zh-CN' : 'en-US'))

  // 最近 7 天（今天往前），摘要拼接后截断 200 字
  const briefDays = computed(() =>
    Array.from({ length: 7 }, (_, i) => {
      const date = localDate(new Date(Date.now() - i * 86400000))
      const dayReports = reportsByDate.value[date] || []
      return {
        date,
        reports: dayReports,
        summary: truncate(dayReports.map((r) => r.summary).filter(Boolean).join(t('common.listSeparator')), 200)
      }
    })
  )

  function briefDayLabel(date: string) {
    const now = Date.now()
    if (date === localDate(new Date(now))) return t('dashboard.today')
    if (date === localDate(new Date(now - 86400000))) return t('dashboard.yesterday')
    return date.slice(5) // MM-DD
  }

  function weekdayLabel(date: string) {
    return new Date(`${date}T00:00:00`).toLocaleDateString(dateLocale.value, { weekday: 'short' })
  }

  // 面板首挂载触发一次取数（两面板同访仅一次）；全部卸载后再次进入重新取数
  onMounted(() => {
    activeMounts += 1
    if (activeMounts === 1) void run()
  })

  onBeforeUnmount(() => {
    activeMounts = Math.max(0, activeMounts - 1)
    if (activeMounts === 0) resetDashboardReports()
  })

  return {
    reportsData,
    loading,
    error,
    run,
    reset: resetDashboardReports,
    selectedDate,
    dayReports,
    briefDays,
    briefDayLabel,
    weekdayLabel,
    selectDate,
    shiftDate
  }
}
