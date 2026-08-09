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

// C8 候选全链路溯源（trace 端点）；raw_text_snapshot 对 030 迁移前的存量任务为 null（降级用 report 当前值）
export type CandidateTrace = {
  candidate: AgentCandidate
  task: {
    id: string
    status: string
    model?: string
    prompt_version?: string
    agent_confidence?: number
    raw_text_snapshot?: string | null
    raw_text_sha256?: string | null
    report_date?: string | null
  }
  report?: { id: string; report_date: string; raw_text: string } | null
  result?: { issue_id?: string; experience_id?: string; title: string; url: string } | null
  audit: {
    id: number
    request_id: string
    username: string
    method: string
    path: string
    action: string
    status_code: number
    actor_type: string
    agent_task_id?: string
    detail?: Record<string, unknown>
    created_at: string
  }[]
}

export function getCandidateTrace(id: string) {
  return request<CandidateTrace>({ url: `/agent/candidates/${id}/trace` })
}
