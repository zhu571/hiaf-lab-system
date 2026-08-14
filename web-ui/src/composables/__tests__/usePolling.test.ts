import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, defineComponent } from 'vue'
import { usePolling } from '../usePolling'

// 测试辅助：把 composable 挂到真实组件 setup 上（onBeforeUnmount 清理钩子必需）
function mountSetup<T>(setup: () => T): { get: () => T; unmount: () => void } {
  let result!: T
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(
    defineComponent({
      setup() {
        result = setup()
        return () => null
      }
    })
  )
  app.mount(host)
  return {
    get: () => result,
    unmount: () => {
      app.unmount()
      host.remove()
    }
  }
}

// jsdom 的 document.hidden 是原型 getter，用 spy 覆盖以模拟切 tab
function hiddenSpy(value: boolean) {
  const spy = vi.spyOn(document, 'hidden', 'get')
  spy.mockReturnValue(value)
  return spy
}

describe('usePolling（S2，§3.5）', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('interval 触发：按 ms 间隔调用 fn，start 前不触发', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { get } = mountSetup(() => usePolling(fn, 1000))
    const p = get()
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).not.toHaveBeenCalled() // 未 start 不轮询

    p.start()
    expect(fn).not.toHaveBeenCalled() // 非 immediate 不立即执行
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(2000)
    expect(fn).toHaveBeenCalledTimes(3)
  })

  it('immediate:true：start 时立即执行一次 fn', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { get } = mountSetup(() => usePolling(fn, 1000, { immediate: true }))
    get().start()
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('document.hidden 暂停：隐藏期间不触发，恢复可见立即执行一次再恢复周期', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const hidden = hiddenSpy(false)
    const { get } = mountSetup(() => usePolling(fn, 1000))
    const p = get()
    p.start()

    hidden.mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(3000)
    expect(fn).not.toHaveBeenCalled() // 隐藏期间暂停

    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fn).toHaveBeenCalledTimes(1) // 恢复可见立即刷新（对齐 AppLayout/SensorsView 现状语义）
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(2) // 周期恢复
  })

  it('resumeImmediate:false：恢复可见不立即执行，仅恢复周期（SensorsView history 现状语义）', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const hidden = hiddenSpy(false)
    const { get } = mountSetup(() => usePolling(fn, 1000, { resumeImmediate: false }))
    const p = get()
    p.start()

    hidden.mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(3000)
    expect(fn).not.toHaveBeenCalled()

    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fn).not.toHaveBeenCalled() // 不立即执行
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(1) // 仅周期恢复
  })

  it('unmount 清理：卸载后 interval 与 visibilitychange 监听均不再触发 fn', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const hidden = hiddenSpy(false)
    const { get, unmount } = mountSetup(() => usePolling(fn, 1000))
    const p = get()
    p.start()
    unmount()

    await vi.advanceTimersByTimeAsync(3000)
    expect(fn).not.toHaveBeenCalled()

    hidden.mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fn).not.toHaveBeenCalled()
  })

  it('stop 后再 start 可重新轮询（autoRefresh 开关启停语义）', async () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { get } = mountSetup(() => usePolling(fn, 1000))
    const p = get()
    p.start()
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(1)

    p.stop()
    await vi.advanceTimersByTimeAsync(3000)
    expect(fn).toHaveBeenCalledTimes(1) // 停止后不再触发

    p.start()
    expect(p.isRunning()).toBe(true)
    await vi.advanceTimersByTimeAsync(1000)
    expect(fn).toHaveBeenCalledTimes(2) // 重启后恢复周期
  })
})
