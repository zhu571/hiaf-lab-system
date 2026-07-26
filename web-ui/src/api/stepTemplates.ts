import { request } from './client'

export interface StepTemplate {
  id: string
  name: string
  kind: 'assembly' | 'experiment'
  description?: string
  source_prompt?: string
  ai_generated: boolean
  created_by?: string
  created_at: string
  updated_at: string
  items?: StepTemplateItem[]
  // 列表接口不返回步骤数，由前端按需补全
  _item_count?: number
}

export interface StepTemplateItem {
  id?: string
  name: string
  description?: string
  step_order: number
  depends_on_order?: number | null
}

export interface GenerateCandidate {
  status: 'ok' | 'clarify' | 'rejected'
  name_suggestion?: string
  steps?: StepTemplateItem[]
  question?: string | null
  reason?: string | null
}

export function generateSteps(kind: string, prompt: string) {
  return request<GenerateCandidate>({ method: 'POST', url: '/step-templates/generate', data: { kind, prompt } })
}

export function listTemplates(params?: { kind?: string; q?: string; page?: number; per_page?: number }) {
  return request<{ items: StepTemplate[]; total: number; page: number; per_page: number }>({ url: '/step-templates', params })
}

export function getTemplate(id: string) {
  return request<StepTemplate>({ url: `/step-templates/${id}` })
}

export function createTemplate(data: {
  name: string
  kind: string
  description?: string
  items: StepTemplateItem[]
  source_prompt?: string
  ai_generated?: boolean
  apply_to_project_id?: string
}) {
  return request<StepTemplate>({ method: 'POST', url: '/step-templates', data })
}

export function updateTemplate(id: string, data: { name?: string; description?: string }) {
  return request<StepTemplate>({ method: 'PATCH', url: `/step-templates/${id}`, data })
}

export function replaceTemplateItems(id: string, items: StepTemplateItem[]) {
  return request<{ id: string }>({ method: 'PATCH', url: `/step-templates/${id}/items`, data: { items } })
}

export function deleteTemplate(id: string) {
  return request<{ id: string }>({ method: 'DELETE', url: `/step-templates/${id}` })
}
