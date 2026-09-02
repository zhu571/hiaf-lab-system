<template>
  <!-- 列表骨架统一 base/ListPage（结构改版 R3）：toolbar + filters 槽 + StateBlock 三态 + pagination 槽 -->
  <ListPage
    :title="t('dailyHistory.title')"
    :loading="loading && !data"
    :error="error"
    :error-text="t('dailyHistory.loadFailed')"
    @retry="run"
  >
    <template #filters>
      <div class="filters">
				<el-radio-group v-if="manageableProjects.length" v-model="mode" @change="onFilter">
					<el-radio-button value="mine">{{ t('dailyHistory.mine') }}</el-radio-button>
					<el-radio-button value="team">{{ t('dailyHistory.team') }}</el-radio-button>
				</el-radio-group>
				<el-select v-if="mode === 'team'" v-model="projectId" @change="onFilter">
					<el-option v-for="project in manageableProjects" :key="project.id" :label="project.name" :value="project.id" />
				</el-select>
        <el-date-picker v-model="date" value-format="YYYY-MM-DD" type="date" :placeholder="t('dailyHistory.date')" @change="onFilter" />
        <el-select v-model="status" :placeholder="t('dailyHistory.status')" clearable @change="onFilter">
          <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
				<el-input v-if="mode === 'mine'" v-model="keyword" :placeholder="t('dailyHistory.keyword')" clearable @change="onFilter" @clear="onFilter" />
      </div>
    </template>
    <!-- 区块级三态：首屏骨架（翻页/筛选有旧数据时走表格 v-loading，骨架不闪屏）> 错误 > 内容 -->
    <ResponsiveTable :rows="reports" :loading="loading" class="clickable-table" @row-click="openDetail">
      <el-table-column :label="t('dailyHistory.date')" width="120" show-overflow-tooltip>
        <template #default="{ row }">{{ formatDate(row.report_date) }}</template>
      </el-table-column>
      <el-table-column :label="t('dailyHistory.author')" width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ row.author_name || row.author_id }}</template>
      </el-table-column>
			<el-table-column v-if="mode === 'mine'" :label="t('dailyHistory.logCount')" width="90">
				<template #default="{ row }">{{ mineLogCount(row) }}</template>
			</el-table-column>
		<el-table-column :label="mode === 'team' ? t('dailyHistory.projectLogs') : t('dailyHistory.summary')" show-overflow-tooltip><template #default="{ row }">
			<span v-if="mode === 'team'">{{ projectLogCount(row) }}</span>
			<template v-else>{{ localized(row, 'summary').text }}<small v-if="localized(row, 'summary').isFallback"> ({{ t('translation.original') }})</small></template>
		</template></el-table-column>
      <el-table-column :label="t('dailyHistory.status')" width="120" show-overflow-tooltip>
        <template #default="{ row }">
          <StatusBadge domain="reportStatus" :value="row.content_status" />
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('dailyHistory.empty')" />
      </template>
      <template #card="{ row }">
        <div class="report-card" @click="openDetail(row)">
					<span class="card-title">{{ mode === 'team' ? t('dailyHistory.logsCount', { n: projectLogCount(row) }) : (localized(row, 'summary').text || '—') }}</span>
          <div class="card-fields">
            <span>{{ formatDate(row.report_date) }}</span>
            <span>{{ row.author_name || row.author_id }}</span>
						<span v-if="mode === 'mine'">{{ t('dailyHistory.logCount') }}: {{ mineLogCount(row) }}</span>
            <StatusBadge domain="reportStatus" :value="row.content_status" />
          </div>
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
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAsyncData } from '@/composables/useAsyncData'
import { usePagination } from '@/composables/usePagination'
import ListPage from '@/components/base/ListPage.vue'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import StatusBadge from '@/components/base/StatusBadge.vue'
import { formatDate } from '@/utils/datetime'
import { listReports, listTeamReports, type DailyReport, type TeamDailyReportProjection } from '@/api/logs'
import { resolveLocalizedText } from '@/utils/contentLanguage'
import { useProjectStore } from '@/stores/project'

const { t, locale } = useI18n()
const router = useRouter()
const projects = useProjectStore()
const mode = ref<'mine' | 'team'>('mine')
const projectId = ref('')
const manageableProjects = computed(() => projects.projects.filter((p) => ['maintainer', 'owner', 'admin'].includes(p.current_user_role || '')))
const date = ref('')
const status = ref('')
const keyword = ref('')
const { page, perPage, total, onSizeChange } = usePagination({ perPage: 20 })
const statuses = computed(() => [
  { value: 'draft', label: t('dailyHistory.draft') },
  { value: 'submitted', label: t('dailyHistory.submitted') },
  { value: 'confirmed', label: t('dailyHistory.confirmed') },
  { value: 'locked', label: t('dailyHistory.locked') }
])

// 列表加载收敛到 useAsyncData（重构方案 §3.5）：内建竞态/卸载保护，替代手写 loading + try/catch；
// 加载失败不再 toast，error 交给 ListPage 内 StateBlock 展示并重试
type HistoryPage = { items: (DailyReport | TeamDailyReportProjection)[]; total: number; page: number }
const { data, loading, error, run } = useAsyncData<HistoryPage>(loadReports)
const reports = computed(() => data.value?.items ?? [])
function localized(row: DailyReport | TeamDailyReportProjection, field: 'summary') {
	const report = row as DailyReport
	return resolveLocalizedText(report[field] || '', report.translations?.[field], locale.value)
}
function projectLogCount(row: DailyReport | TeamDailyReportProjection) { return 'project_log_count' in row ? row.project_log_count : 0 }
function mineLogCount(row: DailyReport | TeamDailyReportProjection) { return 'log_count' in row ? (row.log_count ?? '—') : '—' }

function openDetail(row: DailyReport | TeamDailyReportProjection) {
	router.push(mode.value === 'team' ? `/projects/${projectId.value}/daily-reports/${row.id}` : `/daily-reports/${row.id}`)
}

// 筛选条件变化：回到第一页后重新加载（保持既有行为）
function onFilter() {
  page.value = 1
  run()
}

async function loadReports(): Promise<HistoryPage> {
  const params: Record<string, string | number> = { page: page.value, per_page: perPage.value }
  if (date.value) params.date = date.value
  if (status.value) params.status = status.value
	if (mode.value === 'mine' && keyword.value.trim()) params.keyword = keyword.value.trim()
	const res = mode.value === 'team' && projectId.value ? await listTeamReports(projectId.value, params) : await listReports(params)
  total.value = res.total ?? 0
	return res as HistoryPage
}

onMounted(async () => {
	await projects.load()
	projectId.value = manageableProjects.value[0]?.id || ''
})
</script>

<style scoped>
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.clickable-table :deep(.el-table__row) {
  cursor: pointer;
}

/* 移动端卡片与表格行一致，整卡可点进详情 */
.report-card {
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
