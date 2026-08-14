import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ElMessage } from 'element-plus'
import { showApiError } from '../useNotify'

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
