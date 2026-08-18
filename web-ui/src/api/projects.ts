import { request, requestWithMeta } from './client'

export type Project = {
  id: string
  code: string
  name: string
  short_name: string
  description: string
  status: string
  visibility: string
  member_count?: number
  open_issue_count?: number
  log_count?: number
  tags?: string[]
}

export type ProjectMember = {
  project_id: string
  user_id: string
  username?: string
  role: string
  status: string
  muted: boolean
  joined_at: string
  added_by: string
}

export function listProjects(status = '') {
  return request<Project[]>({ url: '/projects', params: { status } })
}

export function createProject(data: Partial<Project>) {
  return request<Project>({ url: '/projects', method: 'POST', data })
}

export function updateProject(id: string, data: Partial<Project>) {
  return request<Project>({ url: `/projects/${id}`, method: 'PATCH', data })
}

export function transitionProject(id: string, data: { action: string; ignore_warnings?: boolean; reason?: string }) {
  return request<Project>({ url: `/projects/${id}/transition`, method: 'POST', data })
}

export function listMembers(id: string) {
  return request<ProjectMember[]>({ url: `/projects/${id}/members` })
}

export const getMembers = listMembers

export function addMember(projectId: string, data: { user_id: string; role: string }) {
  return requestWithMeta<ProjectMember>({ url: `/projects/${projectId}/members`, method: 'POST', data })
}

export function updateMemberRole(projectId: string, userId: string, role: string) {
  return requestWithMeta<ProjectMember>({ url: `/projects/${projectId}/members/${userId}`, method: 'PATCH', data: { role } })
}

export function removeMember(projectId: string, userId: string) {
  return requestWithMeta<{ success: boolean }>({ url: `/projects/${projectId}/members/${userId}`, method: 'DELETE' })
}
