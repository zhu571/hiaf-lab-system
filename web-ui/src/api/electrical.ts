import { request, requestWithMeta } from './client'

export type ElectricalSample = {
  time: string
  source: string
  channel: string
  metric: string
  unit?: string
  mapping: 'direct' | 'unmapped' | 'request_response'
  signature?: string
  value?: number
  text_value?: string
  raw?: string
  quality: string
  seq: number
}

export type ElectricalSourceHealth = {
  source: string
  mode: 'passive' | 'modbus_active' | 'scpi'
  status: 'disabled' | 'connecting' | 'ok' | 'degraded'
  connected: boolean
  last_sample_at?: string
  last_error?: string
  storage_error?: string
  sample_count: number
  error_count: number
  write_errors: number
  max_gap_ms: number
  crc_errors: number
  partial_bytes: number
  pairing_errors: number
  request_frames: number
  response_frames: number
  mapped_frames: number
  mapping?: 'unmapped' | 'request_response'
}

export type ElectricalLatest = {
  continuous_enabled: boolean
  updated_at: string
  write_backlog: number
  sources: ElectricalSourceHealth[]
  samples: ElectricalSample[]
}

export type ElectricalHistoryParams = {
  source?: string
  channel?: string
  metric?: string
  from?: string
  to?: string
  limit?: number
}

export type ElectricalSingleShotResult = {
  started_at: string
  finished_at: string
  samples: ElectricalSample[]
  sources: ElectricalSourceHealth[]
  errors?: Record<string, string>
}

export function getElectricalLatest() {
  return request<ElectricalLatest>({ url: '/electrical/latest' })
}

export function getElectricalHistory(params: ElectricalHistoryParams = {}) {
  return request<{ points: ElectricalSample[] }>({ url: '/electrical/history', params })
}

export function singleShotElectrical() {
  return requestWithMeta<ElectricalSingleShotResult>({ url: '/electrical/single-shot', method: 'POST', data: {} })
}
