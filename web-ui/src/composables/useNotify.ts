import { ElMessage } from 'element-plus'
import { i18n } from '@/i18n'
import { isApiError, type ApiErrorKind } from '@/api/client'

// kind → i18n key 显式字面量映射（禁用模板字符串拼装动态 key，规避 keys.test.ts 静态扫描盲区）
const KIND_KEYS: Record<ApiErrorKind, string> = {
  network: 'errors.network',
  auth: 'errors.auth',
  permission: 'errors.permission',
  not_found: 'errors.notFound',
  conflict: 'errors.conflict',
  validation: 'errors.validation',
  server: 'errors.server',
  unknown: 'errors.unknown'
}

// 展示后端错误信息，附带 request_id 便于追审计日志（重构方案 §3.5）：
// - ApiError（client.ts 统一包装）按 kind 输出分类文案（errors.*），permission 给统一文案（403 由路由守卫/按钮隐藏兜底）；
// - validation / unknown 保留后端 message：validation 为字段级/逐行错误文案（批量录入 422 现状依赖，行为不变）；
//   unknown 未分类到具体 kind（400 业务错误居多），后端 message 比通用 errors.unknown 更具体，应优先可见；
// - 非 ApiError（视图内 throw 的普通 Error 等）保持 err.message，无 message 时回退 fallback。
export function showApiError(err: unknown, fallback: string) {
  const e = err as (Error & { requestId?: string }) | undefined
  let message: string
  if (isApiError(e)) {
    const keepBackend = (e.kind === 'validation' || e.kind === 'unknown') && e.message
    message = keepBackend ? e.message : i18n.global.t(KIND_KEYS[e.kind])
  } else {
    message = e?.message || fallback
  }
  ElMessage.error(e?.requestId ? `${message}（request_id: ${e.requestId}）` : message)
}
