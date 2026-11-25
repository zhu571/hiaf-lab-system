import { request } from './client'
import type {
  AgentCandidateAction,
  CandidateTrace as CandidateTraceBase,
} from './types.agent.gen'

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

type AgentCandidateCore = Pick<AgentCandidateAction, 'id' | 'task_id' | 'action_type' | 'status' | 'created_at'>
export type AgentCandidate = AgentCandidateCore &
  Partial<Omit<AgentCandidateAction, keyof AgentCandidateCore>> & {
    payload: CandidatePayload
    prompt_version?: string
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

// C8 候选全链路溯源（trace 端点）；结构由 tygo 从 Go model 生成。
export type CandidateTrace = CandidateTraceBase

export function getCandidateTrace(id: string) {
  return request<CandidateTrace>({ url: `/agent/candidates/${id}/trace` })
}
