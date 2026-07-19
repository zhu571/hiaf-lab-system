import { request } from './client'

export type Experience = {
  id: string
  project_id?: string
  title: string
  content: string
  tags: string[]
  status: string
  author_id: string
}

export function listExperiences(params: Record<string, string | number> = {}) {
  return request<{ items: Experience[]; total: number; page: number; per_page: number }>({ url: '/experiences', params })
}

export function createExperience(data: Partial<Experience>) {
  return request<Experience>({ url: '/experiences', method: 'POST', data })
}

export function publishExperience(id: string) {
  return request<Experience>({ url: `/experiences/${id}/publish`, method: 'POST', data: {} })
}

export function archiveExperience(id: string) {
  return request<Experience>({ url: `/experiences/${id}/archive`, method: 'POST', data: {} })
}
