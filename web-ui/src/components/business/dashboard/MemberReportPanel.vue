<template>
  <DashboardPanel
    :title="t('dashboard.teamReports')"
    :icon="Avatar"
    :meta="t('dashboard.reportsCount', { n: dayReports.length })"
    divided
  >
    <div class="date-bar">
      <el-button :icon="ArrowLeft" circle size="small" @click="shiftDate(-1)" />
      <el-date-picker v-model="selectedDate" type="date" value-format="YYYY-MM-DD" :clearable="false" />
      <el-button :icon="ArrowRight" circle size="small" @click="shiftDate(1)" />
    </div>
    <div class="card-list">
      <StateBlock
        :loading="loading && !reportsData"
        :error="error"
        :empty="!dayReports.length"
        :error-text="t('dashboard.loadReportsFailed')"
        :empty-text="t('dashboard.noReportToday')"
        @retry="run"
      >
        <div
          v-for="(report, i) in dayReports"
          :key="report.id"
          class="dash-card member-card"
          :style="stagger(i)"
          @click="router.push('/daily-reports/' + report.id)"
        >
          <div class="member-row">
            <span class="avatar">{{ initial(report) }}</span>
            <span class="member-name">{{ report.author_name || report.author_id }}</span>
            <el-icon class="card-chev"><ArrowRight /></el-icon>
          </div>
          <p class="member-summary" :class="{ empty: !report.summary }">
            {{ truncate(report.summary) || t('dashboard.noSummary') }}
          </p>
        </div>
      </StateBlock>
    </div>
  </DashboardPanel>
</template>

<script setup lang="ts">
// 首页成员日报块（结构改版 R6 §7.1 拆分）：DashboardView 日报 panel 等价平移，
// 数据经 useDashboardReports 共享单例（与综合简报同一份 listReports + selectedDate）。
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, ArrowRight, Avatar } from '@element-plus/icons-vue'
import type { DailyReport } from '@/api/logs'
import StateBlock from '@/components/base/StateBlock.vue'
import DashboardPanel from '@/components/base/DashboardPanel.vue'
import { useDashboardReports } from './useDashboardReports'

const router = useRouter()
const { t } = useI18n()
const { reportsData, loading, error, run, selectedDate, dayReports, shiftDate } = useDashboardReports()

function truncate(text: string | undefined, max = 120) {
  if (!text) return ''
  return text.length > max ? `${text.slice(0, max)}…` : text
}

function initial(report: DailyReport) {
  const name = (report.author_name || report.author_id || '?').trim()
  return name.charAt(0).toUpperCase()
}

// 卡片入场动画的交错延迟
function stagger(i: number) {
  return { animationDelay: `${i * 45}ms` }
}
</script>

<style scoped>
.card-list {
  align-content: start;
  display: grid;
  gap: var(--space-3);
  min-height: 80px;
}

.date-bar {
  align-items: center;
  display: flex;
  gap: 8px;
  margin-bottom: 14px;
}

.date-bar .el-date-editor {
  flex: 1;
}

.member-row {
  align-items: center;
  display: flex;
  gap: 10px;
}

.avatar {
  align-items: center;
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-brand-md);
  color: var(--text-inverse);
  display: inline-flex;
  flex-shrink: 0;
  font-size: 14px;
  font-weight: 700;
  height: 34px;
  justify-content: center;
  width: 34px;
}

.member-name {
  color: var(--text-1);
  font-weight: 600;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.member-row .card-chev {
  margin-left: auto;
}

.card-chev {
  color: var(--text-3);
  flex-shrink: 0;
  font-size: 14px;
  transition:
    color 0.18s ease,
    translate 0.18s ease;
}

.member-card:hover .card-chev {
  color: var(--brand-600);
  translate: 2px 0;
}

.member-summary {
  color: var(--text-2);
  display: -webkit-box;
  font-size: 13px;
  margin: 10px 0 0;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}

.member-summary.empty {
  color: var(--text-3);
}
</style>