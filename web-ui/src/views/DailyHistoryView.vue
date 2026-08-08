<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('dailyHistory.title') }}</h2>
    </div>
    <section class="panel filters-panel">
      <div class="filters">
        <el-date-picker v-model="date" value-format="YYYY-MM-DD" type="date" :placeholder="t('dailyHistory.date')" @change="load" />
        <el-select v-model="status" :placeholder="t('dailyHistory.status')" clearable @change="load">
          <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
        <el-input v-model="keyword" :placeholder="t('dailyHistory.keyword')" clearable @change="load" @clear="load" />
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
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import StatusBadge from '../components/StatusBadge.vue'
import { listReports, type DailyReport } from '../api/logs'

const { t } = useI18n()
const router = useRouter()
const date = ref('')
const status = ref('')
const keyword = ref('')
const reports = ref<DailyReport[]>([])
const loading = ref(false)
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

async function load() {
  loading.value = true
  try {
    const params: Record<string, string | number> = { per_page: 100 }
    if (date.value) params.date = date.value
    if (status.value) params.status = status.value
    if (keyword.value.trim()) params.keyword = keyword.value.trim()
    const data = await listReports(params)
    reports.value = data.items ?? []
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
