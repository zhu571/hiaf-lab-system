import { request } from './client'

export type AuditRecord = {
  id: number
  request_id: string
  user_id?: string
  username: string
  method: string
  path: string
  action: string
  status_code: number
  client_ip: string
  actor_type?: string
  acting_user_id?: string
  agent_task_id?: string
  detail?: Record<string, unknown>
  created_at: string
}

export function getAudit(requestId: string) {
  return request<{ items: AuditRecord[]; total: number }>({ url: `/audit/${requestId}` })
}

// C12 审计事件列表（消费 C7 的 /api/v1/audit/events）：?action=&user_id=&actor_type=&from=&to=&page=&per_page=
export function listAuditEvents(params: Record<string, string | number> = {}) {
  return request<{ items: AuditRecord[]; total: number; page: number; per_page: number }>({ url: '/audit/events', params })
}
