import { onBeforeUnmount, ref, watch, type WatchSource } from 'vue'
import { ApiError, isApiError } from '@/api/client'

export interface UseAsyncDataOptions {
  /** 创建时立即执行一次 loader（页面级 loader 默认 true） */
  immediate?: boolean
  /** 任一源变化时重新 run（如路由参数） */
  watch?: WatchSource[]
}

// 竞态保护（重构方案 §3.5）：每次 run 自增 seq，仅当次 seq 最新才回写 data/error/loading；
// 组件 unmount 后置 unmounted 标记，回写一律丢弃（泛化 SensorsView 手写 seq 模式，
// 闭合「路由切换后旧请求回写 state」问题）。逻辑取消，不做 axios 层物理 abort（§3.5 定稿）。
// error 只写 ref 不自动 toast——toast 决策权留给视图（保留 SensorsView silent 模式的表达力）。
export function useAsyncData<T>(loader: () => Promise<T>, opts: UseAsyncDataOptions = {}) {
  const { immediate = true } = opts
  const data = ref<T | null>(null)
  const loading = ref(false)
  const error = ref<ApiError | null>(null)

  let seq = 0
  let unmounted = false

  // 非 ApiError 的异常（视图内 throw 的普通 Error）统一归一化为 unknown kind，方便下游按 ApiError 消费
  function normalize(err: unknown): ApiError {
    if (isApiError(err)) return err
    const message = err instanceof Error && err.message ? err.message : '请求失败'
    return new ApiError(message, 'unknown')
  }

  async function run() {
    const current = ++seq
    error.value = null
    loading.value = true
    try {
      const result = await loader()
      if (current === seq && !unmounted) data.value = result
    } catch (err) {
      if (current === seq && !unmounted) error.value = normalize(err)
    } finally {
      if (current === seq && !unmounted) loading.value = false
    }
  }

  // 使在途请求全部过期并回到初始态（筛选条件重置等场景）
  function reset() {
    seq++
    data.value = null
    error.value = null
    loading.value = false
  }

  if (opts.watch?.length) {
    watch(opts.watch, () => run())
  }
  if (immediate) void run()

  onBeforeUnmount(() => {
    unmounted = true
  })

  return { data, loading, error, run, reset }
}
