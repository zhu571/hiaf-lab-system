import { request, requestWithMeta } from './client'
import type { FieldTranslations } from '@/utils/contentLanguage'

export type TranslationSidecar = Record<string, FieldTranslations>
export type TranslationRequest = { field: string; target_locale: 'zh' | 'en'; force?: boolean; translated_text?: string }

export type DailyReport = {
  id: string
  report_date: string
  author_id: string
  author_name?: string
	log_count?: number
  raw_text: string
  summary: string
  content_status: string
  quality_status: string
  logs?: LogItem[]
  translations?: TranslationSidecar
}

export type DailyReportRef = Pick<DailyReport, 'id' | 'report_date' | 'author_id' | 'author_name' | 'content_status' | 'quality_status'> & {
  can_read_detail: boolean
}

export type TeamDailyReportProjection = Pick<DailyReport, 'id' | 'report_date' | 'author_id' | 'author_name' | 'content_status' | 'quality_status'> & {
  project_log_count: number
  logs?: LogItem[]
}

export type LogItem = {
  id: string
  project_id: string
	project_code?: string
	project_name?: string
  author_id: string
	author_name?: string
  occurred_at: string
  category: string
  content: string
  raw_snippet?: string
  source: string
  content_status: string
  created_at?: string
  translations?: TranslationSidecar
}

export function todayReport() {
  return request<DailyReport>({ url: '/daily-reports/today', method: 'POST', data: {} })
}

export function updateReport(id: string, data: { raw_text?: string; summary?: string }) {
  return request<DailyReport>({ url: `/daily-reports/${id}`, method: 'PATCH', data })
}

export function updateReportRawText(id: string, raw_text: string) {
  return updateReport(id, { raw_text })
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

export function listTeamReports(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: TeamDailyReportProjection[]; total: number; page: number }>({ url: `/projects/${projectId}/daily-reports`, params })
}

export function getTeamReport(projectId: string, reportId: string) {
  return request<TeamDailyReportProjection>({ url: `/projects/${projectId}/daily-reports/${reportId}` })
}

export function getReport(id: string) {
  return request<DailyReport>({ url: `/daily-reports/${id}` })
}
export function requestReportTranslation(id: string, data: TranslationRequest) { return request<unknown>({ url: `/daily-reports/${id}/translations`, method: 'POST', data }) }
export function saveReportTranslation(id: string, data: TranslationRequest) { return request<unknown>({ url: `/daily-reports/${id}/translations`, method: 'PATCH', data }) }
export function requestLogTranslation(id: string, data: TranslationRequest) { return request<unknown>({ url: `/logs/${id}/translations`, method: 'POST', data }) }
export function saveLogTranslation(id: string, data: TranslationRequest) { return request<unknown>({ url: `/logs/${id}/translations`, method: 'PATCH', data }) }

export function reportByDate(date: string) {
  return request<DailyReport>({ url: '/daily-reports/by-date', params: { date } })
}

export function createLog(projectId: string, data: { daily_report_id?: string; category: string; content: string; raw_snippet?: string; occurred_at?: string; source?: string }) {
  return request<LogItem>({ url: `/projects/${projectId}/logs`, method: 'POST', data })
}

// AI 整理日报为结构化日志草稿（不落库）。写方法 → axios 拦截器自动生成一次性 Idempotency-Key。
export type AiParseLogEntry = {
  category: string
  project_id: string
  content: string
  raw_snippet: string
  occurred_at: string
}

export type AiParseResult = {
  status: 'ok' | 'clarify' | 'rejected'
  logs: AiParseLogEntry[]
  summary: string | null
  question: string | null
  reason: string | null
  model?: string
  prompt_version?: string
}

export function aiParseReport(reportId: string) {
  return requestWithMeta<AiParseResult>({ url: `/daily-reports/${reportId}/ai-parse`, method: 'POST', data: {} })
}

export function updateLog(id: string, data: { category?: string; content?: string; content_status?: 'confirmed' }) {
  return request<LogItem>({ url: `/logs/${id}`, method: 'PATCH', data })
}

export function listProjectLogs(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: LogItem[]; total: number; page: number }>({ url: `/projects/${projectId}/logs`, params })
}

export function listMyLogs(params: Record<string, string | number> = {}) {
  return request<{ items: LogItem[]; total: number; page: number }>({ url: '/logs/mine', params })
}

export function getLog(id: string) {
	return request<LogItem>({ url: `/logs/${id}` })
}

export function listReportsByLog(id: string) {
  return request<DailyReportRef[]>({ url: `/logs/${id}/daily-reports` })
}
