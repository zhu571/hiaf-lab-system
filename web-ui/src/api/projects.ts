import { request, requestWithMeta } from './client'
import type { Project as ProjectBase, ProjectMember as ProjectMemberBase, ProjectWithStats } from './types.projects.gen'

// tygo 不展开 Go 嵌入字段：核心字段保持必填，其余生成字段（含 ProjectWithStats）可选，
// 兼容旧 Project 形状与测试里的最小 fixture。
type ProjectCore = Pick<ProjectBase, 'id' | 'code' | 'name' | 'short_name' | 'description' | 'status' | 'visibility'>
export type Project = ProjectCore & Partial<Omit<ProjectBase, keyof ProjectCore>> & Partial<ProjectWithStats>
export type ProjectMember = ProjectMemberBase & { username?: string }

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
