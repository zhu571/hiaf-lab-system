<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('dailyHistory.title') }}</h2>
    </div>
    <section class="panel filters-panel">
      <div class="filters">
        <el-date-picker v-model="date" value-format="YYYY-MM-DD" type="date" :placeholder="t('dailyHistory.date')" @change="onFilter" />
        <el-select v-model="status" :placeholder="t('dailyHistory.status')" clearable @change="onFilter">
          <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
        <el-input v-model="keyword" :placeholder="t('dailyHistory.keyword')" clearable @change="onFilter" @clear="onFilter" />
      </div>
    </section>
    <section class="panel">
      <el-table v-loading="loading" :data="reports" class="clickable-table" @row-click="openDetail">
        <el-table-column prop="report_date" :label="t('dailyHistory.date')" width="120" show-overflow-tooltip />
        <el-table-column :label="t('dailyHistory.author')" width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.author_name || row.author_id }}</template>
        </el-table-column>
        <el-table-column prop="summary" :label="t('dailyHistory.summary')" show-overflow-tooltip />
        <el-table-column :label="t('dailyHistory.status')" width="120" show-overflow-tooltip>
          <template #default="{ row }">
            <StatusBadge :value="row.content_status" />
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="t('dailyHistory.empty')" />
        </template>
      </el-table>
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="perPage"
        class="pager"
        layout="total, sizes, prev, pager, next"
        :page-sizes="[20, 50, 100]"
        :total="total"
        @current-change="load"
        @size-change="onFilter"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import StatusBadge from '@/components/base/StatusBadge.vue'
import { listReports, type DailyReport } from '../api/logs'

const { t } = useI18n()
const router = useRouter()
const date = ref('')
const status = ref('')
const keyword = ref('')
const reports = ref<DailyReport[]>([])
const loading = ref(false)
const page = ref(1)
const perPage = ref(20)
const total = ref(0)
const statuses = computed(() => [
  { value: 'draft', label: t('dailyHistory.draft') },
  { value: 'submitted', label: t('dailyHistory.submitted') },
  { value: 'confirmed', label: t('dailyHistory.confirmed') },
  { value: 'locked', label: t('dailyHistory.locked') }
])

onMounted(load)

function openDetail(row: DailyReport) {
  router.push(`/daily-reports/${row.id}`)
}

function onFilter() {
  page.value = 1
  load()
}

async function load() {
  loading.value = true
  try {
    const params: Record<string, string | number> = { page: page.value, per_page: perPage.value }
    if (date.value) params.date = date.value
    if (status.value) params.status = status.value
    if (keyword.value.trim()) params.keyword = keyword.value.trim()
    const data = await listReports(params)
    reports.value = data.items ?? []
    total.value = data.total ?? 0
  } catch (err) {
    showApiError(err, t('dailyHistory.loadFailed'))
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.filters-panel {
  padding: 14px 20px;
}

.pager {
  justify-content: flex-end;
  margin-top: 14px;
}

.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.clickable-table :deep(.el-table__row) {
  cursor: pointer;
}

.filters .el-input {
  max-width: 240px;
}

.filters .el-select {
  width: 160px;
}

@media (max-width: 768px) {
  .filters .el-input,
  .filters .el-select,
  .filters .el-date-editor {
    max-width: none;
    width: 100%;
  }
}
</style>
