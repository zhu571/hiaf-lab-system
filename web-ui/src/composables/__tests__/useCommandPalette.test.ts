import { describe, it, expect, beforeEach } from 'vitest'
import { useCommandPalette } from '@/composables/useCommandPalette'

// useCommandPalette 单例开关测试（结构改版 R2 §3.1）：Ctrl/⌘+K 与顶栏触发框共用同一实例。
describe('useCommandPalette 单例开关', () => {
  beforeEach(() => {
    useCommandPalette().closePalette()
  })

  it('open/close/toggle 语义与跨调用共享同一状态', () => {
    const a = useCommandPalette()
    const b = useCommandPalette()
    expect(a.paletteOpen.value).toBe(false)

    a.openPalette()
    expect(b.paletteOpen.value).toBe(true)

    b.togglePalette()
    expect(a.paletteOpen.value).toBe(false)

    b.togglePalette()
    expect(a.paletteOpen.value).toBe(true)

    a.closePalette()
    expect(b.paletteOpen.value).toBe(false)
  })
})
