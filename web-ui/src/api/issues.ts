import { request } from './client'
import type { Comment as CommentBase, Issue as IssueBase } from './types.issues.gen'

type IssueCore = Pick<IssueBase, 'id' | 'project_id' | 'title' | 'description' | 'status' | 'severity' | 'author_id'>
export type Issue = IssueCore &
  Partial<Omit<IssueBase, keyof IssueCore>> & {
    ai_generated?: boolean
    created_at?: string
    comments?: Comment[]
  }

export type Comment = CommentBase

export function listIssues(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: Issue[]; total: number; page: number }>({ url: `/projects/${projectId}/issues`, params })
}

export function listIssuesByLog(logId: string, params: Record<string, string | number> = {}) {
  return request<{ items: Issue[]; total: number; page: number }>({ url: '/issues', params: { related_log_id: logId, ...params } })
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
