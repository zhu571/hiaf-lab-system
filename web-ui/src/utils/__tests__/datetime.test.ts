import { describe, it, expect, afterEach } from 'vitest'
import { formatDateTime, formatDate, formatFullDate, formatTime, formatRelative } from '@/utils/datetime'
import { i18n } from '@/i18n'

// datetime 纯函数单测（重构方案 §5 关键路径 #6）：同一 ts 在 zh/en locale 下输出不同格式；
// locale 未传时跟随 i18n 全局 locale（消灭 P1 三种策略并存）。
const TS = new Date('2026-08-14T10:30:00+08:00')

afterEach(() => {
  i18n.global.locale.value = 'zh'
})

describe('formatDateTime', () => {
  it('同一 ts 在 zh/en locale 参数下输出不同格式', () => {
    const zh = formatDateTime(TS, 'zh')
    const en = formatDateTime(TS, 'en')
    expect(zh).not.toBe(en)
    expect(zh).toMatch(/\d{4}\/\d{2}\/\d{2}/)
    expect(en).toMatch(/10:30/)
  })

  it('locale 未传时跟随 i18n 全局 locale（zh → zh-CN、en → en-US）', () => {
    i18n.global.locale.value = 'zh'
    const zh = formatDateTime(TS)
    i18n.global.locale.value = 'en'
    const en = formatDateTime(TS)
    expect(zh).not.toBe(en)
  })

  it('空值/非法时间返回 —（与现有视图 formatTime 行为一致）', () => {
    expect(formatDateTime()).toBe('—')
    expect(formatDateTime(null)).toBe('—')
    expect(formatDateTime('not-a-date')).toBe('—')
    expect(formatDateTime('')).toBe('—')
  })
})

describe('formatDate / formatTime', () => {
  it('formatDate 仅输出日期，formatTime 仅输出时分', () => {
    const d = formatDate(TS, 'zh')
    const tm = formatTime(TS, 'zh')
    expect(d).toMatch(/\d{4}\/\d{2}\/\d{2}/)
    expect(d).not.toContain('10:30')
    expect(tm).toBe('10:30')
    expect(tm).not.toContain('2026')
  })
})

describe('formatFullDate', () => {
  it('zh 输出年月日 + 星期（R6 工作台头条日期）', () => {
    const zh = formatFullDate(TS, 'zh')
    expect(zh).toContain('2026年8月14日')
    expect(zh).toMatch(/星期|周/)
  })

  it('en 输出长格式英文日期（含 weekday long）', () => {
    const en = formatFullDate(TS, 'en')
    expect(en).toContain('Friday')
    expect(en).toContain('August')
  })

  it('空值/非法时间返回 —', () => {
    expect(formatFullDate()).toBe('—')
    expect(formatFullDate(null)).toBe('—')
    expect(formatFullDate('not-a-date')).toBe('—')
  })
})

describe('formatRelative', () => {
  it('1 分钟内为「刚刚」，过去数小时/天走 RelativeTimeFormat', () => {
    expect(formatRelative(Date.now(), 'zh')).toBe('刚刚')
    expect(formatRelative(Date.now() - 5 * 60 * 1000, 'zh')).toMatch(/5 ?分钟前/)
    expect(formatRelative(Date.now() - 3 * 3600 * 1000, 'en')).toBe('3 hours ago')
    expect(formatRelative(Date.now() - 2 * 86400 * 1000, 'zh')).toMatch(/2 ?天前|前天/)
  })
})
