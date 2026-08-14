import { onBeforeUnmount } from 'vue'

export interface UsePollingOptions {
  /** 启动时立即执行一次 fn */
  immediate?: boolean
  /** document.hidden 时暂停；恢复可见时立即执行一次再恢复定时（默认 true） */
  pauseOnHidden?: boolean
  /** 恢复可见时是否立即执行一次 fn（默认 true；SensorsView history 传 false 保持「恢复只刷 latest」现状） */
  resumeImmediate?: boolean
}

// 轮询封装（重构方案 §3.5）：setInterval + visibilitychange 暂停/恢复 + unmount 清理，
// 泛化 AppLayout 徽章轮询（:107-150）与 SensorsView 双定时器（:214-245）两处现状。
// fn 内部自决错误策略（现状徽章静默降级保留），本 composable 不吞异常、不 toast。
export function usePolling(fn: () => void | Promise<void>, ms: number, opts: UsePollingOptions = {}) {
  const { immediate = false, pauseOnHidden = true, resumeImmediate = true } = opts
  let timer: ReturnType<typeof setInterval> | undefined
  let running = false
  let hiddenPaused = false

  function clearTimer() {
    if (timer !== undefined) {
      clearInterval(timer)
      timer = undefined
    }
  }

  function onVisibilityChange() {
    if (document.hidden) {
      if (!hiddenPaused) {
        hiddenPaused = true
        clearTimer()
      }
    } else if (hiddenPaused) {
      hiddenPaused = false
      if (running) {
        // 恢复可见立即刷新一次（对齐 AppLayout「恢复即刷新」与 SensorsView「恢复刷 latest」语义）
        if (resumeImmediate) void fn()
        schedule()
      }
    }
  }

  function schedule() {
    if (timer !== undefined) return
    timer = setInterval(() => {
      // 兜底守卫：start 于隐藏页 / 恢复竞态下不执行
      if (!(pauseOnHidden && document.hidden)) void fn()
    }, ms)
  }

  function start() {
    if (running) return
    running = true
    hiddenPaused = pauseOnHidden && document.hidden
    if (pauseOnHidden) document.addEventListener('visibilitychange', onVisibilityChange)
    if (immediate) void fn()
    schedule()
  }

  function stop() {
    if (!running) return
    running = false
    hiddenPaused = false
    clearTimer()
    if (pauseOnHidden) document.removeEventListener('visibilitychange', onVisibilityChange)
  }

  onBeforeUnmount(stop)

  return { start, stop, isRunning: () => running }
}
