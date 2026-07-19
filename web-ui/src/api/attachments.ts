import { api, newIdempotencyKey, request } from './client'

export type Attachment = {
  id: string
  original_name: string
  sha256: string
  description: string
  mime_type: string
  file_size: number
  uploaded_by?: string
  created_at: string
  updated_at: string
}

export type AttachmentLink = {
  id: string
  attachment_id: string
  entity_type: string
  entity_id: string
  description: string
  created_by?: string
  created_at: string
}

export type UploadResult = {
  attachment: Attachment
  links?: AttachmentLink[]
}

export const ATTACHMENT_ENTITY_TYPES = ['assembly_step', 'daily_report', 'issue', 'log', 'test_data', 'experiment_run', 'rf_matching_record']

// multipart 上传，entity_type/entity_id 必须成对
export function uploadAttachment(file: File, entityType = '', entityId = '', description = '') {
  const form = new FormData()
  form.append('file', file)
  if (entityType && entityId) {
    form.append('entity_type', entityType)
    form.append('entity_id', entityId)
  }
  if (description) form.append('description', description)
  return request<UploadResult>({ url: '/attachments', method: 'POST', data: form })
}

export function listAttachments(params: Record<string, string | number> = {}) {
  return request<{ items: Attachment[]; total: number; page: number }>({ url: '/attachments', params })
}

export function getAttachment(id: string) {
  return request<Attachment>({ url: `/attachments/${id}` })
}

// 下载也强制要求 Idempotency-Key，需要手工带 header（拦截器只对写方法自动加）
export async function getAttachmentContent(id: string) {
  const response = await api.request<Blob>({
    url: `/attachments/${id}/content`,
    responseType: 'blob',
    headers: { 'Idempotency-Key': newIdempotencyKey() }
  })
  return response.data
}

export function addAttachmentLink(id: string, data: { entity_type: string; entity_id: string; description?: string }) {
  return request<AttachmentLink>({ url: `/attachments/${id}/links`, method: 'POST', data })
}

export function removeAttachmentLink(attachmentId: string, linkId: string) {
  return request<{ attachment_id: string; link_id: string }>({ url: `/attachments/${attachmentId}/links/${linkId}`, method: 'DELETE' })
}

export function deleteAttachment(id: string) {
  return request<{ id: string }>({ url: `/attachments/${id}`, method: 'DELETE' })
}
