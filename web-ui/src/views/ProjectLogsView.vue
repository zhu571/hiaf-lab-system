<template>
  <ListPage
    :title="t('logList.title')"
    :loading="loading && !items"
    :error="error"
    :empty="!items?.length"
    :error-text="t('logList.loadFailed')"
    :empty-text="t('logList.empty')"
    @retry="run"
  >
    <template #filters>
      <div class="filters">
        <el-select v-model="category" clearable :placeholder="t('logList.categoryAll')" @change="onFilter">
          <el-option v-for="c in LOG_CATEGORIES" :key="c" :label="categoryLabel(c)" :value="c" />
        </el-select>
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          value-format="YYYY-MM-DD"
          :start-placeholder="t('logList.dateStart')"
          :end-placeholder="t('logList.dateEnd')"
          @change="onFilter"
        />
        <el-select v-model="status" @change="onFilter">
          <el-option v-for="s in LOG_STATUSES" :key="s" :label="statusLabel(s)" :value="s" />
        </el-select>
      </div>
    </template>
    <ResponsiveTable :rows="items ?? []" :loading="loading" class="log-table" @row-click="openLog">
      <el-table-column :label="t('logList.time')" width="170">
        <template #default="{ row }">{{ formatDateTime(row.occurred_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('logList.category')" width="110">
        <template #default="{ row }">{{ categoryLabel(row.category) }}</template>
      </el-table-column>
      <el-table-column :label="t('logList.content')" min-width="240" show-overflow-tooltip>
        <template #default="{ row }">{{ contentPreview(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('logList.source')" width="90">
        <template #default="{ row }">{{ sourceLabel(row.source) }}</template>
      </el-table-column>
      <el-table-column :label="t('logList.status')" width="100">
        <template #default="{ row }">
          <StatusBadge domain="logStatus" :value="row.content_status" />
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('logList.empty')" />
      </template>
      <template #card="{ row }">
        <div class="log-card" role="button" tabindex="0" @click="openLog(row)" @keydown.enter="openLog(row)">
          <div class="card-fields">
            <span>{{ formatDateTime(row.occurred_at) }}</span>
            <span>{{ categoryLabel(row.category) }}</span>
            <span>{{ sourceLabel(row.source) }}</span>
            <StatusBadge domain="logStatus" :value="row.content_status" />
          </div>
          <p class="log-card-content">{{ contentPreview(row) }}</p>
        </div>
      </template>
    </ResponsiveTable>
    <template #pagination>
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="perPage"
        layout="total, sizes, prev, pager, next"
        :page-sizes="[20, 50, 100]"
        :total="total"
        @current-change="run"
        @size-change="(n: number) => { onSizeChange(n); run() }"
      />
    </template>
  </ListPage>
</template>

<script setup lang="ts">
// 项目日志列表页（log-view-optimization 批 W1）：补齐后端 GET /projects/{id}/logs
// 已有的 category/date_from/date_to/status/分页过滤能力在 Web 侧的入口。
// status 缺省 confirmed（对齐后端缺省语义，下拉不可清空——空 status 后端仍只回 confirmed，避免误导）。
// 日期范围为本地日期，边界转 RFC3339（date_to 取当天 23:59:59.999，后端为 <= 精确时间）。
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import ListPage from '@/components/base/ListPage.vue'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import StatusBadge from '@/components/base/StatusBadge.vue'
import { useAsyncData } from '@/composables/useAsyncData'
import { usePagination } from '@/composables/usePagination'
import { formatDateTime } from '@/utils/datetime'
import { statusMetaFor } from '@/utils/statusMeta'
import { LOG_CATEGORIES, LOG_STATUSES, logCategoryKey, logSourceKey } from '@/utils/logMeta'
import { listProjectLogs, type LogItem } from '@/api/logs'
import { resolveLocalizedText } from '@/utils/contentLanguage'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()

// 项目上下文唯一事实来源是路由参数（ProjectLayout 以 :key=projectId 切换时整页重挂载）
const projectId = computed(() => String(route.params.id || ''))

const category = ref('')
const status = ref('confirmed')
const dateRange = ref<[string, string] | null>(null)

const { page, perPage, total, onSizeChange } = usePagination({ perPage: 20 })

const { data: items, loading, error, run } = useAsyncData<LogItem[]>(async () => {
  if (!projectId.value) return []
  const params: Record<string, string | number> = { page: page.value, per_page: perPage.value, status: status.value }
  if (category.value) params.category = category.value
  if (dateRange.value?.[0]) params.date_from = new Date(`${dateRange.value[0]}T00:00:00`).toISOString()
  if (dateRange.value?.[1]) params.date_to = new Date(`${dateRange.value[1]}T23:59:59.999`).toISOString()
  const res = await listProjectLogs(projectId.value, params)
  total.value = res.total ?? 0
  return res.items ?? []
})

function onFilter() {
  page.value = 1
  run()
}

function categoryLabel(c: string) {
  const key = logCategoryKey(c)
  return key ? t(key) : c
}

function sourceLabel(s: string) {
  const key = logSourceKey(s)
  return key ? t(key) : s
}

function statusLabel(s: string) {
  const m = statusMetaFor('logStatus', s)
  return m ? t(m.labelKey) : s
}

// 正文预览：本地化文本 + 换行折叠为空格（show-overflow-tooltip 单行省略）
function contentPreview(log: LogItem) {
  return resolveLocalizedText(log.content, log.translations?.content, locale.value).text.replace(/\s+/g, ' ').trim()
}

function openLog(row: LogItem) {
  router.push(`/logs/${row.id}`)
}
</script>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.filters .el-select {
  width: 160px;
}

.log-table :deep(.el-table__row) {
  cursor: pointer;
}

.log-card {
  cursor: pointer;
  display: grid;
  gap: 8px;
}

.log-card-content {
  color: var(--text-2);
  display: -webkit-box;
  font-size: 13px;
  line-height: 1.6;
  overflow: hidden;
  overflow-wrap: anywhere;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}

@media (max-width: 768px) {
  .filters .el-select,
  .filters .el-date-editor {
    width: 100%;
  }
}
</style>
