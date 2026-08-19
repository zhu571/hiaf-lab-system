export type ContentVariant = { status: string; text?: string; origin?: string; editable?: boolean }
export type FieldTranslations = { source_locale: string; source_hash: string; zh: ContentVariant; en: ContentVariant }
export function resolveLocalizedText(original: string, field: FieldTranslations | undefined, locale: string) {
  if (!original) return { text: '', displayedLocale: locale, isFallback: false, status: 'missing' }
  if (!field) return { text: original, displayedLocale: 'und', isFallback: true, status: 'missing' }
  if (locale === field.source_locale) return { text: original, displayedLocale: locale, isFallback: false, status: 'ready' }
  const variant = locale === 'zh' ? field.zh : field.en
  if (variant.status === 'ready' && variant.text) return { text: variant.text, displayedLocale: locale, isFallback: false, status: 'ready' }
  return { text: original, displayedLocale: field.source_locale, isFallback: true, status: variant.status }
}
