import { ref } from 'vue'

export interface UsePaginationOptions {
  /** 每页条数，默认 10 */
  perPage?: number
}

// 通用分页状态（重构方案 §3.5）：收敛 TestDataView/IssuesView 两套手写 page/perPage/total
// 与 el-pagination 事件处理；配合后端分页响应（items + total）。视图在事件回调后自行触发 load：
// @current-change="(n) => { onCurrentChange(n); load() }"（或 watch page/perPage）。
export function usePagination(opts: UsePaginationOptions = {}) {
  const page = ref(1)
  const perPage = ref(opts.perPage ?? 10)
  const total = ref(0)
  const loading = ref(false)

  // el-pagination @current-change：页码切换
  function onCurrentChange(p: number) {
    page.value = p
  }

  // el-pagination @size-change：每页条数切换，回到第一页（对齐 IssuesView onSizeChange 现状）
  function onSizeChange(size: number) {
    perPage.value = size
    page.value = 1
  }

  function setTotal(n: number) {
    total.value = n
  }

  // 回到第一页并清空 total（筛选条件变化时用；perPage 保留）
  function reset() {
    page.value = 1
    total.value = 0
  }

  return { page, perPage, total, loading, setTotal, reset, onSizeChange, onCurrentChange }
}
