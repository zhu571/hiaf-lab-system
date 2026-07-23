import { request } from './client'

// 与 go-server/instruments/handler.go 的响应结构保持一致

export type InstrumentSummary = {
  id: string
  name: string
  state: string // running | rate_limited | needs_reconnect | error
}

export type InstrumentStatus = {
  instrument_id: string
  state: string
  rate_limited: boolean
}

// 白名单命令参数定义（来自仪器白名单.yaml，数值字段可能是 number 或 string）
export type CommandParamDef = {
  type?: string // float | int | string
  min?: number | string
  max?: number | string
  unit?: string
  default?: unknown
  enum?: (string | number)[]
  description?: string
}

export type WhitelistCommand = {
  name: string
  description: string
  risk: string // green | yellow | red
  scpi?: string
  build?: string
  timeout_ms?: number
  params?: Record<string, CommandParamDef>
  returns?: unknown
}

export type CommandResult = {
  command: string
  response?: string
  duration: number // Go time.Duration，单位纳秒
}

export interface NLCommandCandidate {
  status: string           // "ok" | "clarify" | "rejected"
  command?: string
  risk?: string
  scpi_preview?: string
  explanation?: string
  question?: string
  reason?: string
}

export interface NLExecuteResult {
  status: string
  command: string
  scpi: string
  explanation: string
  response: string
  parsed_value?: number
  parsed_points?: Array<{ x: number, y: number }>
  plot_type?: string
  duration_ms: number
  error: string
}

export type PiezoStatus = {
  a1: number
  valve_sp: number
  running: boolean
  error?: string
}

export function listInstruments() {
  return request<InstrumentSummary[]>({ url: '/instruments' })
}

export function getWhitelist() {
  return request<WhitelistCommand[]>({ url: '/instruments/whitelist' })
}

export function getStatus(id: string) {
  return request<InstrumentStatus>({ url: `/instruments/${id}/status` })
}

export function emergencyStop(id: string) {
  return request<{ status: string }>({ url: `/instruments/${id}/emergency-stop`, method: 'POST' })
}

export function executeCommand(id: string, command: string, params: Record<string, unknown> = {}) {
  return request<CommandResult>({ url: `/instruments/${id}/commands`, method: 'POST', data: { command, params } })
}

export function piezoStatus() {
  return request<PiezoStatus>({ url: '/instruments/piezo/status' })
}

export function piezoStart() {
  return request<{ status: string }>({ url: '/instruments/piezo/start', method: 'POST' })
}

export function piezoStop() {
  return request<{ status: string }>({ url: '/instruments/piezo/stop', method: 'POST' })
}

export function piezoSetpoint(value: number) {
  return request<{ setpoint: number }>({ url: '/instruments/piezo/setpoint', method: 'POST', data: { value } })
}

export function interpretNL(instrumentId: string, input: string, history: Array<{ role: string, content: string }>) {
  return request<NLCommandCandidate>({ url: `/instruments/${instrumentId}/nl-commands`, method: 'POST', data: { input, history } })
}

export function executeNL(instrumentId: string, input: string, history: Array<{ role: string, content: string }>) {
  return request<NLExecuteResult>({ url: `/instruments/${instrumentId}/nl-execute`, method: 'POST', data: { input, history } })
}
