import { request } from './client'

export type AlertLevel = 'info' | 'warning' | 'error' | 'critical'
export type AlertStatus = 'active' | 'resolved'

export type AlertRecord = {
  id: string
  level: AlertLevel
  source: string
  title: string
  detail: string
  status: AlertStatus
  occurrence_count: number
  first_seen: string
  last_seen: string
  resolved_at?: string | null
  resolved_by: string
  created_at: string
}

// GET /api/v1/alerts?status=active|resolved&limit=&offset=
export function listAlerts(params: Record<string, string | number> = {}) {
  return request<{ items: AlertRecord[]; total: number; limit: number; offset: number }>({ url: '/alerts', params })
}

// GET /api/v1/alerts/{id}
export function getAlert(id: string) {
  return request<AlertRecord>({ url: `/alerts/${id}` })
}

// POST /api/v1/alerts/resolve —— 前端手动 resolve（{id}，admin/maintainer），
// Idempotency-Key 与 CSRF 由 client 拦截器自动携带
export function resolveAlert(body: { id?: string; source?: string; title?: string }) {
  return request<{ resolved: boolean }>({ url: '/alerts/resolve', method: 'POST', data: body })
}
