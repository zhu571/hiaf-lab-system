import { request } from './client'

export type CandidatePayload = {
  title?: string
  description?: string
  severity?: string
  is_duplicate?: boolean
  duplicate_issue_id?: string | null
  issue_id?: string
  content?: string
  [key: string]: unknown
}

export type AgentCandidate = {
  id: string
  task_id: string
  report_id?: string
  action_type: string
  project_id?: string
  payload: CandidatePayload
  status: string
  agent_confidence?: number
  reviewed_by?: string
  reviewed_at?: string
  review_reason?: string
  executed_at?: string
  execution_error?: string
  prompt_version?: string
  created_at: string
}

export function listAgentCandidates(params: Record<string, string | number> = {}) {
  return request<{ items: AgentCandidate[]; total: number; page: number; per_page: number }>({ url: '/agent/candidates', params })
}

export function approveCandidate(id: string) {
  return request<AgentCandidate>({ url: `/agent/candidates/${id}/approve`, method: 'POST' })
}

export function rejectCandidate(id: string, reason: string) {
  return request<AgentCandidate>({ url: `/agent/candidates/${id}/reject`, method: 'POST', data: { reason } })
}
