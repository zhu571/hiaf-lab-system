import { describe, it, expect, vi, beforeEach } from 'vitest'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig, type AxiosResponse } from 'axios'

// client.ts 拦截器深测（方案 §3.4）：mock 方式定稿为自定义 axios adapter（api.defaults.adapter 注入
// vi.fn，axios 官方测试接口，零新依赖，不引入 msw）。
// 可执行性前提（方案 §3.4 两条）：
// ① client.ts 模块级状态 csrfToken(:8) 与 refreshPromise(:53) 跨用例泄漏 ——
//    每用例 vi.resetModules() + 动态 import('../client') 取干净模块；
// ② redirectToLogin 的 window.location.assign(:77-79) 在 jsdom 触发 navigation 未实现异常 ——
//    用例内把 window.location 替换为 assign 打桩的对象。

type ClientModule = typeof import('../client')

async function loadClient(): Promise<ClientModule> {
  vi.resetModules()
  return import('../client')
}

/** adapter 收到的 config 已过请求拦截器（InternalAxiosRequestConfig），附重试标记 */
type AdapterConfig = InternalAxiosRequestConfig & { _retriedAfterRefresh?: boolean }

function jsonResponse(config: AdapterConfig, data: unknown, status = 200): AxiosResponse {
  return {
    data,
    status,
    statusText: status >= 200 && status < 300 ? 'OK' : 'ERR',
    headers: { 'content-type': 'application/json' },
    config
  }
}

/** 以带 response 的 AxiosError 拒绝（走 error 拦截器分支），等价于后端返回指定状态码 */
function rejectWithStatus(config: AdapterConfig, status: number, data: unknown): never {
  throw new AxiosError('Request failed with status code ' + status, 'ERR_BAD_REQUEST', config, undefined, jsonResponse(config, data, status))
}

function headerOf(config: AdapterConfig, name: string): string | undefined {
  return (config.headers as AxiosHeaders).get(name) as string | undefined
}

const assignMock = vi.fn()

beforeEach(() => {
  assignMock.mockClear()
  document.cookie = 'csrf_token=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT'
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: { pathname: '/', href: '', origin: '', assign: assignMock } as unknown as Location
  })
})

describe('请求拦截器：CSRF / 幂等键注入（:39-49）', () => {
  it('POST 自动注入一次性 Idempotency-Key（生成非空）', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) => jsonResponse(config, { data: { ok: true }, request_id: 'r1' }))
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await client.request({ url: '/todos', method: 'POST' })

    const cfg = adapter.mock.calls[0][0]
    expect(headerOf(cfg, 'Idempotency-Key')).toBeTruthy()
  })

  it('调用方显式传入的 Idempotency-Key 优先，拦截器不覆盖', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) => jsonResponse(config, { data: { ok: true }, request_id: 'r1' }))
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await client.request({ url: '/ask/chat', method: 'POST', headers: { 'Idempotency-Key': 'manual-key' } })

    expect(headerOf(adapter.mock.calls[0][0], 'Idempotency-Key')).toBe('manual-key')
  })

  it('POST 读取 csrf_token cookie 注入 X-CSRF-Token', async () => {
    document.cookie = 'csrf_token=csrf-from-cookie; path=/'
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) => jsonResponse(config, { data: { ok: true }, request_id: 'r1' }))
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await client.request({ url: '/todos', method: 'POST' })

    expect(headerOf(adapter.mock.calls[0][0], 'X-CSRF-Token')).toBe('csrf-from-cookie')
  })

  it('GET 不注入 Idempotency-Key / X-CSRF-Token（即使 cookie 存在）', async () => {
    document.cookie = 'csrf_token=csrf-from-cookie; path=/'
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) => jsonResponse(config, { data: [], request_id: 'r1' }))
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await client.request({ url: '/todos' })

    const cfg = adapter.mock.calls[0][0]
    expect(headerOf(cfg, 'Idempotency-Key')).toBeUndefined()
    expect(headerOf(cfg, 'X-CSRF-Token')).toBeUndefined()
  })
})

describe('响应拦截器：envelope 运行时校验与空 data 兜底（:98-112）', () => {
  it('正常 envelope：request 返回 data、requestWithMeta 返回 {data, requestId}', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) => jsonResponse(config, { data: ['a', 'b'], request_id: 'req-ok' }))
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await expect(client.request({ url: '/todos' })).resolves.toEqual(['a', 'b'])
    await expect(client.requestWithMeta({ url: '/todos' })).resolves.toEqual({ data: ['a', 'b'], requestId: 'req-ok' })
  })

  it('缺 data 字段 → reject「API 响应结构异常」', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) => jsonResponse(config, { request_id: 'r1' }))
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await expect(client.request({ url: '/todos' })).rejects.toThrow(/API 响应结构异常/)
  })

  it('缺 request_id 字段 → reject「API 响应结构异常」', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) => jsonResponse(config, { data: [] }))
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await expect(client.request({ url: '/todos' })).rejects.toThrow(/API 响应结构异常/)
  })

  it('data:null 兜底判定表：GET 集合末段 → []、GET 详情末段 → {}、POST → {}', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) =>
      jsonResponse(config, { data: null, request_id: 'r1' })
    )
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await expect(client.request({ url: '/projects' })).resolves.toEqual([])
    await expect(
      client.request({ url: '/experiment-runs/550e8400-e29b-41d4-a716-446655440000' })
    ).resolves.toEqual({})
    await expect(client.request({ url: '/todos', method: 'POST' })).resolves.toEqual({})
  })

  it('blob / 非 JSON 响应直接放行，不校验 envelope', async () => {
    const client = await loadClient()
    const blob = new Blob(['%PDF-fake'], { type: 'application/pdf' })
    const adapter = vi.fn(async (config: AdapterConfig) => ({
      data: blob,
      status: 200,
      statusText: 'OK',
      headers: { 'content-type': 'application/pdf' },
      config
    }))
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    const res = await client.api.request({ url: '/attachments/1/content', responseType: 'blob' })
    expect(res.data).toBeInstanceOf(Blob)
  })
})

