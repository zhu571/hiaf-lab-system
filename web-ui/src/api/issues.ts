import { request } from './client'

export type Issue = {
  id: string
  project_id: string
  title: string
  description: string
  status: string
  severity: string
  ai_generated?: boolean
  author_id: string
  created_at?: string
  comments?: Comment[]
}

export type Comment = {
  id: string
  issue_id: string
  author_id: string
  content: string
  created_at: string
}

export function listIssues(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: Issue[]; total: number; page: number }>({ url: `/projects/${projectId}/issues`, params })
}

export const listProjectIssues = listIssues

export function getIssue(id: string) {
  return request<Issue>({ url: `/issues/${id}` })
}

export function createIssue(projectId: string, data: Partial<Issue>) {
  return request<Issue>({ url: `/projects/${projectId}/issues`, method: 'POST', data })
}

export function transitionIssue(id: string, target_status: string, reason = '') {
  return request<Issue>({ url: `/issues/${id}/transition`, method: 'POST', data: { target_status, reason, add_comment: Boolean(reason) } })
}

export function addIssueComment(id: string, content: string) {
  return request<Comment>({ url: `/issues/${id}/comments`, method: 'POST', data: { content } })
}
