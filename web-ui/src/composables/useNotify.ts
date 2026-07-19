import { ElMessage } from 'element-plus'

// 展示后端错误信息，附带 request_id 便于追审计日志
export function showApiError(err: unknown, fallback: string) {
  const e = err as (Error & { requestId?: string }) | undefined
  const message = e?.message || fallback
  ElMessage.error(e?.requestId ? `${message}（request_id: ${e.requestId}）` : message)
}