describe('401 单飞刷新与重试（:53-80, 114-126）', () => {
  it('并发多个 401 仅触发一次 /auth/refresh，原请求各自重试成功', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) => {
      if (config.url === '/auth/refresh') return jsonResponse(config, { data: { csrf_token: 'csrf-new' }, request_id: 'r-refresh' })
      if (config._retriedAfterRefresh) return jsonResponse(config, { data: ['ok'], request_id: 'r-retry' })
      return rejectWithStatus(config, 401, { error: { message: 'unauthorized' }, request_id: 'r-401' })
    })
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    const [a, b] = await Promise.all([client.request({ url: '/todos' }), client.request({ url: '/experiences' })])

    expect(a).toEqual(['ok'])
    expect(b).toEqual(['ok'])
    const refreshCalls = adapter.mock.calls.filter(([c]) => c.url === '/auth/refresh')
    expect(refreshCalls).toHaveLength(1)
  })

  it('refresh 失败：不再重试原请求，跳转 /login', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) =>
      rejectWithStatus(config, 401, { error: { message: 'unauthorized' }, request_id: 'r-401' })
    )
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await expect(client.request({ url: '/todos' })).rejects.toMatchObject({ message: 'unauthorized' })
    expect(assignMock).toHaveBeenCalledWith('/login')
    expect(adapter.mock.calls.filter(([c]) => c.url === '/auth/refresh')).toHaveLength(1)
  })

  it('重试后再次 401：_retriedAfterRefresh 防循环，直接归一化 reject', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) => {
      if (config.url === '/auth/refresh') return jsonResponse(config, { data: { csrf_token: 'csrf-new' }, request_id: 'r-refresh' })
      return rejectWithStatus(config, 401, { error: { message: 'still-unauthorized' }, request_id: 'r-401' })
    })
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await expect(client.request({ url: '/todos' })).rejects.toMatchObject({ message: 'still-unauthorized' })
    const refreshCalls = adapter.mock.calls.filter(([c]) => c.url === '/auth/refresh')
    expect(refreshCalls).toHaveLength(1)
  })

  it('/auth/login 401（noRefresh 白名单）不触发 /auth/refresh、不重试原请求', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async (config: AdapterConfig) =>
      rejectWithStatus(config, 401, { error: { message: '用户名或密码错误' }, request_id: 'r-login-401' })
    )
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await expect(client.request({ url: '/auth/login', method: 'POST' })).rejects.toMatchObject({ message: '用户名或密码错误' })
    expect(adapter.mock.calls.filter(([c]) => c.url === '/auth/login')).toHaveLength(1)
    expect(adapter.mock.calls.filter(([c]) => c.url === '/auth/refresh')).toHaveLength(0)
    expect(assignMock).not.toHaveBeenCalled()
  })
})

describe('error 归一化（:127-133）', () => {
  it('422 details 透传：message/requestId/status/details 全部保留（批量录入逐行错误依赖）', async () => {
    const client = await loadClient()
    const details = [{ row: 2, error: 'value 必须为数字' }]
    const adapter = vi.fn(async (config: AdapterConfig) =>
      rejectWithStatus(config, 422, { error: { message: 'validation_failed', details }, request_id: 'req-422' })
    )
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    const err = (await client.request({ url: '/test-data/batch', method: 'POST' }).catch((e: unknown) => e)) as Error & {
      requestId?: string
      status?: number
      details?: unknown
    }
    expect(err.message).toBe('validation_failed')
    expect(err.requestId).toBe('req-422')
    expect(err.status).toBe(422)
    expect(err.details).toEqual(details)
  })

  it('网络错误（无 response）：message 取 error.message，空 message 兜底「请求失败」', async () => {
    const client = await loadClient()
    const adapter = vi.fn(async () => {
      throw new Error('Network Error')
    })
    client.api.defaults.adapter = adapter as unknown as typeof client.api.defaults.adapter

    await expect(client.request({ url: '/todos' })).rejects.toThrow('Network Error')

    // 空 message 的异常走兜底文案
    const adapter2 = vi.fn(async () => {
      throw new Error()
    })
    client.api.defaults.adapter = adapter2 as unknown as typeof client.api.defaults.adapter
    await expect(client.request({ url: '/todos' })).rejects.toThrow('请求失败')
  })
})
