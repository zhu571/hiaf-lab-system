// 时间格式化统一工具（重构方案 §3.7）：locale 统一跟随 i18n（zh → 'zh-CN'、en → 'en-US'），
// 消灭 P1 的 12 处手写重复（formatTime/fmtTime/formatDate）与「写死 zh-CN / 跟随浏览器 / 跟随 i18n」
// 三种策略并存的问题。基于 Intl.DateTimeFormat 手写，不引入 dayjs（重构方案 §7 排除项）。
// 本批仅建文件 + 纯函数单测；12 处调用点替换属 S4/S5 视图迁移，不在本批。
// 空值/非法时间一律返回 '—'（与现有视图 formatTime 行为一致）。

import { i18n } from '@/i18n'

const EMPTY_TEXT = '—'

function resolveLocale(locale?: string): 'zh-CN' | 'en-US' {
  return (locale || i18n.global.locale.value) === 'zh' ? 'zh-CN' : 'en-US'
}

function toDate(ts?: string | number | Date | null): Date | null {
  if (ts === undefined || ts === null || ts === '') return null
  const d = ts instanceof Date ? ts : new Date(ts)
  return Number.isNaN(d.getTime()) ? null : d
}

/** 完整时间：YYYY-MM-DD HH:mm（en-US 为 MM/DD/YYYY, HH:mm），跟随 i18n locale */
export function formatDateTime(ts?: string | number | Date | null, locale?: string): string {
  const d = toDate(ts)
  if (!d) return EMPTY_TEXT
  return new Intl.DateTimeFormat(resolveLocale(locale), {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  }).format(d)
}

/** 日期：YYYY-MM-DD（en-US 为 MM/DD/YYYY） */
export function formatDate(ts?: string | number | Date | null, locale?: string): string {
  const d = toDate(ts)
  if (!d) return EMPTY_TEXT
  return new Intl.DateTimeFormat(resolveLocale(locale), {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  }).format(d)
}

/** 时间：HH:mm */
export function formatTime(ts?: string | number | Date | null, locale?: string): string {
  const d = toDate(ts)
  if (!d) return EMPTY_TEXT
  return new Intl.DateTimeFormat(resolveLocale(locale), {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  }).format(d)
}

/** 相对时间：<1min「刚刚/just now」，其余走 Intl.RelativeTimeFormat（x 分钟前 / 2 days ago） */
export function formatRelative(ts?: string | number | Date | null, locale?: string): string {
  const d = toDate(ts)
  if (!d) return EMPTY_TEXT
  const diff = d.getTime() - Date.now()
  const localeName = resolveLocale(locale)
  if (Math.abs(diff) < 60_000) return localeName === 'zh-CN' ? '刚刚' : 'just now'
  const rtf = new Intl.RelativeTimeFormat(localeName, { numeric: 'auto' })
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ['year', 31536000e3],
    ['month', 2592000e3],
    ['day', 86400e3],
    ['hour', 3600e3],
    ['minute', 60e3]
  ]
  for (const [unit, ms] of units) {
    if (Math.abs(diff) >= ms) return rtf.format(Math.round(diff / ms), unit)
  }
  return rtf.format(Math.round(diff / 60e3), 'minute')
}
