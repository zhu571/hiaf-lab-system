<template>
  <div class="page">
		<div class="toolbar"><h2>{{ t('dailyHistory.teamDetail') }}</h2><el-button @click="router.back()">{{ t('dailyHistory.back') }}</el-button></div>
    <StateBlock :loading="loading" :error="error" :error-text="t('dailyHistory.loadFailed')" @retry="run">
      <template v-if="report">
        <section class="panel"><el-descriptions border :column="2">
          <el-descriptions-item :label="t('dailyHistory.date')">{{ formatDate(report.report_date) }}</el-descriptions-item>
          <el-descriptions-item :label="t('dailyHistory.author')">{{ report.author_name || report.author_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('dailyHistory.status')"><StatusBadge domain="reportStatus" :value="report.content_status" /></el-descriptions-item>
          <el-descriptions-item :label="t('dailyHistory.projectLogs')">{{ report.project_log_count }}</el-descriptions-item>
        </el-descriptions></section>
        <section class="panel"><h3>{{ t('dailyHistory.projectLogs') }}</h3>
          <el-empty v-if="!report.logs?.length" :description="t('logList.empty')" />
          <el-button v-for="log in report.logs" :key="log.id" link type="primary" @click="router.push(`/logs/${log.id}`)">
            {{ formatDateTime(log.occurred_at) }} · {{ log.content }}
          </el-button>
        </section>
      </template>
    </StateBlock>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import StateBlock from '@/components/base/StateBlock.vue'
import StatusBadge from '@/components/base/StatusBadge.vue'
import { useAsyncData } from '@/composables/useAsyncData'
import { getTeamReport, type TeamDailyReportProjection } from '@/api/logs'
import { formatDate, formatDateTime } from '@/utils/datetime'
const route = useRoute(); const router = useRouter(); const { t } = useI18n()
const projectId = computed(() => String(route.params.id || ''))
const reportId = computed(() => String(route.params.reportId || ''))
const { data: report, loading, error, run } = useAsyncData<TeamDailyReportProjection>(
  () => getTeamReport(projectId.value, reportId.value), { watch: [projectId, reportId] })
</script>

<style scoped>
.panel { align-content: start; display: grid; gap: 12px; }
</style>
