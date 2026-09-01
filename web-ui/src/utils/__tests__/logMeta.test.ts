import { describe, it, expect } from 'vitest'
import { LOG_CATEGORIES, LOG_SOURCES, LOG_STATUSES, logCategoryKey, logSourceKey } from '@/utils/logMeta'
import { i18n } from '@/i18n'

// logMeta 枚举映射单测（对齐 statusMeta.test.ts 防线）：
// 每个枚举值都有显式 i18n key 且 zh/en 双语可解析；未登记值返回 undefined 由调用方回退原文。

describe('logMeta 枚举与 i18n key 映射', () => {
  it('枚举值集与后端 go-server/logs/model.go 一致', () => {
    expect(LOG_CATEGORIES).toEqual(['general', 'assembly', 'test', 'cryo', 'rf', 'vacuum', 'beam', 'data_analysis'])
    expect(LOG_STATUSES).toEqual(['draft', 'confirmed', 'locked', 'voided'])
    expect(LOG_SOURCES).toEqual(['manual', 'agent', 'import', 'wechat'])
  })

  it('全部分类/来源的 key 在 zh/en 双语下均可解析', () => {
    for (const locale of ['zh', 'en'] as const) {
      i18n.global.locale.value = locale
      for (const c of LOG_CATEGORIES) {
        const key = logCategoryKey(c)
        expect(key, `${c} 缺 i18n key`).toBeTruthy()
        expect(i18n.global.t(key!), `${key} 在 ${locale} 未解析`).not.toBe(key)
      }
      for (const s of LOG_SOURCES) {
        const key = logSourceKey(s)
        expect(key, `${s} 缺 i18n key`).toBeTruthy()
        expect(i18n.global.t(key!), `${key} 在 ${locale} 未解析`).not.toBe(key)
      }
    }
    i18n.global.locale.value = 'zh'
  })

  it('未登记值返回 undefined（调用方回退原文）', () => {
    expect(logCategoryKey('bogus')).toBeUndefined()
    expect(logSourceKey('bogus')).toBeUndefined()
  })
})
