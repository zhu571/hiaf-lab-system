import { request } from './client'

export type AuditRecord = {
  id: number
  request_id: string
  username: string
  method: string
  path: string
  action: string
  status_code: number
  client_ip: string
  created_at: string
}

export function getAudit(requestId: string) {
  return request<{ items: AuditRecord[]; total: number }>({ url: `/audit/${requestId}` })
}
