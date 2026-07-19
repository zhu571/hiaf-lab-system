import { request } from './client'

export type ExperimentRun = {
  id: string
  project_id: string
  name: string
  campaign?: string
  run_type: string
  status: string
  gas_type: string
  target_temp?: number
  min_temp?: number
  pressure_min?: number
  pressure_max?: number
  pressure_unit: string
  has_beam: boolean
  devices?: string[]
  started_at?: string
  ended_at?: string
  description?: string
  created_at: string
  updated_at: string
  created_by?: string
}

export type RunPayload = {
  name?: string
  campaign?: string
  run_type?: string
  gas_type?: string
  target_temp?: number
  min_temp?: number
  pressure_min?: number
  pressure_max?: number
  pressure_unit?: string
  has_beam?: boolean
  devices?: string[]
  description?: string
}

export type ReportLinkResult = {
  run_id: string
  report_ids: string[]
}

export function createRun(projectId: string, data: RunPayload) {
  return request<ExperimentRun>({ url: `/projects/${projectId}/runs`, method: 'POST', data })
}

export function listRuns(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: ExperimentRun[]; total: number; page: number }>({ url: `/projects/${projectId}/runs`, params })
}

export function getRun(id: string) {
  return request<ExperimentRun>({ url: `/experiment-runs/${id}` })
}

export function updateRun(id: string, data: RunPayload) {
  return request<ExperimentRun>({ url: `/experiment-runs/${id}`, method: 'PATCH', data })
}

// transition 与元数据字段互斥，单独提交
export function transitionRun(id: string, transition: string) {
  return request<ExperimentRun>({ url: `/experiment-runs/${id}`, method: 'PATCH', data: { transition } })
}

export function deleteRun(id: string) {
  return request<{ id: string }>({ url: `/experiment-runs/${id}`, method: 'DELETE' })
}

export function addReportLink(runId: string, reportId: string) {
  return request<ReportLinkResult>({ url: `/experiment-runs/${runId}/reports/${reportId}`, method: 'POST' })
}

export function removeReportLink(runId: string, reportId: string) {
  return request<ReportLinkResult>({ url: `/experiment-runs/${runId}/reports/${reportId}`, method: 'DELETE' })
}
