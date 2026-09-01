// 日志领域枚举与 i18n key 映射（唯一事实源 = go-server/logs/model.go 常量）：
// category 8 类、content_status 4 态、source 4 源。label 一律显式 i18n key 字面量
//（对齐 statusMeta 约定：禁模板字符串拼装动态 key，keys.test.ts 静态可扫）。
// 状态徽标走 statusMeta 的 logStatus 域；本文件只管分类/来源这两个非状态枚举的文案映射。

export const LOG_CATEGORIES = ['general', 'assembly', 'test', 'cryo', 'rf', 'vacuum', 'beam', 'data_analysis'] as const

export const LOG_STATUSES = ['draft', 'confirmed', 'locked', 'voided'] as const

export const LOG_SOURCES = ['manual', 'agent', 'import', 'wechat'] as const

const LOG_CATEGORY_KEYS: Record<string, string> = {
  general: 'logCategory.general',
  assembly: 'logCategory.assembly',
  test: 'logCategory.test',
  cryo: 'logCategory.cryo',
  rf: 'logCategory.rf',
  vacuum: 'logCategory.vacuum',
  beam: 'logCategory.beam',
  data_analysis: 'logCategory.data_analysis'
}

const LOG_SOURCE_KEYS: Record<string, string> = {
  manual: 'logSource.manual',
  agent: 'logSource.agent',
  import: 'logSource.import',
  wechat: 'logSource.wechat'
}

/** 分类枚举 → i18n key；未登记值返回 undefined，调用方回退原文 */
export function logCategoryKey(category: string): string | undefined {
  return LOG_CATEGORY_KEYS[category]
}

/** 来源枚举 → i18n key；未登记值返回 undefined，调用方回退原文 */
export function logSourceKey(source: string): string | undefined {
  return LOG_SOURCE_KEYS[source]
}
