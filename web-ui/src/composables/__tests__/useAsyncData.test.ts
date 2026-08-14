import { describe, it, expect, vi } from 'vitest'
import { createApp, defineComponent, nextTick, ref } from 'vue'
import { ApiError } from '@/api/client'
import { useAsyncData } from '../useAsyncData'

// 测试辅助：把 composable 挂到真实组件 setup 上（onBeforeUnmount 等生命周期钩子必需），
// 通过 get() 取回返回值、unmount() 触发组件卸载。
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

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('useAsyncData（S2，§3.5）', () => {
  it('immediate 默认 true：创建即执行一次 loader，成功后回写 data 并复位 loading', async () => {
    const loader = vi.fn(async () => 42)
    const { get } = mountSetup(() => useAsyncData(loader))
    const s = get()
    expect(s.loading.value).toBe(true)
    await nextTick()
    expect(loader).toHaveBeenCalledTimes(1)
    expect(s.data.value).toBe(42)
    expect(s.loading.value).toBe(false)
    expect(s.error.value).toBeNull()
  })

  it('immediate:false：不自动执行，手动 run 才加载', async () => {
    const loader = vi.fn(async () => 'ok')
    const { get } = mountSetup(() => useAsyncData(loader, { immediate: false }))
    const s = get()
    expect(loader).not.toHaveBeenCalled()
    expect(s.loading.value).toBe(false)
    s.run()
    await nextTick()
    expect(s.data.value).toBe('ok')
  })

  it('连续两次 run 只有后者回写（竞态）：先完成的旧请求不回写 data/loading', async () => {
    const d1 = deferred<number>()
    const d2 = deferred<number>()
    let call = 0
    const { get } = mountSetup(() =>
      useAsyncData(() => {
        call++
        return call === 1 ? d1.promise : d2.promise
      }, { immediate: false })
    )
    const s = get()
    const r1 = s.run()
    const r2 = s.run()

    d1.resolve(1) // 旧请求先完成
    await r1
    expect(s.data.value).toBeNull()
    expect(s.loading.value).toBe(true) // loading 仍由新请求持有

    d2.resolve(2)
    await r2
    expect(s.data.value).toBe(2)
    expect(s.loading.value).toBe(false)
  })

  it('unmount 后回写忽略：data/error/loading 均不再变更', async () => {
    const d = deferred<number>()
    const { get, unmount } = mountSetup(() => useAsyncData(() => d.promise, { immediate: false }))
    const s = get()
    const runPromise = s.run()
    unmount()
    d.resolve(42)
    await runPromise
    expect(s.data.value).toBeNull()
    expect(s.loading.value).toBe(true) // 回写被丢弃，状态冻结在 unmount 前
  })

  it('loader 失败：ApiError 写入 error ref（kind 透传）、loading 复位、不向上抛', async () => {
    const { get } = mountSetup(() =>
      useAsyncData(
        async () => {
          throw new ApiError('Network Error', 'network', { requestId: 'req_x' })
        },
        { immediate: false }
      )
    )
    const s = get()
    await expect(s.run()).resolves.toBeUndefined()
    expect(s.data.value).toBeNull()
    expect(s.loading.value).toBe(false)
    expect(s.error.value).toMatchObject({ message: 'Network Error', kind: 'network', requestId: 'req_x' })
  })

  it('普通 Error 归一化为 ApiError（unknown kind），非 Error 异常兜底「请求失败」', async () => {
    const { get } = mountSetup(() =>
      useAsyncData(
        async () => {
          throw new Error('手动错误')
        },
        { immediate: false }
      )
    )
    const s = get()
    await s.run()
    expect(s.error.value).toBeInstanceOf(ApiError)
    expect(s.error.value).toMatchObject({ message: '手动错误', kind: 'unknown' })
  })

  it('reset：在途请求过期并复位 data/error/loading', async () => {
    const d = deferred<number>()
    const { get } = mountSetup(() => useAsyncData(() => d.promise, { immediate: false }))
    const s = get()
    s.run()
    s.reset()
    expect(s.data.value).toBeNull()
    expect(s.loading.value).toBe(false)
    d.resolve(1)
    await nextTick()
    expect(s.data.value).toBeNull() // 过期请求不回写
  })

  it('watch 源变化自动重新 run', async () => {
    const source = ref(1)
    const loader = vi.fn(async () => source.value * 10)
    const { get } = mountSetup(() => useAsyncData(loader, { immediate: false, watch: [source] }))
    const s = get()
    source.value = 2
    await nextTick()
    expect(loader).toHaveBeenCalledTimes(1)
    await nextTick()
    expect(s.data.value).toBe(20)
  })
})
