import { describe, it, expect, afterEach } from 'vitest'
import {
  STATUS_META,
  statusMetaFor,
  findStatusMeta,
  type StatusDomain,
  type StatusMeta,
  type StatusTone
} from '@/utils/statusMeta'
import { i18n } from '@/i18n'

// statusMeta 注册表单测（重构方案 §5 关键路径 #7）：全域注册表 + 每 domain 未注册 value 的 fallback 行为；
// labelKey 显式字面量且 zh/en 双语言均可解析（keys.test.ts 防线之外的双语可解析性检查）。

const TONES: StatusTone[] = ['success', 'warning', 'info', 'primary', 'danger']
const ALL_DOMAINS = Object.keys(STATUS_META) as StatusDomain[]

afterEach(() => {
  i18n.global.locale.value = 'zh'
})

function allEntries(): Array<[StatusDomain, string, StatusMeta]> {
  return ALL_DOMAINS.flatMap((domain) =>
    Object.entries(STATUS_META[domain]).map(([value, meta]): [StatusDomain, string, StatusMeta] => [domain, value, meta])
  )
}

describe('注册表结构与完整性', () => {
  it('十域起步 + 行为兼容核对补登域 + 美术 S4 派生态域，共 14 个 domain', () => {
    expect(ALL_DOMAINS).toEqual([
      'runStatus',
      'stepStatus',
      'issueStatus',
      'issueSeverity',
      'alertLevel',
      'instrumentState',
      'testQuality',
      'todoPriority',
      'userRole',
      'projectStage',
      'experienceStatus',
      'reportStatus',
      'agentCandidateStatus',
      'onlineStatus',
      'invitationCode',
      'translation'
    ])
  })

  it('每个 meta 的 tone 合法、labelKey 为显式字面量且 zh/en 双语均可解析', () => {
    for (const [domain, value, meta] of allEntries()) {
      expect(TONES, `${domain}.${value} tone 非法`).toContain(meta.tone)
      expect(meta.labelKey, `${domain}.${value} 缺 labelKey`).toMatch(/^[a-zA-Z][\w.]*$/)
      expect(meta.graphic, `${domain}.${value} 缺 graphic`).toMatch(/^--/)
      expect(meta.text, `${domain}.${value} 缺 text`).toMatch(/^--/)
      expect(meta.soft, `${domain}.${value} 缺 soft`).toMatch(/^--/)
      for (const locale of ['zh', 'en'] as const) {
        i18n.global.locale.value = locale
        const label = i18n.global.t(meta.labelKey)
        expect(label, `${meta.labelKey} 在 ${locale} 未解析`).not.toBe(meta.labelKey)
        expect(label).toBeTruthy()
      }
    }
  })

  it('行为兼容核对：原 3 组硬编码映射 tone 全部保留（美术 §3.8）', () => {
    const success = ['active', 'published', 'confirmed', 'resolved']
    const warning = ['draft', 'candidate', 'open']
    const info = ['archived', 'closed', 'locked']
    for (const value of success) {
      expect(findStatusMeta(value)?.tone, `${value} 应 success`).toBe('success')
    }
    for (const value of warning) {
      expect(findStatusMeta(value)?.tone, `${value} 应 warning`).toBe('warning')
    }
    for (const value of info) {
      expect(findStatusMeta(value)?.tone, `${value} 应 info`).toBe('info')
    }
  })
})

describe('statusMetaFor 按 domain 查表', () => {
  it('已知 domain/value 返回正确 tone', () => {
    expect(statusMetaFor('runStatus', 'active')?.tone).toBe('success')
    expect(statusMetaFor('runStatus', 'aborted')?.tone).toBe('danger')
    expect(statusMetaFor('stepStatus', 'in_progress')?.tone).toBe('primary')
    expect(statusMetaFor('issueStatus', 'open')?.tone).toBe('warning')
    expect(statusMetaFor('issueSeverity', 'critical')?.tone).toBe('danger')
    expect(statusMetaFor('alertLevel', 'error')?.tone).toBe('danger')
    expect(statusMetaFor('testQuality', 'normal')?.tone).toBe('success')
    expect(statusMetaFor('todoPriority', 'high')?.tone).toBe('danger')
    expect(statusMetaFor('userRole', 'admin')?.tone).toBe('primary')
    expect(statusMetaFor('projectStage', 'draft')?.tone).toBe('warning')
  })

  it('每个 domain 的未注册 value 返回 undefined（fallback 走组件侧：原文 + warn + tone 落 info）', () => {
    for (const domain of ALL_DOMAINS) {
      expect(statusMetaFor(domain, 'bogus_status'), `${domain} 未注册值应 undefined`).toBeUndefined()
    }
  })

  it('findStatusMeta 跨域值优先查找：任意域注册过的值都能命中', () => {
    expect(findStatusMeta('active')?.tone).toBe('success')
    expect(findStatusMeta('published')?.labelKey).toBe('experiences.published')
    expect(findStatusMeta('locked')?.labelKey).toBe('dailyHistory.locked')
    expect(findStatusMeta('execution_failed')?.tone).toBe('danger')
    expect(findStatusMeta('bogus_status')).toBeUndefined()
  })
})
