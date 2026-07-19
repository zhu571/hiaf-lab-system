import { request } from './client'

export type TestData = {
  id: string
  project_id: string
  run_id?: string
  data_type: string
  measurement: string
  value: number
  unit: string
  quality: string
  source: string
  measured_at?: string
  notes?: string
  created_at: string
  updated_at: string
  recorded_by?: string
}

// POST/PATCH 均开启 DisallowUnknownFields，字段必须与后端白名单一致
export type TestDataPayload = {
  data_type?: string
  measurement?: string
  value?: number
  run_id?: string
  unit?: string
  quality?: string
  source?: string
  measured_at?: string
  notes?: string
}

export function createTestData(projectId: string, data: TestDataPayload) {
  return request<TestData>({ url: `/projects/${projectId}/test-data`, method: 'POST', data })
}

export function listTestData(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: TestData[]; total: number; page: number }>({ url: `/projects/${projectId}/test-data`, params })
}

export function getTestData(id: string) {
  return request<TestData>({ url: `/test-data/${id}` })
}

export function updateTestData(id: string, data: TestDataPayload) {
  return request<TestData>({ url: `/test-data/${id}`, method: 'PATCH', data })
}

// DELETE 是标记 quality=invalid，不是硬删除
export function deleteTestData(id: string) {
  return request<{ id: string }>({ url: `/test-data/${id}`, method: 'DELETE' })
}
