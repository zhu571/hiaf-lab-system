import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ElMessage } from 'element-plus'
import { showApiError } from '../useNotify'
import { ApiError } from '@/api/client'

// ElMessage 由 test-utils/setup.ts 的 element-plus 部分 mock 打桩（组件真实、消息两件套为 vi.fn）
const errorMock = (ElMessage as unknown as { error: ReturnType<typeof vi.fn> }).error

describe('showApiError 后端错误展示', () => {
  beforeEach(() => {
    errorMock.mockClear()
  })

  it('带 requestId：拼接 request_id 便于追审计日志', () => {
    showApiError(Object.assign(new Error('密码错误'), { requestId: 'req_0001' }), 'fallback')
    expect(errorMock).toHaveBeenCalledWith('密码错误（request_id: req_0001）')
  })

  it('无 requestId：展示裸消息；err 无 message 时回退 fallback', () => {
    showApiError(new Error('网络错误'), 'fallback')
    expect(errorMock).toHaveBeenCalledWith('网络错误')

    showApiError(undefined, '兜底消息')
    expect(errorMock).toHaveBeenCalledWith('兜底消息')
  })
})

describe('showApiError 按 ApiError.kind 分文案（S2，§3.5）', () => {
  beforeEach(() => {
    errorMock.mockClear()
  })

  it('kind=network：输出 errors.network 文案 + request_id', () => {
    showApiError(new ApiError('Network Error', 'network', { requestId: 'req_0002' }), 'fallback')
    expect(errorMock).toHaveBeenCalledWith('网络连接失败，请检查网络后重试（request_id: req_0002）')
  })

  it('kind=auth / permission / server：分别输出对应分类文案', () => {
    showApiError(new ApiError('unauthorized', 'auth'), 'fallback')
    expect(errorMock).toHaveBeenCalledWith('登录状态已失效，请重新登录')

    showApiError(new ApiError('forbidden', 'permission'), 'fallback')
    expect(errorMock).toHaveBeenCalledWith('没有权限执行此操作')

    showApiError(new ApiError('boom', 'server'), 'fallback')
    expect(errorMock).toHaveBeenCalledWith('服务器开小差了，请稍后重试')
  })

  it('kind=validation：保留后端 message（字段级/逐行错误文案最精确，行为不变）', () => {
    showApiError(new ApiError('用户名长度需为 2-32 个字符', 'validation'), 'fallback')
    expect(errorMock).toHaveBeenCalledWith('用户名长度需为 2-32 个字符')
  })

  it('kind=unknown：后端 message 存在时优先展示具体 message（400 业务错误可见）', () => {
    showApiError(new ApiError('设备状态不允许该操作', 'unknown', { requestId: 'req_0400' }), 'fallback')
    expect(errorMock).toHaveBeenCalledWith('设备状态不允许该操作（request_id: req_0400）')
  })
})
