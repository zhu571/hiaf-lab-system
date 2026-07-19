import { request } from './client'

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
