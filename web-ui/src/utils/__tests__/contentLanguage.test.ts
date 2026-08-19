import { describe, expect, it } from 'vitest'
import { resolveLocalizedText } from '../contentLanguage'

const field = { source_locale: 'zh', source_hash: 'x', zh: { status: 'ready' }, en: { status: 'ready', text: 'English' } }
describe('resolveLocalizedText', () => {
  it('uses ready target and original source', () => { expect(resolveLocalizedText('中文', field, 'en').text).toBe('English'); expect(resolveLocalizedText('中文', field, 'zh').isFallback).toBe(false) })
  it('falls back for missing or pending', () => { expect(resolveLocalizedText('中文', undefined, 'en').status).toBe('missing'); expect(resolveLocalizedText('中文', { ...field, en: { status: 'pending' } }, 'en').isFallback).toBe(true) })
})
