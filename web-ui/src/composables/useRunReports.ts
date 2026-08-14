import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { i18n } from '@/i18n'
import { showApiError } from '@/composables/useNotify'
import { listReports, type DailyReport } from '@/api/logs'
import { addReportLink as linkRunReport, removeReportLink as unlinkRunReport } from '@/api/runs'

// 批次详情「关联日报」面板（重构方案 S5：从 RunDetailView 616 行 script 拆出）。
// 行为与拆分前逐字等价：日报候选列表 + 关联/解绑（响应的 report_ids 为全量列表，直接覆盖本地状态）。

export function useRunReports(runId: string) {
  const t = (key: string, params?: Record<string, unknown>) =>
    (params ? i18n.global.t(key, params) : i18n.global.t(key)) as string

  const reportOptions = ref<DailyReport[]>([])
  const reportsLoading = ref(false)
  const selectedReportId = ref('')
  const linkedReportIds = ref<string[]>([])
  const linking = ref(false)

  async function loadReports() {
    reportsLoading.value = true
    try {
      const data = await listReports({ per_page: 50 })
      reportOptions.value = data.items ?? []
    } catch (err) {
      showApiError(err, t('runDetail.reportsLoadFailed'))
    } finally {
      reportsLoading.value = false
    }
  }

  async function link() {
    if (!selectedReportId.value) return
    linking.value = true
    try {
      const res = await linkRunReport(runId, selectedReportId.value)
      // 响应的 report_ids 是全量列表，直接覆盖本地状态
      linkedReportIds.value = res.report_ids
      selectedReportId.value = ''
      ElMessage.success(t('runDetail.reportLinked'))
    } catch (err) {
      showApiError(err, t('runDetail.linkFailed'))
    } finally {
      linking.value = false
    }
  }

  async function unlink(reportId: string) {
    try {
      const res = await unlinkRunReport(runId, reportId)
      linkedReportIds.value = res.report_ids
      ElMessage.success(t('runDetail.unlinked'))
    } catch (err) {
      showApiError(err, t('runDetail.unlinkFailed'))
    }
  }

  function reportLabel(r: DailyReport) {
    const summary = (r.summary || '').trim()
    const short = summary.length > 24 ? `${summary.slice(0, 24)}…` : summary
    return short ? `${r.report_date} · ${short}` : r.report_date
  }

  return {
    reportOptions,
    reportsLoading,
    selectedReportId,
    linkedReportIds,
    linking,
    loadReports,
    link,
    unlink,
    reportLabel
  }
}
