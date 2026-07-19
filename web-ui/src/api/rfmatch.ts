import { request } from './client'

export type RFMatchingRecord = {
  id: string
  project_id: string
  device: string
  frequency_mhz: number
  s11?: number
  input_freq?: number
  input_voltage?: number
  input_power?: number
  input_desc?: string
  output_freq?: number
  output_voltage?: number
  output_power?: number
  output_desc?: string
  transformer_turns?: string
  capacitance_text?: string
  transformer_material?: string
  shunt_inductance?: string
  series_capacitor?: string
  status?: string
  notes?: string
  measured_at?: string
  measured_by?: string
  is_void: boolean
  voided_at?: string
  voided_by?: string
  void_reason?: string
  created_at: string
  updated_at: string
}

// POST/PATCH 均开启 DisallowUnknownFields；PATCH 不可改 device/frequency_mhz/measured_at
export type RFMatchingPayload = {
  device?: string
  frequency_mhz?: number
  s11?: number
  input_freq?: number
  input_voltage?: number
  input_power?: number
  input_desc?: string
  output_freq?: number
  output_voltage?: number
  output_power?: number
  output_desc?: string
  transformer_turns?: string
  capacitance_text?: string
  transformer_material?: string
  shunt_inductance?: string
  series_capacitor?: string
  status?: string
  notes?: string
  measured_at?: string
}

export function createRFMatching(projectId: string, data: RFMatchingPayload) {
  return request<RFMatchingRecord>({ url: `/projects/${projectId}/rf-matching`, method: 'POST', data })
}

export function listRFMatching(projectId: string, params: Record<string, string | number> = {}) {
  return request<{ items: RFMatchingRecord[]; total: number; page: number }>({ url: `/projects/${projectId}/rf-matching`, params })
}

export function getRFMatching(id: string) {
  return request<RFMatchingRecord>({ url: `/rf-matching/${id}` })
}

export function updateRFMatching(id: string, data: RFMatchingPayload) {
  return request<RFMatchingRecord>({ url: `/rf-matching/${id}`, method: 'PATCH', data })
}

// DELETE 是标记 is_void，可带作废原因
export function deleteRFMatching(id: string, reason = '') {
  return request<{ id: string }>({ url: `/rf-matching/${id}`, method: 'DELETE', data: reason ? { reason } : undefined })
}
