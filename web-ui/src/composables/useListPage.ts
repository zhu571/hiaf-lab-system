import { computed, type WatchSource } from 'vue'
import { useAsyncData } from './useAsyncData'
import { usePagination } from './usePagination'

export interface ListPageParams {
  page: number
  perPage: number
}

export interface ListPageResult<T> {
  items: T[]
  total: number
}

export interface UseListPageOptions {
  perPage?: number
  immediate?: boolean
  watch?: WatchSource[]
}

/**
 * 列表页统一状态（AR-11）：items/loading/error/page/perPage/total + refresh。
 * loader 只负责按分页参数取数；翻页/改页大小会自动重新请求。
 */
export function useListPage<T>(
  loader: (params: ListPageParams) => Promise<ListPageResult<T>>,
  opts: UseListPageOptions = {}
) {
  const pagination = usePagination({ perPage: opts.perPage })
  const { data, loading, error, run, reset } = useAsyncData<T[]>(
    async () => {
      const result = await loader({ page: pagination.page.value, perPage: pagination.perPage.value })
      pagination.setTotal(result.total ?? 0)
      return result.items ?? []
    },
    { immediate: opts.immediate, watch: opts.watch }
  )

  const items = computed(() => data.value ?? [])

  async function refresh() {
    await run()
  }

  function onCurrentChange(page: number) {
    pagination.onCurrentChange(page)
    void refresh()
  }

  function onSizeChange(size: number) {
    pagination.onSizeChange(size)
    void refresh()
  }

  function resetPage() {
    pagination.reset()
    reset()
  }

  return {
    items,
    loading,
    error,
    page: pagination.page,
    perPage: pagination.perPage,
    total: pagination.total,
    refresh,
    onCurrentChange,
    onSizeChange,
    resetPage,
  }
}
