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

/** 刷新会话：旋转 access_token Cookie（仅在 SSE 401 时调用，见 connectUpdateStream） */
export function refreshSession() {
  return request<{ csrf_token?: string }>({ url: '/auth/refresh', method: 'POST' })
}

/** SSE 连接句柄 */
export interface UpdateStreamHandlers {
  onEvent: (event: SSEEvent) => void
  /** 服务端返回 401 —— 仅此时才需要刷新 token */
  onAuthError: () => void
  /** 网络/服务中断（非 401），无需刷新 token，直接按退避重连 */
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
      if (res.status === 401) {
        // token 过期 —— 唯一需要刷新 token 的场景
        handlers.onAuthError()
        return
      }
      if (!res.ok || !res.body) {
        // 409 等其它错误按网络层处理
        handlers.onNetworkError()
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
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
      handlers.onNetworkError() // 流被服务端关闭（服务重启/脚本结束但未收到 done）
    } catch (e) {
      if (!closed && (e as Error).name !== 'AbortError') handlers.onNetworkError()
    }
  })()

  return { close: () => { closed = true; controller.abort() } }
}
