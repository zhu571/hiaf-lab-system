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
    config.headers.set('Idempotency-Key', newIdempotencyKey())
    csrfToken ||= decodeURIComponent(csrfFromCookie() || '')
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

function redirectToLogin() {
  if (window.location.pathname !== '/login') {
    window.location.assign('/login')
  }
}

api.interceptors.response.use(
  (response) => response,
  async (error) => {
    const config = error.config as (AxiosRequestConfig & { _retriedAfterRefresh?: boolean }) | undefined
    const url = config?.url ?? ''
    // /auth/* 自身 401（登录密码错、refresh 失效）直接抛给调用方，不参与刷新重试
    if (error.response?.status === 401 && config && !url.startsWith('/auth/') && !config._retriedAfterRefresh) {
      config._retriedAfterRefresh = true
      if (await refreshSession()) {
        return api.request(config)
      }
      redirectToLogin()
    }
    const message = error.response?.data?.error?.message || error.message || '请求失败'
    const err = new Error(message) as Error & { requestId?: string }
    err.requestId = error.response?.data?.request_id
    return Promise.reject(err)
  }
)

export async function request<T>(config: AxiosRequestConfig) {
  const response = await api.request<Envelope<T>>(config)
  return response.data.data
}
