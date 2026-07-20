import { request } from './client'

// 与 go-server/sensors 的实际参数一致：
// latest 用 tags（逗号分隔 measurement 列表，空 = 全部默认 measurement）
// history 只支持单个 tag（measurement 名），from/to 是 Flux range 表达式（如 -1h 或 RFC3339），
// interval 是 Flux duration（如 1m），用于 aggregateWindow 降采样

export type SensorPoint = {
  time: string
  tag: string
  value: number
  meta?: Record<string, string>
}

export type SensorPoints = {
  points: SensorPoint[]
}

export function getLatest(tags: string[] = []) {
  return request<SensorPoints>({
    url: '/sensors/latest',
    params: tags.length ? { tags: tags.join(',') } : {}
  })
}

export function getHistory(tag: string, from = '-1h', to = '', interval = '') {
  const params: Record<string, string> = { tag, from }
  if (to) params.to = to
  if (interval) params.interval = interval
  return request<SensorPoints>({ url: '/sensors/history', params })
}
