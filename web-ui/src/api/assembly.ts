import { request } from './client'

export type AssemblyStep = {
  id: string
  project_id: string
  name: string
  description?: string
  depends_on?: string
  status: string
  assigned_to?: string
  step_order: number
  started_at?: string
  completed_at?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export type AssemblyStepPayload = {
  name?: string
  description?: string
  depends_on?: string
  assigned_to?: string
  step_order?: number
}

export type ReorderPayload = {
  project_id: string
  steps: { id: string; step_order: number }[]
}

export function createAssemblyStep(projectId: string, data: AssemblyStepPayload) {
  return request<AssemblyStep>({ url: `/projects/${projectId}/assembly`, method: 'POST', data })
}

export function listAssemblySteps(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: AssemblyStep[]; total: number; page: number }>({ url: `/projects/${projectId}/assembly`, params })
}

export function getAssemblyStep(id: string) {
  return request<AssemblyStep>({ url: `/assembly/${id}` })
}

export function updateAssemblyStep(id: string, data: AssemblyStepPayload) {
  return request<AssemblyStep>({ url: `/assembly/${id}`, method: 'PATCH', data })
}

// transition 与元数据字段互斥；override_reason 仅在 transition 时可用
export function transitionAssemblyStep(id: string, transition: string, override_reason = '') {
  return request<AssemblyStep>({
    url: `/assembly/${id}`,
    method: 'PATCH',
    data: override_reason ? { transition, override_reason } : { transition }
  })
}

export function reorderAssemblySteps(data: ReorderPayload) {
  return request<ReorderPayload>({ url: '/assembly/reorder', method: 'POST', data })
}

export function deleteAssemblyStep(id: string) {
  return request<{ id: string }>({ url: `/assembly/${id}`, method: 'DELETE' })
}
