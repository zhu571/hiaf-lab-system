import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { nextTick } from 'vue'

// useTheme 是模块级单例（import 即执行 useColorMode），因此：
// 1. matchMedia 必须在 import 前 stub（setup.ts 的静态 stub 不支持可控切换，此处自建可派发 change 的可控版）；
// 2. 每个用例经 vi.resetModules() + 动态 import 重建模块实例，模拟「刷新页面」重新初始化。
// 断言基调（美术 §3.6 契约 + 测试方案 §8.4 R4-9）：只验行为不测库内部——localStorage 裸值持久化、
// 三态解析（含非法值回落亮色）、html.dark class、theme-color 双 meta 同步、storage 事件多标签同步。

type ThemeModule = typeof import('../useTheme')
let useThemeFn: ThemeModule['useTheme']
let theme: ReturnType<ThemeModule['useTheme']>

// 双 meta canonical 值（与 useTheme.ts / index.html / tokens.css 逐字一致）
const THEME_COLOR_LIGHT = '#f1f4f9'
const THEME_COLOR_DARK = '#0e1822'

type MqlListener = (e: { matches: boolean }) => void

// 可控 matchMedia：返回 mql（初始不匹配 = 系统浅色），setSystemDark 切换匹配态并派发 change 事件
function createMatchMedia() {
  const listeners = new Set<MqlListener>()
  const mql = {
    matches: false,
    media: '(prefers-color-scheme: dark)',
    addEventListener: (_type: string, cb: MqlListener) => listeners.add(cb),
    removeEventListener: (_type: string, cb: MqlListener) => listeners.delete(cb)
  }
  return {
    mql,
    setSystemDark(v: boolean) {
      mql.matches = v
      for (const cb of listeners) cb({ matches: v })
    }
  }
}

let setSystemDark: (v: boolean) => void

async function reload() {
  vi.resetModules()
  useThemeFn = (await import('../useTheme')).useTheme
  theme = useThemeFn()
}

const htmlClass = () => document.documentElement.classList
const metas = () => Array.from(document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]'))
const metaFor = (scheme: 'light' | 'dark') => metas().find((m) => m.getAttribute('media')?.includes(scheme))!

beforeEach(async () => {
  localStorage.clear()
  htmlClass().remove('light', 'dark')
  metas().forEach((m) => m.remove())
  const ctrl = createMatchMedia()
  setSystemDark = ctrl.setSystemDark
  window.matchMedia = vi.fn(() => ctrl.mql) as unknown as typeof window.matchMedia
  // 双 meta（与 index.html 同构：带 media，auto 态走系统级跟随；setAttribute 保证 jsdom 反射可读）
  const mk = (media: string, content: string) => {
    const el = document.createElement('meta')
    el.setAttribute('name', 'theme-color')
    el.setAttribute('media', media)
    el.setAttribute('content', content)
    document.head.appendChild(el)
  }
  mk('(prefers-color-scheme: light)', THEME_COLOR_LIGHT)
  mk('(prefers-color-scheme: dark)', THEME_COLOR_DARK)
  await reload()
})

afterEach(() => {
  localStorage.clear()
  htmlClass().remove('light', 'dark')
  metas().forEach((m) => m.remove())
})

describe('useTheme 单例契约', () => {
  it('多次调用返回同一 store/state 引用（组件禁止各自调 useColorMode）', () => {
    const a = useThemeFn()
    const b = useThemeFn()
    expect(a.store).toBe(b.store)
    expect(a.state).toBe(b.state)
    expect(a.system).toBe(b.system)
  })
})

describe('storageKey 读写与持久化', () => {
  it("setTheme 写 localStorage 'theme' 裸三态值，且同步 html.dark class", async () => {
    theme.setTheme('dark')
    await nextTick() // useStorage 落盘 flush:'pre' 异步
    expect(localStorage.getItem('theme')).toBe('dark')
    await nextTick()
    expect(htmlClass().contains('dark')).toBe(true)

    theme.setTheme('light')
    await nextTick()
    expect(localStorage.getItem('theme')).toBe('light')
    theme.setTheme('auto')
    await nextTick()
    expect(localStorage.getItem('theme')).toBe('auto')
  })

  it('重载模块（模拟刷新）后从 localStorage 恢复持久化状态', async () => {
    theme.setTheme('dark')
    await nextTick()
    await reload()
    expect(theme.store.value).toBe('dark')
    await nextTick()
    expect(htmlClass().contains('dark')).toBe(true)
  })
})

