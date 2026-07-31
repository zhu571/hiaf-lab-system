import { request, requestWithMeta } from './client'

export interface VersionInfo {
  current: string
  current_short: string
  latest: string
  latest_short: string
  behind: number
  can_update: boolean
}

export interface UpdateTriggerResult {
  session_id: string
  current: string
}

export interface SSELineEvent {
  seq: number
  ts: string
  type: 'line'
  text: string
}

export interface SSEStepEvent {
  seq: number
  ts: string
  type: 'step'
  step: number
  step_total: number
  title: string
}

export interface SSEDoneEvent {
  seq: number
  ts: string
  type: 'done'
  exit_code: number
  success: boolean
  old_sha: string
  new_sha: string
}

export interface SSEErrorEvent {
  seq: number
  ts: string
  type: 'error'
  message: string
}

export type SSEEvent = SSELineEvent | SSEStepEvent | SSEDoneEvent | SSEErrorEvent

/** 获取版本信息 */
export function getVersion() {
  return request<VersionInfo>({ url: '/admin/system/version' })
}

/** 触发系统更新，返回 session_id */
export function triggerUpdate() {
  return requestWithMeta<UpdateTriggerResult>({
    url: '/admin/system/update',
    method: 'POST'
  })
}

/** SSE 连接句柄 */
export interface UpdateStreamHandlers {
  onEvent: (event: SSEEvent) => void
  /** 服务端返回 401 —— 仅此时才需要刷新 token */
  onAuthError: () => void
  /** HTTP 层错误（非 401、非 2xx）：如 404 session 不存在、409 订阅者过多。调用方按状态码决策，不当作网络错误盲重连 */
  onHttpError: (status: number) => void
  /** 网络/服务中断（非 HTTP 层错误），无需刷新 token，直接按退避重连 */
  onNetworkError: () => void
}

/**
 * 建立 SSE 连接，返回断开函数
 *  用 fetch + ReadableStream 而非 EventSource，原因：
 *  1. 需要识别 HTTP 状态码 —— 401（token 过期）只在这一种情况下才刷新 token；
 *     EventSource 的 onerror 拿不到状态码，无法区分"401"与"网络断"；
 *  2. 不依赖 EventSource 固定 3s 自动重连（无法退避），
 *     断开后由调用方 close() 并自行指数退避重连（见 SettingsView.connectStream）。
 *  Cookie 由 credentials: 'include' 自动携带（HttpOnly access_token），无需手动加头。
 */
export function connectUpdateStream(
  sessionId: string,
  handlers: UpdateStreamHandlers
): { close: () => void } {
  const controller = new AbortController()
  let closed = false

  void (async () => {
    try {
      const res = await fetch(`/api/v1/admin/system/update/stream/${sessionId}`, {
        headers: { Accept: 'text/event-stream' },
        credentials: 'include',
        signal: controller.signal
      })
      if (closed) return // 等待响应期间已被 unmount/close，不再触发任何回调
      if (res.status === 401) {
        // token 过期 —— 唯一需要刷新 token 的场景
        controller.abort() // 终止本次连接，交由调用方刷新 token 后重连
        handlers.onAuthError()
        return
      }
      if (!res.ok || !res.body) {
        // 404 session 不存在 / 409 订阅者过多等：交给 onHttpError 按状态码决策，
        // 404 不应盲重连（session 已不存在），409 也非网络层问题。
        handlers.onHttpError(res.status)
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      try {
        while (!closed) {
          const { done, value } = await reader.read()
          if (done) break
          buf += decoder.decode(value, { stream: true })
          let idx
          while ((idx = buf.indexOf('\n\n')) >= 0) {
            // SSE 帧以空行分隔
            const frame = buf.slice(0, idx)
            buf = buf.slice(idx + 2)
            const dataLine = frame.split('\n').find((l) => l.startsWith('data:'))
            if (!dataLine) continue // 跳过 keepalive 注释帧 / id: / event: 行
            try {
              handlers.onEvent(JSON.parse(dataLine.slice(5).trim()) as SSEEvent)
            } catch {
              /* 忽略解析失败的帧 */
            }
          }
        }
      } catch (e) {
        if (!closed && (e as Error).name !== 'AbortError') handlers.onNetworkError()
        return
      }
      if (!closed) handlers.onNetworkError() // 流被服务端关闭（服务重启/脚本结束但未收到 done）
    } catch (e) {
      if (!closed && (e as Error).name !== 'AbortError') handlers.onNetworkError()
    }
  })()

  return { close: () => { closed = true; controller.abort() } }
}
