<template>
  <!-- 整块内容由 DashboardPanel 内建 StateBlock 门控：加载/错误态下横向滚动条不渲染 -->
  <DashboardPanel
    :title="t('dashboard.brief')"
    :icon="DataAnalysis"
    :meta="t('dashboard.last7Days')"
    divided
    :loading="loading && !reportsData"
    :error="error"
    :error-text="t('dashboard.loadReportsFailed')"
    @retry="run"
  >
    <!-- 简报条恒渲染 7 天卡片，无空态，仅收敛 loading/error -->
    <div class="brief-strip">
      <div
        v-for="(day, i) in briefDays"
        :key="day.date"
        class="dash-card brief-card"
        :class="{ active: day.date === selectedDate }"
        :style="stagger(i)"
        @click="selectDate(day.date)"
      >
        <div class="brief-top">
          <span class="brief-date">{{ briefDayLabel(day.date) }}</span>
          <span class="brief-count">{{ t('dashboard.peopleCount', { n: day.reports.length }) }}</span>
        </div>
        <span class="brief-week">{{ weekdayLabel(day.date) }}</span>
        <p class="brief-summary" :class="{ empty: !day.summary }">{{ day.summary || t('dashboard.noReport') }}</p>
      </div>
    </div>
  </DashboardPanel>
</template>

<script setup lang="ts">
// 首页综合简报块（结构改版 R6 §7.1 拆分）：DashboardView 简报 panel 等价平移，
// 数据经 useDashboardReports 共享单例（与成员日报同一份 listReports + selectedDate）。
import { useI18n } from 'vue-i18n'
import { DataAnalysis } from '@element-plus/icons-vue'
import DashboardPanel from '@/components/base/DashboardPanel.vue'
import { useDashboardReports } from './useDashboardReports'

const { t } = useI18n()
const { reportsData, loading, error, run, selectedDate, briefDays, briefDayLabel, weekdayLabel, selectDate } =
  useDashboardReports()

// 卡片入场动画的交错延迟
function stagger(i: number) {
  return { animationDelay: `${i * 45}ms` }
}
</script>

<style scoped>
.brief-strip {
  display: flex;
  gap: var(--space-3);
  margin: -4px -4px -12px;
  overflow-x: auto;
  padding: 4px 4px 16px;
}

.brief-card {
  display: flex;
  flex: 0 0 210px;
  flex-direction: column;
  height: 158px;
}

.brief-card.active {
  background: var(--brand-050);
  border-color: var(--brand-500);
  box-shadow: var(--shadow-brand-sm);
}

.brief-top {
  align-items: center;
  display: flex;
  justify-content: space-between;
}

.brief-date {
  color: var(--text-1);
  font-size: 15px;
  font-weight: var(--fw-semibold);
}

.brief-count {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-full);
  color: var(--text-3);
  font-size: 11px;
  padding: 1px 8px;
  transition:
    background 0.18s ease,
    color 0.18s ease;
  white-space: nowrap;
}

.brief-card.active .brief-count {
  background: var(--brand-600);
  border-color: var(--brand-600);
  color: var(--text-inverse);
}

.brief-week {
  color: var(--text-3);
  font-size: 12px;
  margin-top: 2px;
}

.brief-summary {
  color: var(--text-2);
  display: -webkit-box;
  font-size: 13px;
  line-height: 1.55;
  margin: 8px 0 0;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 4;
}

.brief-summary.empty {
  color: var(--text-3);
}

@media (max-width: 768px) {
  .brief-card {
    flex-basis: 186px;
  }
}
</style>