describe('三态解析（html.dark class 驱动）', () => {
  it('light：html 加 light class，无 dark；state 解析 light', async () => {
    theme.setTheme('light')
    await nextTick()
    expect(htmlClass().contains('light')).toBe(true)
    expect(htmlClass().contains('dark')).toBe(false)
    expect(theme.state.value).toBe('light')
  })

  it('dark：html 加 dark class，无 light；state 解析 dark', async () => {
    theme.setTheme('dark')
    await nextTick()
    expect(htmlClass().contains('dark')).toBe(true)
    expect(htmlClass().contains('light')).toBe(false)
    expect(theme.state.value).toBe('dark')
  })

  it('auto + 系统浅色：state 解析 light，html 无 dark', async () => {
    theme.setTheme('auto')
    await nextTick()
    expect(theme.state.value).toBe('light')
    expect(htmlClass().contains('dark')).toBe(false)
  })

  it('auto + 系统深色：state 解析 dark，html 有 dark', async () => {
    setSystemDark(true)
    theme.setTheme('auto')
    await nextTick()
    expect(theme.state.value).toBe('dark')
    expect(htmlClass().contains('dark')).toBe(true)
  })

  it('auto 下系统偏好切换：state 与 html class 实时跟随', async () => {
    theme.setTheme('auto')
    await nextTick()
    setSystemDark(true)
    await nextTick()
    expect(theme.state.value).toBe('dark')
    expect(htmlClass().contains('dark')).toBe(true)
    setSystemDark(false)
    await nextTick()
    expect(theme.state.value).toBe('light')
    expect(htmlClass().contains('dark')).toBe(false)
  })
})

describe('非法存储值回退亮色（§6.1 无注入路径）', () => {
  it('非法值：html 上 light/dark class 均移除，视觉回落亮色', async () => {
    localStorage.setItem('theme', 'bogus')
    await reload()
    await nextTick()
    expect(htmlClass().contains('dark')).toBe(false)
    expect(htmlClass().contains('light')).toBe(false)
    // 无 dark class → 所有 dark 令牌规则不生效 = 亮色
  })

  it('非法值即使系统深色也不加 dark（不按 auto 语义漂移）', async () => {
    localStorage.setItem('theme', 'bogus')
    setSystemDark(true)
    await reload()
    await nextTick()
    expect(htmlClass().contains('dark')).toBe(false)
  })
})

describe('theme-color 双 meta 同步', () => {
  it('手动 dark：两个 meta 统一写深色 canonical', async () => {
    theme.setTheme('dark')
    await nextTick()
    metas().forEach((m) => expect(m.content).toBe(THEME_COLOR_DARK))
  })

  it('手动 light：两个 meta 统一写浅色 canonical', async () => {
    theme.setTheme('light')
    await nextTick()
    metas().forEach((m) => expect(m.content).toBe(THEME_COLOR_LIGHT))
  })

  it('auto（由手动切换回）：按 media 恢复 canonical 双值', async () => {
    theme.setTheme('dark')
    await nextTick()
    metas().forEach((m) => expect(m.content).toBe(THEME_COLOR_DARK))
    theme.setTheme('auto')
    await nextTick()
    expect(metaFor('light').content).toBe(THEME_COLOR_LIGHT)
    expect(metaFor('dark').content).toBe(THEME_COLOR_DARK)
  })

  it('auto 下系统切换：meta 保持按 media 的 canonical 双值', async () => {
    theme.setTheme('auto')
    theme.setTheme('dark')
    await nextTick()
    theme.setTheme('auto')
    await nextTick()
    setSystemDark(true)
    await nextTick()
    expect(metaFor('light').content).toBe(THEME_COLOR_LIGHT)
    expect(metaFor('dark').content).toBe(THEME_COLOR_DARK)
  })
})

describe('storage 事件多标签同步', () => {
  it('另一标签写入 theme 并派发 storage 事件：store / html class 跟随', async () => {
    await nextTick() // 先 flush 掉本实例初始写入，避免与手写值撞车
    localStorage.setItem('theme', 'dark')
    window.dispatchEvent(
      new StorageEvent('storage', { key: 'theme', newValue: 'dark', storageArea: window.localStorage })
    )
    await nextTick()
    expect(theme.store.value).toBe('dark')
    expect(htmlClass().contains('dark')).toBe(true)
  })
})

describe('disableTransition（若可测：临时禁过渡 style 插入即移除、无残留）', () => {
  it('切换主题时插入禁用过渡 style 并立即移除，head 无残留', async () => {
    await nextTick() // flush 掉初始化时的那次插入，保证 spy 计数干净
    const appendSpy = vi.spyOn(document.head, 'appendChild')
    const removeSpy = vi.spyOn(document.head, 'removeChild')
    try {
      theme.setTheme('dark')
      await nextTick()
      expect(appendSpy).toHaveBeenCalled()
      expect(removeSpy).toHaveBeenCalled()
      expect(appendSpy.mock.calls.length).toBe(removeSpy.mock.calls.length)
      expect(document.head.querySelectorAll('style')).toHaveLength(0)
      expect(htmlClass().contains('dark')).toBe(true) // 过渡禁用不影响 class 更新
    } finally {
      appendSpy.mockRestore()
      removeSpy.mockRestore()
    }
  })
})
