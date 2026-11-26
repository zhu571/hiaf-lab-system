import { api, request } from './client'

export type AssistantSession = {
  id: string
  user_id: string
  project_id?: string
  title: string
  status: 'active' | 'archived' | string
  summary: string
  last_message_at?: string
  created_at: string
  updated_at: string
}

export type AssistantMessage = {
  id: string
  session_id: string
  run_id?: string
  user_id?: string
  role: 'user' | 'assistant' | 'tool' | string
  content: string
  content_json?: Record<string, unknown>
  created_at: string
}

export type AssistantRun = {
  id: string
  session_id: string
  user_id: string
  status: string
  model: string
  prompt_version: string
  input_tokens: number
  output_tokens: number
  error?: string
  started_at: string
  finished_at?: string
}

export type AssistantRunResult = {
  run: AssistantRun
  messages: AssistantMessage[]
}

export type AssistantUsage = {
  date: string
  runs: number
  input_tokens: number
  output_tokens: number
  tool_calls: number
  run_limit: number
}

export function listAssistantSessions(params: { status?: string; limit?: number; q?: string } = {}) {
  return request<{ items: AssistantSession[] }>({ url: '/assistant/sessions', params })
}

export function createAssistantSession(title = '', projectId = '') {
  return request<AssistantSession>({
    url: '/assistant/sessions',
    method: 'POST',
    data: { title, project_id: projectId }
  })
}

export function updateAssistantSession(id: string, data: { title?: string; status?: string }) {
  return request<AssistantSession>({ url: `/assistant/sessions/${id}`, method: 'PATCH', data })
}

export function archiveAssistantSession(id: string) {
  return request<{ archived: boolean }>({ url: `/assistant/sessions/${id}`, method: 'DELETE' })
}

export function listAssistantMessages(id: string) {
  return request<{ items: AssistantMessage[] }>({ url: `/assistant/sessions/${id}/messages` })
}

export function addAssistantMessage(id: string, content: string) {
  return request<AssistantMessage>({ url: `/assistant/sessions/${id}/messages`, method: 'POST', data: { content } })
}

export function startAssistantRun(id: string, content: string) {
  return request<AssistantRunResult>({ url: `/assistant/sessions/${id}/runs`, method: 'POST', data: { content } })
}

export function getAssistantUsage() {
  return request<AssistantUsage>({ url: '/assistant/usage' })
}

export async function exportAssistantSession(id: string) {
  const response = await api.get(`/assistant/sessions/${id}/export`, { responseType: 'text' })
  return String(response.data)
}
