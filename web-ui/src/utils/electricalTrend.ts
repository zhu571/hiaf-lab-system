import type { ElectricalSample } from '@/api/electrical'
import type { ChartPoint } from '@/utils/chartTheme'

export const MESTEK_ARC_THRESHOLD_A = 0.1
export const ELECTRICAL_TREND_POINT_LIMIT = 120

export type MestekArcState = 'unknown' | 'safe' | 'danger'

export function mestekArcState(value: number | undefined): MestekArcState {
  if (typeof value !== 'number' || !Number.isFinite(value)) return 'unknown'
  return value > MESTEK_ARC_THRESHOLD_A ? 'danger' : 'safe'
}

export function electricalSeriesKey(source: string, channel: string, metric: string) {
  return [source, channel, metric].join('\u0000')
}

/** Stable latest-sample identity; array positions must never decide replacement. */
export function electricalSampleKey(sample: Pick<ElectricalSample, 'source' | 'channel' | 'metric' | 'mapping' | 'signature'>) {
  return [electricalSeriesKey(sample.source, sample.channel, sample.metric), sample.mapping, sample.signature ?? ''].join('\u0000')
}

/** Merge stored history and live samples without letting signatures split a known channel. */
export function mergeElectricalTrend(
  history: Record<string, ChartPoint[]>,
  samples: ElectricalSample[],
  limit = ELECTRICAL_TREND_POINT_LIMIT
): Record<string, ChartPoint[]> {
  const next = { ...history }
  for (const sample of samples) {
    if (sample.channel === 'UNMAPPED' || typeof sample.value !== 'number' || !Number.isFinite(sample.value)) continue
    const time = Date.parse(sample.time)
    if (!Number.isFinite(time)) continue
    const key = electricalSeriesKey(sample.source, sample.channel, sample.metric)
    const byTime = new Map((next[key] ?? []).map((point) => [point.time, point]))
    byTime.set(time, { time, value: sample.value })
    next[key] = Array.from(byTime.values()).sort((a, b) => a.time - b.time).slice(-limit)
  }
  return next
}

export type SparklinePaths = {
  line: string
  area: string
  endX: number
  endY: number
}

/** Build a time-proportional sparkline; a single reading becomes a flat segment. */
export function buildSparklinePaths(
  points: ChartPoint[],
  width = 300,
  height = 72,
  padding = 5
): SparklinePaths {
  if (!points.length) return { line: '', area: '', endX: 0, endY: 0 }
  const usableWidth = width - padding * 2
  const usableHeight = height - padding * 2
  const values = points.map((point) => point.value)
  const minValue = Math.min(...values)
  const maxValue = Math.max(...values)
  const valueSpan = maxValue - minValue
  const startTime = points[0].time
  const endTime = points[points.length - 1].time
  const timeSpan = endTime - startTime
  const coordinates = points.map((point, index) => {
    const x = points.length === 1
      ? padding + usableWidth / 2
      : padding + (timeSpan > 0 ? (point.time - startTime) / timeSpan : index / (points.length - 1)) * usableWidth
    const y = valueSpan > 0
      ? padding + (maxValue - point.value) / valueSpan * usableHeight
      : height / 2
    return { x, y }
  })
  if (coordinates.length === 1) {
    const point = coordinates[0]
    coordinates.unshift({ x: padding, y: point.y })
    coordinates.push({ x: width - padding, y: point.y })
  }
  const line = coordinates.map((point, index) => `${index === 0 ? 'M' : 'L'}${point.x.toFixed(2)},${point.y.toFixed(2)}`).join(' ')
  const first = coordinates[0]
  const last = coordinates[coordinates.length - 1]
  const area = `${line} L${last.x.toFixed(2)},${height - padding} L${first.x.toFixed(2)},${height - padding} Z`
  return { line, area, endX: last.x, endY: last.y }
}
