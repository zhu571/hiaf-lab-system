import { request } from './client'

export type TodoPriority = 'high' | 'medium' | 'low'
export type TodoStatus = 'pending' | 'done' | 'deferred' | 'cancelled'

export type Todo = {
  id: string
  title: string
  priority: TodoPriority
  status: TodoStatus
  source: string
  created_by: string
  created_for: string
  project_id?: string | null
  issue_id?: string | null
  completed_at?: string | null
  completed_by?: string | null
  created_at: string
  updated_at: string
  owner_display_name: string
}

export type LLMDraft = {
  status: 'ok' | 'rejected'
  title: string
  priority: TodoPriority
  reason?: string | null
}

export type TodoListParams = {
  date?: string
  scope?: 'all' | 'mine' | 'shared'
  status?: 'open' | 'done' | 'cancelled' | 'all'
}

export function listTodos(params: TodoListParams = {}) {
  return request<Todo[]>({
    url: '/todos',
    params: { date: params.date || undefined, scope: params.scope, status: params.status, limit: 100 }
  })
}

export function createTodo(data: { title: string; priority?: TodoPriority; project_id?: string | null }) {
  return request<Todo>({ url: '/todos', method: 'POST', data })
}

export function llmParse(rawText: string) {
  return request<LLMDraft>({ url: '/todos/llm-parse', method: 'POST', data: { raw_text: rawText } })
}

export function llmAdd(data: { title: string; priority: TodoPriority }) {
  return request<Todo>({ url: '/todos/llm-add', method: 'POST', data })
}

export function doneTodo(id: string) {
  return request<Todo>({ url: `/todos/${id}/done`, method: 'PATCH', data: {} })
}

export function deferTodo(id: string) {
  return request<Todo>({ url: `/todos/${id}/defer`, method: 'PATCH', data: {} })
}

export function updateTodo(id: string, data: { updated_at: string; title?: string; priority?: TodoPriority; project_id?: string | null }) {
  return request<Todo>({ url: `/todos/${id}`, method: 'PATCH', data })
}

export function deleteTodo(id: string) {
  return request<{ id: string }>({ url: `/todos/${id}`, method: 'DELETE' })
}

export function getNotificationTopic() {
  return request<{ topic: string; subscribe_url: string }>({ url: '/todos/notification-topic' })
}

export function provisionTopic() {
  return request<{ provision_token: string; expires_at: string }>({
    url: '/todos/notification-topic/provision',
    method: 'POST',
    data: {}
  })
}

export function redeemTopic(provisionToken: string) {
  return request<{ username: string; password: string; topic: string }>({
    url: '/todos/notification-topic/redeem',
    method: 'POST',
    data: { provision_token: provisionToken }
  })
}
