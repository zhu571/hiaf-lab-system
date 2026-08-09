import { request } from './client'

// AI 智能查询（方案 §4）：ask_history 存 rows JSONB 快照（≤256KB），
// 列表接口不含 rows 大字段，明细接口返回快照供前端还原表格（不重新查询）。

export type AskChatResponse = {
  id: string
  question: string
  answer: string
  sql: string
  table_name: string
  columns: string[]
  rows: Record<string, unknown>[]
  row_count: number
  truncated: boolean
  duration_ms: number
  created_at: string
}

export type AskHistoryItem = {
  id: string
  user_id: string
  request_id: string
  question: string
  answer: string
  sql_text: string
  table_name: string
  columns: string[]
  row_count: number
  duration_ms: number
  model: string
  created_at: string
}

export type AskHistoryDetail = AskHistoryItem & { rows: Record<string, unknown>[] }

/** 提问：Idempotency-Key 由调用方传入（显式 header 优先，拦截器只兜底生成） */
export function askChat(question: string, idempotencyKey: string) {
  return request<AskChatResponse>({
    url: '/ask/chat',
    method: 'POST',
    data: { question },
    headers: { 'Idempotency-Key': idempotencyKey }
  })
}

/** 我的问答历史列表（不含 rows 大字段） */
export function askHistory(params: { page?: number; per_page?: number } = {}) {
  return request<{ items: AskHistoryItem[]; total: number; page: number; per_page: number }>({
    url: '/ask/history',
    params
  })
}

/** 历史明细：含 rows 快照，供还原表格 */
export function askHistoryDetail(id: string) {
  return request<AskHistoryDetail>({ url: `/ask/history/${id}` })
}
