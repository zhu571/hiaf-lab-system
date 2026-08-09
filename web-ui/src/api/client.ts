import axios, { AxiosHeaders, type AxiosRequestConfig } from 'axios'

type Envelope<T> = {
  data: T
  request_id: string
}

let csrfToken = ''

export function setCSRFToken(token: string) {
  csrfToken = token
}

function csrfFromCookie() {
  return document.cookie
    .split('; ')
    .find((item) => item.startsWith('csrf_token='))
    ?.split('=')
    .slice(1)
    .join('=')
}

export function newIdempotencyKey() {
  // crypto.randomUUID 仅在安全上下文（HTTPS/localhost）可用，内网 HTTP 部署时需要兜底
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export const api = axios.create({
  baseURL: '/api/v1',
  withCredentials: true,
  headers: {
    'Content-Type': 'application/json'
  }
})

api.interceptors.request.use((config) => {
  config.headers = AxiosHeaders.from(config.headers)
  const method = (config.method || 'get').toUpperCase()
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    // 调用方显式传入的 Idempotency-Key 优先（如 askChat 幂等键），拦截器只兜底自动生成
    if (!config.headers.get('Idempotency-Key')) config.headers.set('Idempotency-Key', newIdempotencyKey())
    csrfToken = decodeURIComponent(csrfFromCookie() || '')
    if (csrfToken) config.headers.set('X-CSRF-Token', csrfToken)
  }
  return config
})

// access token 只有 15 分钟，过期后先单飞刷新再原样重试一次；
// 刷新也失败说明会话已失效，整页跳回登录（会清空内存态，由路由守卫重新鉴权）。
let refreshPromise: Promise<boolean> | null = null

function refreshSession(): Promise<boolean> {
  refreshPromise ??= api
    .post('/auth/refresh', {})
    .then((res) => {
      const token = res.data?.data?.csrf_token
      if (token) setCSRFToken(token)
      return true
    })
    .catch(() => false)
    .finally(() => {
      refreshPromise = null
    })
  return refreshPromise
}

/** 单飞刷新 access_token：axios 401 重试与 SSE 401 恢复共用同一个 Promise，
 *  避免并发 401 触发多次 refresh；成功时同步更新 CSRF token。 */
export function refreshAuthSession(): Promise<boolean> {
  return refreshSession()
}

function redirectToLogin() {
  if (window.location.pathname !== '/login') {
    window.location.assign('/login')
  }
}

// C15 运行时校验（轻量结构断言，不上 zod）：成功响应必须是 { data, request_id }
// envelope；data 为 null/undefined（Go nil slice 序列化为 null）时按请求形态兜底——
// GET 集合端点给 []（列表防崩，替代 store 层散落补丁），其余给 {}（详情字段渲染为空）。
// 非 JSON 端点直接放行：附件 blob 下载（responseType/Content-Type 判断）；
// SSE 流（system.ts fetch、GasControlView EventSource）与静态资源本就不经此 axios 实例。
const COLLECTION_SEGMENT = /^[a-z]+$/

function isCollectionRequest(config?: AxiosRequestConfig): boolean {
  if ((config?.method || 'get').toUpperCase() !== 'GET') return false
  const segments = (config?.url || '').split('?')[0].split('/').filter(Boolean)
  const last = segments[segments.length - 1] || ''
  // 纯小写字母末段视为集合（projects/todos/whitelist/members…）；
  // UUID/含数字/含连字符的末段（by-date、{id} 等）按详情处理。
  return COLLECTION_SEGMENT.test(last)
}

api.interceptors.response.use(
  (response) => {
    const contentType = String(response.headers?.['content-type'] ?? '')
    if (response.config.responseType === 'blob' || !contentType.includes('application/json')) {
      return response
    }
    const body = response.data
    if (!body || typeof body !== 'object' || !('data' in body) || typeof body.request_id !== 'string') {
      return Promise.reject(new Error(`API 响应结构异常（缺 data/request_id）：${response.config.url}`))
    }
    if (body.data === null || body.data === undefined) {
      body.data = isCollectionRequest(response.config) ? [] : {}
      console.warn(`[api] ${response.config.url} 返回空 data，已兜底`, body.data)
    }
    return response
  },
  async (error) => {
    const config = error.config as (AxiosRequestConfig & { _retriedAfterRefresh?: boolean }) | undefined
    const url = config?.url ?? ''
    // 仅 /auth/login、/auth/refresh 的 401 是预期业务响应（密码错误/刷新失效），不参与刷新重试；
    // 其它 /auth/*（me/change-password/profile 等受保护端点）token 过期时同样参与单飞 refresh 后原样重试。
    const noRefresh = ['/auth/login', '/auth/refresh'].includes(url)
    if (error.response?.status === 401 && config && !noRefresh && !config._retriedAfterRefresh) {
      config._retriedAfterRefresh = true
      if (await refreshSession()) {
        return api.request(config)
      }
      redirectToLogin()
    }
    const message = error.response?.data?.error?.message || error.message || '请求失败'
    const err = new Error(message) as Error & { requestId?: string; status?: number }
    err.requestId = error.response?.data?.request_id
    err.status = error.response?.status
    return Promise.reject(err)
  }
)

export async function request<T>(config: AxiosRequestConfig) {
  const response = await api.request<Envelope<T>>(config)
  return response.data.data
}

export async function requestWithMeta<T>(config: AxiosRequestConfig) {
  const response = await api.request<Envelope<T>>(config)
  return { data: response.data.data, requestId: response.data.request_id }
}
