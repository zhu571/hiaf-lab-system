import { createI18n } from 'vue-i18n'
import zh from './zh'
import en from './en'

export type AppLocale = 'zh' | 'en'

const STORAGE_KEY = 'language'

export function getStoredLocale(): AppLocale {
  return localStorage.getItem(STORAGE_KEY) === 'en' ? 'en' : 'zh'
}

// 语言优先级：登录后以后端 user.language 为准（auth store 会调 setLocale 覆盖），
// 未登录/后端字段缺失时回退到 localStorage，最终兜底中文。
export const i18n = createI18n({
  legacy: false,
  locale: getStoredLocale(),
  fallbackLocale: 'zh',
  messages: { zh, en }
})

export function setLocale(locale: AppLocale) {
  i18n.global.locale.value = locale
  localStorage.setItem(STORAGE_KEY, locale)
}
