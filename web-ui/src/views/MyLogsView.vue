<template>
  <ListPage :title="t('logList.myTitle')" :loading="loading && !items" :error="error" :empty="!items?.length"
    :error-text="t('logList.loadFailed')" :empty-text="t('logList.empty')" @retry="run">
    <template #filters>
      <div class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('logList.keyword')" @keyup.enter="onFilter" @clear="onFilter" />
        <el-select v-model="category" clearable :placeholder="t('logList.categoryAll')" @change="onFilter">
          <el-option v-for="c in LOG_CATEGORIES" :key="c" :label="categoryLabel(c)" :value="c" />
        </el-select>
        <el-date-picker v-model="dateRange" type="daterange" value-format="YYYY-MM-DD"
          :start-placeholder="t('logList.dateStart')" :end-placeholder="t('logList.dateEnd')" @change="onFilter" />
        <el-select v-model="status" @change="onFilter">
          <el-option v-for="s in LOG_STATUSES" :key="s" :label="statusLabel(s)" :value="s" />
        </el-select>
      </div>
    </template>
    <ResponsiveTable :rows="items ?? []" :loading="loading" class="log-table" @row-click="openLog">
      <el-table-column :label="t('logList.project')" width="150">
        <template #default="{ row }"><el-button link type="primary" @click.stop="openProject(row)">{{ row.project_code || row.project_name }}</el-button></template>
      </el-table-column>
      <el-table-column :label="t('logList.time')" width="170"><template #default="{ row }">{{ formatDateTime(row.occurred_at) }}</template></el-table-column>
      <el-table-column :label="t('logList.category')" width="110"><template #default="{ row }">{{ categoryLabel(row.category) }}</template></el-table-column>
      <el-table-column prop="content" :label="t('logList.content')" min-width="240" show-overflow-tooltip />
      <el-table-column :label="t('logList.status')" width="100"><template #default="{ row }"><StatusBadge domain="logStatus" :value="row.content_status" /></template></el-table-column>
      <template #card="{ row }"><div class="log-card" role="button" tabindex="0" @click="openLog(row)" @keydown.enter="openLog(row)">
        <el-button link type="primary" @click.stop="openProject(row)">{{ row.project_code || row.project_name }}</el-button>
        <span>{{ formatDateTime(row.occurred_at) }} · {{ categoryLabel(row.category) }}</span><p>{{ row.content }}</p>
      </div></template>
    </ResponsiveTable>
    <template #pagination><el-pagination v-model:current-page="page" v-model:page-size="perPage" layout="total, sizes, prev, pager, next"
      :page-sizes="[20, 50, 100]" :total="total" @current-change="run" @size-change="(n: number) => { onSizeChange(n); run() }" /></template>
  </ListPage>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import ListPage from '@/components/base/ListPage.vue'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import StatusBadge from '@/components/base/StatusBadge.vue'
import { useAsyncData } from '@/composables/useAsyncData'
import { usePagination } from '@/composables/usePagination'
import { formatDateTime } from '@/utils/datetime'
import { statusMetaFor } from '@/utils/statusMeta'
import { LOG_CATEGORIES, LOG_STATUSES, logCategoryKey } from '@/utils/logMeta'
import { listMyLogs, type LogItem } from '@/api/logs'

const router = useRouter()
const { t } = useI18n()
const keyword = ref('')
const category = ref('')
const status = ref('confirmed')
const dateRange = ref<[string, string] | null>(null)
const { page, perPage, total, onSizeChange } = usePagination({ perPage: 20 })
const { data: items, loading, error, run } = useAsyncData<LogItem[]>(async () => {
  const params: Record<string, string | number> = { page: page.value, per_page: perPage.value, status: status.value }
  if (keyword.value) params.keyword = keyword.value
  if (category.value) params.category = category.value
  if (dateRange.value?.[0]) params.date_from = new Date(`${dateRange.value[0]}T00:00:00`).toISOString()
  if (dateRange.value?.[1]) params.date_to = new Date(`${dateRange.value[1]}T23:59:59.999`).toISOString()
  const result = await listMyLogs(params)
  total.value = result.total
  return result.items ?? []
})
function onFilter() { page.value = 1; run() }
function categoryLabel(value: string) { const key = logCategoryKey(value); return key ? t(key) : value }
function statusLabel(value: string) { const meta = statusMetaFor('logStatus', value); return meta ? t(meta.labelKey) : value }
function openLog(row: LogItem) { router.push(`/logs/${row.id}`) }
function openProject(row: LogItem) { router.push(`/projects/${row.project_id}/logs`) }
</script>

<style scoped>
.filters { display: flex; flex-wrap: wrap; gap: 10px; }
.filters > * { width: 180px; }
.log-table :deep(.el-table__row), .log-card { cursor: pointer; }
.log-card { display: grid; gap: 8px; }
.log-card p { color: var(--text-2); margin: 0; overflow-wrap: anywhere; }
@media (max-width: 768px) { .filters > * { width: 100%; } }
</style>
