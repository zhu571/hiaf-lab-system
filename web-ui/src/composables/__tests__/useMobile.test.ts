import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, type Ref } from 'vue'
import { useMobile } from '../useMobile'

// @vueuse/core 打桩：只验证 useMobile 的封装正确性（§3.1 不测库自身行为）——
// 断言传入 useMediaQuery 的 query 恰为 (max-width: 768px)，且返回值跟随 matchMedia 匹配态。
const mocks = vi.hoisted(() => ({
  useMediaQuery: vi.fn()
}))

vi.mock('@vueuse/core', () => ({
  useMediaQuery: mocks.useMediaQuery
}))

// 可控 matchMedia stub：支持 addEventListener('change')，测试可切换匹配态并派发事件
function createControllableMql() {
  const listeners = new Set<(e: { matches: boolean }) => void>()
  const mql = {
    matches: false,
    media: '(max-width: 768px)',
    addEventListener: (_type: string, cb: (e: { matches: boolean }) => void) => {
      listeners.add(cb)
    },
    removeEventListener: (_type: string, cb: (e: { matches: boolean }) => void) => {
      listeners.delete(cb)
    }
  }
  return {
    mql,
    setMatches(v: boolean) {
      mql.matches = v
      for (const cb of listeners) cb({ matches: v })
    }
  }
}

describe('useMobile 移动端断点封装', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('传给 useMediaQuery 的 query 恰为 (max-width: 768px)', () => {
    mocks.useMediaQuery.mockReturnValue(ref(false))
    useMobile()
    expect(mocks.useMediaQuery).toHaveBeenCalledWith('(max-width: 768px)')
  })

  it('返回 ref 跟随 matchMedia 匹配态变化', () => {
    const { mql, setMatches } = createControllableMql()
    mocks.useMediaQuery.mockImplementation((_query: string): Ref<boolean> => {
      const result = ref(mql.matches)
      mql.addEventListener('change', (e) => {
        result.value = e.matches
      })
      return result
    })

    const isMobile = useMobile()
    expect(isMobile.value).toBe(false)

    setMatches(true)
    expect(isMobile.value).toBe(true)

    setMatches(false)
    expect(isMobile.value).toBe(false)
  })
})
