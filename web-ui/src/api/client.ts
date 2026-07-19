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

api.interceptors.response.use(
  (response) => response,
  (error) => {
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
