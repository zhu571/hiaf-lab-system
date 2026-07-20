<template>
  <div class="page">
    <div class="toolbar">
      <h2>日报历史</h2>
    </div>
    <section class="panel filters-panel">
      <div class="filters">
        <el-date-picker v-model="date" value-format="YYYY-MM-DD" type="date" placeholder="日期" @change="load" />
        <el-select v-model="status" placeholder="状态" clearable @change="load">
          <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
        <el-input v-model="keyword" placeholder="关键词（摘要/正文）" clearable @change="load" @clear="load" />
      </div>
    </section>
    <section class="panel">
      <el-table v-loading="loading" :data="reports" class="clickable-table" @row-click="openDetail">
        <el-table-column prop="report_date" label="日期" width="120" />
        <el-table-column label="作者" width="140">
          <template #default="{ row }">{{ row.author_name || row.author_id }}</template>
        </el-table-column>
        <el-table-column prop="summary" label="摘要" />
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <StatusBadge :value="row.content_status" />
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无日报记录" />
        </template>
      </el-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import StatusBadge from '../components/StatusBadge.vue'
import { listReports, type DailyReport } from '../api/logs'

const router = useRouter()
const date = ref('')
const status = ref('')
const keyword = ref('')
const reports = ref<DailyReport[]>([])
const loading = ref(false)
const statuses = [
  { value: 'draft', label: '草稿' },
  { value: 'submitted', label: '已提交' },
  { value: 'confirmed', label: '已确认' },
  { value: 'locked', label: '已锁定' }
]

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
    ElMessage.error(err instanceof Error ? err.message : '日报加载失败')
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
