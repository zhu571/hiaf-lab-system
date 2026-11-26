import { request } from './client'

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
  role: 'user' | 'assistant' | 'tool' | string
  content: string
  content_json?: Record<string, unknown>
  created_at: string
}

export function listAssistantSessions(params: { status?: string; limit?: number } = {}) {
  return request<{ items: AssistantSession[] }>({ url: '/assistant/sessions', params })
}

export function createAssistantSession(title = '') {
  return request<AssistantSession>({ url: '/assistant/sessions', method: 'POST', data: { title } })
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
