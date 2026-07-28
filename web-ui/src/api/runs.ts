import { request, requestWithMeta } from './client'

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
  return request<ExperimentRun>({ url: `/projects/${projectId}/experiment-runs`, method: 'POST', data })
}

export function listRuns(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: ExperimentRun[]; total: number; page: number }>({
    url: `/projects/${projectId}/experiment-runs`,
    params
  })
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
  return request<ReportLinkResult>({ url: `/experiment-runs/${runId}/daily-reports/${reportId}`, method: 'POST' })
}

export function removeReportLink(runId: string, reportId: string) {
  return request<ReportLinkResult>({ url: `/experiment-runs/${runId}/daily-reports/${reportId}`, method: 'DELETE' })
}

// ---- 实验步骤（runs 模块步骤 API）----

export interface RunStep {
  id: string
  run_id: string
  name: string
  description?: string
  depends_on?: string
  status: string
  step_order: number
  started_at?: string
  completed_at?: string
  source_template_id?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export type StepTransition = 'start' | 'pause' | 'resume' | 'complete' | 'skip' | 'cancel'

// 后端返回 { items, total } 包装结构
export function listRunSteps(runId: string) {
  return request<{ items: RunStep[]; total: number }>({ url: `/experiment-runs/${runId}/steps` })
}

// step_order 不传（0）时由服务端自动取 max+1
export function createRunStep(runId: string, data: { name: string; description?: string; step_order?: number; depends_on?: string }) {
  return requestWithMeta<RunStep>({ url: `/experiment-runs/${runId}/steps`, method: 'POST', data })
}

export function applyRunTemplate(
  runId: string,
  payload: {
    template_id?: string
    steps?: { name: string; description?: string; step_order: number; depends_on_order?: number | null }[]
    source_prompt?: string
  }
) {
  return requestWithMeta<RunStep[]>({ url: `/experiment-runs/${runId}/steps/apply-template`, method: 'POST', data: payload })
}

export function updateRunStep(id: string, data: { name?: string; description?: string; transition?: string }) {
  return requestWithMeta<RunStep>({ url: `/run-steps/${id}`, method: 'PATCH', data })
}

export function deleteRunStep(id: string) {
  return requestWithMeta<{ id: string }>({ url: `/run-steps/${id}`, method: 'DELETE' })
}

export function reorderRunSteps(runId: string, steps: { id: string; step_order: number }[]) {
  return requestWithMeta<unknown>({ url: `/run-steps/reorder`, method: 'POST', data: { run_id: runId, steps } })
}
