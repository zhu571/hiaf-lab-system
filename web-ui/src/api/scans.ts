import { api, newIdempotencyKey, request } from './client'

export type ScanItem = {
  id: string
  project_id: string
  machine: string
  filename: string
  source_tag: string
  tags: string[]
  scan_at?: string
  scan_at_source: string
  format: string
  columns: string[]
  row_count: number
  bad_rows: number
  parse_warning?: string
  txt_sha256: string
  png_sha256?: string
  txt_size: number
  png_size?: number
  txt_path: string
  png_path?: string
  thumb_path?: string
  source_path?: string
  title: string
  description: string
  annotated_at?: string
  ingest_source: string
  is_void: boolean
  voided_at?: string
  voided_by?: string
  void_reason?: string
  uploaded_by?: string
  created_at: string
  updated_at: string
  duplicate?: boolean
}

export type ScanList = { items: ScanItem[]; total: number; page: number; per_page: number }
export type ScanTag = { tag: string; count: number }

export function listScans(projectId: string, params: Record<string, string | number>) {
  return request<ScanList>({ url: `/projects/${projectId}/scans`, method: 'GET', params })
}

export function listScanTags(projectId: string) {
  return request<{ items: ScanTag[] }>({ url: `/projects/${projectId}/scans/tags`, method: 'GET' })
}

export function getScan(id: string) {
  return request<ScanItem>({ url: `/scans/${id}`, method: 'GET' })
}

export function uploadScan(projectId: string, form: FormData) {
  return request<ScanItem>({ url: `/projects/${projectId}/scans`, method: 'POST', data: form })
}

export function updateScan(id: string, data: { title?: string; description?: string; tags?: string[] }) {
  return request<ScanItem>({ url: `/scans/${id}`, method: 'PATCH', data })
}

export function voidScan(id: string, reason: string) {
  return request<ScanItem>({ url: `/scans/${id}/void`, method: 'POST', data: { reason } })
}

export function scanFileUrl(id: string, kind: 'txt' | 'png') {
  return `/api/v1/scans/${id}/file/${kind}`
}

export function scanThumbUrl(id: string) {
  return `/api/v1/scans/${id}/thumb`
}

export async function downloadScanFile(id: string, kind: 'txt' | 'png', filename: string) {
  const response = await api.get(`/scans/${id}/file/${kind}`, {
    params: { download: '1' },
    responseType: 'blob',
    headers: { 'Idempotency-Key': newIdempotencyKey() },
  })
  const url = URL.createObjectURL(response.data as Blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}
