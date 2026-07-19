import { request } from './client'

export type DailyReport = {
  id: string
  report_date: string
  author_id: string
  author_name?: string
  raw_text: string
  summary: string
  content_status: string
  quality_status: string
  logs?: LogItem[]
}

export type LogItem = {
  id: string
  project_id: string
  author_id: string
  occurred_at: string
  category: string
  content: string
  source: string
  content_status: string
  created_at?: string
}

export function todayReport() {
  return request<DailyReport>({ url: '/daily-reports/today', method: 'POST', data: {} })
}

export function updateReportRawText(id: string, raw_text: string) {
  return request<DailyReport>({ url: `/daily-reports/${id}`, method: 'PATCH', data: { raw_text } })
}

export function submitReport(id: string, force = false) {
  return request<{ report: DailyReport; warnings: unknown[]; blocked: boolean }>({
    url: `/daily-reports/${id}/submit`,
    method: 'POST',
    data: { force }
  })
}

export function listReports(params: Record<string, string | number> = {}) {
  return request<{ items: DailyReport[]; total: number; page: number }>({ url: '/daily-reports', params: { per_page: 20, ...params } })
}

export function getReport(id: string) {
  return request<DailyReport>({ url: `/daily-reports/${id}` })
}

export function reportByDate(date: string) {
  return request<DailyReport>({ url: '/daily-reports/by-date', params: { date } })
}

export function createLog(projectId: string, data: { daily_report_id?: string; category: string; content: string; occurred_at?: string }) {
  return request<LogItem>({ url: `/projects/${projectId}/logs`, method: 'POST', data })
}

export function updateLog(id: string, data: { category?: string; content?: string; content_status?: 'confirmed' }) {
  return request<LogItem>({ url: `/logs/${id}`, method: 'PATCH', data })
}

export function listProjectLogs(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: LogItem[]; total: number; page: number }>({ url: `/projects/${projectId}/logs`, params })
}
