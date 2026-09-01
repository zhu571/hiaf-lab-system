<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('dailyReportDetail.title') }}</h2>
      <RouterLink to="/daily-report/history"><el-button>{{ t('dailyReportDetail.backToHistory') }}</el-button></RouterLink>
    </div>
    <StateBlock
      :loading="loading"
      :error="error && !notFound ? error : null"
      :error-text="t('dailyReportDetail.loadFailed')"
      @retry="run"
    >
      <el-empty v-if="notFound" :description="t('dailyReportDetail.notFound')" />
      <template v-else-if="report">
        <section class="panel">
          <el-descriptions border :column="isMobile ? 1 : 2" size="small">
            <el-descriptions-item :label="t('dailyReportDetail.date')">{{ report.report_date }}</el-descriptions-item>
            <el-descriptions-item :label="t('dailyReportDetail.author')">{{ report.author_name || report.author_id }}</el-descriptions-item>
            <el-descriptions-item :label="t('dailyReportDetail.status')"><StatusBadge domain="reportStatus" :value="report.content_status" /></el-descriptions-item>
            <el-descriptions-item :label="t('dailyReportDetail.qualityStatus')"><StatusBadge domain="reportQuality" :value="report.quality_status" /></el-descriptions-item>
            <el-descriptions-item :label="t('dailyReportDetail.summary')" :span="isMobile ? 1 : 2">{{ localized('summary').text || '-' }}</el-descriptions-item>
          </el-descriptions>
        </section>
        <section class="panel">
          <h3>{{ t('dailyReportDetail.rawText') }}</h3>
          <MarkdownView v-if="report.raw_text" :source="localized('raw_text').text" />
          <p v-else class="raw-none">{{ t('dailyReportDetail.none') }}</p>
        </section>
        <section class="panel">
          <h3>{{ t('dailyReportDetail.projectLogs') }}</h3>
          <ResponsiveTable :rows="report.logs || []" class="log-table" @row-click="openLog">
            <el-table-column :label="t('dailyReportDetail.occurredAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.occurred_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('dailyReportDetail.category')" width="110">
              <template #default="{ row }">{{ categoryLabel(row.category) }}</template>
            </el-table-column>
            <el-table-column :label="t('dailyReportDetail.content')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ resolveLocalizedText(row.content, row.translations?.content, locale).text }}</template>
            </el-table-column>
            <el-table-column :label="t('dailyReportDetail.project')" width="140">
              <template #default="{ row }">
                <el-button link type="primary" @click.stop="goProject(row.project_id)">{{ projectName(row.project_id) }}</el-button>
              </template>
            </el-table-column>
            <el-table-column :label="t('dailyReportDetail.status')" width="110" show-overflow-tooltip>
              <template #default="{ row }">
                <StatusBadge domain="logStatus" :value="row.content_status" />
              </template>
            </el-table-column>
            <el-table-column :label="t('dailyReportDetail.actions')" width="90">
              <template #default="{ row }">
                <el-button link type="primary" @click.stop="openLog(row)">{{ t('dailyReportDetail.detail') }}</el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="t('dailyReportDetail.noLogs')" />
            </template>
            <template #card="{ row }">
              <div class="log-card" role="button" tabindex="0" @click="openLog(row)" @keydown.enter="openLog(row)">
                <div class="card-fields">
                  <span>{{ formatDateTime(row.occurred_at) }}</span>
                  <span>{{ categoryLabel(row.category) }}</span>
                  <StatusBadge domain="logStatus" :value="row.content_status" />
                </div>
                <p class="log-card-content">{{ resolveLocalizedText(row.content, row.translations?.content, locale).text }}</p>
                <div class="card-actions">
                  <el-button size="small" link type="primary" @click.stop="goProject(row.project_id)">{{ projectName(row.project_id) }}</el-button>
                </div>
              </div>
            </template>
          </ResponsiveTable>
        </section>
        <section class="panel">
          <h3>{{ t('dailyReportDetail.attachments') }}</h3>
          <AttachmentList entity-type="daily_report" :entity-id="report.id" />
        </section>
      </template>
    </StateBlock>
  </div>
</template>

<script setup lang="ts">
// 日报详情页（log-view-optimization 批 W3 修复）：
// - 加载三态改 StateBlock：仅 404 归 notFound 空态，403/网络/5xx 走错误态 + 重试（不再混为一谈）；
// - 关联日志换 ResponsiveTable（移动端卡片降级），补发生时间/项目跳转/详情动作列，行点击进日志详情；
// - 状态徽标显式 domain（reportStatus/logStatus/reportQuality），补 quality_status 展示；
// - 附件区经 AttachmentList 查询服务端已上传附件（entity_type=daily_report）。
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import StatusBadge from '@/components/base/StatusBadge.vue'
import StateBlock from '@/components/base/StateBlock.vue'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import MarkdownView from '@/components/business/MarkdownView.vue'
import AttachmentList from '@/components/business/AttachmentList.vue'
import { useAsyncData } from '@/composables/useAsyncData'
import { useMobile } from '@/composables/useMobile'
import { formatDateTime } from '@/utils/datetime'
import { logCategoryKey } from '@/utils/logMeta'
import { getReport, type DailyReport, type LogItem } from '@/api/logs'
import { resolveLocalizedText } from '@/utils/contentLanguage'
import { useProjectStore } from '@/stores/project'

const { t, locale } = useI18n()
const isMobile = useMobile()
const route = useRoute()
const router = useRouter()
const projects = useProjectStore()

const { data: report, loading, error, run } = useAsyncData<DailyReport>(
  async () => {
    // 项目名展示依赖项目列表缓存；加载失败不阻塞日报本体渲染（回退显示 ID）
    try {
      await projects.load()
    } catch {
      // 项目列表失败仅影响项目名展示
    }
    return getReport(route.params.id as string)
  },
  { watch: [() => route.params.id] }
)

const notFound = computed(() => error.value?.kind === 'not_found')

function localized(field: 'raw_text' | 'summary') {
  return resolveLocalizedText(report.value?.[field] || '', report.value?.translations?.[field], locale.value)
}

function categoryLabel(c: string) {
  const key = logCategoryKey(c)
  return key ? t(key) : c
}

function projectName(id: string) {
  return projects.projects.find((p) => p.id === id)?.name || id
}

function openLog(row: LogItem) {
  router.push(`/logs/${row.id}`)
}

function goProject(id: string) {
  if (id) router.push(`/projects/${id}/logs`)
}
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
</style>
