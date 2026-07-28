<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('dailyReportDetail.title') }}</h2>
      <RouterLink to="/daily-report/history"><el-button>{{ t('dailyReportDetail.backToHistory') }}</el-button></RouterLink>
    </div>
    <section v-loading="loading" class="panel">
      <template v-if="report">
        <el-descriptions border :column="2" size="small">
          <el-descriptions-item :label="t('dailyReportDetail.date')">{{ report.report_date }}</el-descriptions-item>
          <el-descriptions-item :label="t('dailyReportDetail.author')">{{ report.author_name || report.author_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('dailyReportDetail.status')"><StatusBadge :value="report.content_status" /></el-descriptions-item>
          <el-descriptions-item :label="t('dailyReportDetail.summary')">{{ report.summary || '-' }}</el-descriptions-item>
        </el-descriptions>
        <h3>{{ t('dailyReportDetail.rawText') }}</h3>
        <pre class="raw-text">{{ report.raw_text || t('dailyReportDetail.none') }}</pre>
        <h3>{{ t('dailyReportDetail.projectLogs') }}</h3>
        <el-table :data="report.logs || []">
          <el-table-column prop="category" :label="t('dailyReportDetail.category')" width="140" />
          <el-table-column prop="content" :label="t('dailyReportDetail.content')" />
          <el-table-column :label="t('dailyReportDetail.status')" width="120">
            <template #default="{ row }">
              <StatusBadge :value="row.content_status" />
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="t('dailyReportDetail.noLogs')" />
          </template>
        </el-table>
      </template>
      <el-empty v-else-if="!loading" :description="t('dailyReportDetail.notFound')" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import StatusBadge from '../components/StatusBadge.vue'
import { getReport, type DailyReport } from '../api/logs'

const { t } = useI18n()
const route = useRoute()
const report = ref<DailyReport | null>(null)
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    report.value = await getReport(route.params.id as string)
  } catch (err) {
    showApiError(err, t('dailyReportDetail.loadFailed'))
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.panel {
  align-content: start;
  display: grid;
  gap: 14px;
}

.panel h3 {
  color: var(--text-1);
  font-size: 15px;
}

.raw-text {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--text-2);
  font-size: 13px;
  overflow: auto;
  padding: 10px;
  white-space: pre-wrap;
}
</style>
