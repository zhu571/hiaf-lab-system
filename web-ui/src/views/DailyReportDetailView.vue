<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('dailyReportDetail.title') }}</h2>
      <RouterLink to="/daily-report/history"><el-button>{{ t('dailyReportDetail.backToHistory') }}</el-button></RouterLink>
    </div>
    <section v-loading="loading" class="panel">
      <template v-if="report">
        <el-descriptions border :column="isMobile ? 1 : 2" size="small">
          <el-descriptions-item :label="t('dailyReportDetail.date')">{{ report.report_date }}</el-descriptions-item>
          <el-descriptions-item :label="t('dailyReportDetail.author')">{{ report.author_name || report.author_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('dailyReportDetail.status')"><StatusBadge :value="report.content_status" /></el-descriptions-item>
          <el-descriptions-item :label="t('dailyReportDetail.summary')">{{ localized('summary').text || '-' }}</el-descriptions-item>
        </el-descriptions>
        <h3>{{ t('dailyReportDetail.rawText') }}</h3>
        <MarkdownView v-if="report.raw_text" :source="localized('raw_text').text" />
        <p v-else class="raw-none">{{ t('dailyReportDetail.none') }}</p>
        <h3>{{ t('dailyReportDetail.projectLogs') }}</h3>
        <el-table :data="report.logs || []">
          <el-table-column prop="category" :label="t('dailyReportDetail.category')" width="140" show-overflow-tooltip />
          <el-table-column :label="t('dailyReportDetail.content')" show-overflow-tooltip><template #default="{ row }">{{ resolveLocalizedText(row.content, row.translations?.content, locale).text }}</template></el-table-column>
          <el-table-column :label="t('dailyReportDetail.status')" width="120" show-overflow-tooltip>
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
import { useMobile } from '../composables/useMobile'
import StatusBadge from '@/components/base/StatusBadge.vue'
import MarkdownView from '@/components/business/MarkdownView.vue'
import { getReport, type DailyReport } from '../api/logs'
import { resolveLocalizedText } from '@/utils/contentLanguage'

const { t, locale } = useI18n()
const isMobile = useMobile()
const route = useRoute()
const report = ref<DailyReport | null>(null)
const loading = ref(false)
function localized(field: 'raw_text' | 'summary') { return resolveLocalizedText(report.value?.[field] || '', report.value?.translations?.[field], locale.value) }

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

.raw-none {
  color: var(--text-3);
  font-size: 13px;
}
</style>
