<template>
  <div class="page dashboard">
    <!-- 工作台头条（R6 §7.1）：问候 + 日期 + 快捷操作（按角色过滤） -->
    <section class="panel workspace-banner">
      <div class="banner-main">
        <h2 class="greeting-title">{{ greeting }}</h2>
        <p class="banner-date">{{ bannerDate }}</p>
      </div>
      <div class="banner-actions">
        <el-button
          v-for="action in quickActions"
          :key="action.key"
          :type="action.key === 'todo' ? 'primary' : 'default'"
          :icon="action.icon"
          @click="action.run()"
        >
          {{ t(action.labelKey) }}
        </el-button>
      </div>
    </section>

    <!-- 12 列网格：待办 span7 + 设备 span5 / 简报 span5 + 日报 span7，窄屏自动堆叠 -->
    <div class="dashboard-grid">
      <TodoPanel class="grid-todo" />
      <DeviceStatusPanel class="grid-device" />
      <BriefPanel class="grid-brief" />
      <MemberReportPanel class="grid-member" />
    </div>
  </div>
</template>

<script setup lang="ts">
// 首页工作台（结构改版 R6 §7.1）：DashboardView 拆分为「壳（头条 + 12 列网格）」+
// components/business/dashboard/ 四块（TodoPanel/DeviceStatusPanel/BriefPanel/MemberReportPanel）。
// 各块 useAsyncData/StateBlock 逻辑等价平移，数据口径零变化；本壳仅保留头条与会话级组装。
import { computed, type Component } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ChatDotRound, CirclePlus, EditPen, VideoPlay } from '@element-plus/icons-vue'
import { formatFullDate } from '@/utils/datetime'
import { filterNavByRole } from '@/config/navigation'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import { useAskDialog } from '@/composables/useAskDialog'
import TodoPanel from '@/components/business/dashboard/TodoPanel.vue'
import DeviceStatusPanel from '@/components/business/dashboard/DeviceStatusPanel.vue'
import BriefPanel from '@/components/business/dashboard/BriefPanel.vue'
import MemberReportPanel from '@/components/business/dashboard/MemberReportPanel.vue'

const router = useRouter()
const { t } = useI18n()
const auth = useAuthStore()
const projects = useProjectStore()
const { openAskDialog } = useAskDialog()

// 时段问候：<12 上午 / <18 下午 / 否则晚上；displayName 缺失时回退用户名
const greetingKey = computed(() => {
  const h = new Date().getHours()
  if (h < 12) return 'morning'
  if (h < 18) return 'afternoon'
  return 'evening'
})

const displayName = computed(() => auth.user?.display_name || auth.user?.username || '')

const greeting = computed(() => t(`dashboard.greeting.${greetingKey.value}`, { name: displayName.value }))

// 今日日期走 utils/datetime.ts（含星期，locale 跟随 i18n）
const bannerDate = computed(() => formatFullDate(new Date()))

// 快捷操作（§3.1 依据表）：写日报/新建批次 maintainer+（DailyReportView canSubmit、
// RunListView canEdit），新建待办/AI 问答全角色；新建批次在项目列表为空时隐藏。
type QuickAction = {
  key: 'dailyReport' | 'todo' | 'ask' | 'run'
  labelKey: string
  icon: Component
  minRole?: 'maintainer'
  needsProject?: boolean
  run: () => void
}

const quickActions = computed<QuickAction[]>(() => {
  const defs: QuickAction[] = [
    { key: 'dailyReport', labelKey: 'dashboard.quickActions.dailyReport', icon: EditPen, minRole: 'maintainer', run: () => router.push('/daily-report') },
    { key: 'todo', labelKey: 'dashboard.quickActions.todo', icon: CirclePlus, run: () => router.push('/todos') },
    { key: 'ask', labelKey: 'dashboard.quickActions.ask', icon: ChatDotRound, run: () => openAskDialog() },
    {
      key: 'run',
      labelKey: 'dashboard.quickActions.run',
      icon: VideoPlay,
      minRole: 'maintainer',
      needsProject: true,
      run: () => {
        const current = projects.current
        if (current) router.push(`/projects/${current.id}/experiment-runs`)
      }
    }
  ]
  return filterNavByRole(defs, auth.user?.role ?? '').filter((d) => !d.needsProject || projects.projects.length > 0)
})
</script>

<style scoped>
/* ---------- 工作台头条 ---------- */

.workspace-banner {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-4);
}

.banner-main {
  min-width: 0;
}

.greeting-title {
  font-size: var(--fs-title-xl);
}

.banner-date {
  color: var(--text-3);
  font-size: 13px;
  margin-top: 2px;
}

.banner-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-left: auto;
}

/* ---------- 12 列网格（R6 §7.1）：宽屏固定 7/5 双行，窄屏自动堆叠 ---------- */

.dashboard-grid {
  align-items: start;
  display: grid;
  gap: var(--space-6);
  grid-template-columns: repeat(12, 1fr);
}

.grid-todo,
.grid-member {
  grid-column: span 7;
}

.grid-device,
.grid-brief {
  grid-column: span 5;
}

/* 中窄有效宽度（≤1200 视口，200% 缩放走查同一约束）回退 auto-fit：固定 span 在
   769-1200 区间会挤压列（span5 不足 300px），auto-fit + 300px 最小列宽保持既有折行行为 */
@media (max-width: 1200px) {
  .dashboard-grid {
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr));
  }

  .grid-todo,
  .grid-member,
  .grid-device,
  .grid-brief {
    grid-column: auto;
  }
}

@media (max-width: 768px) {
  .dashboard-grid {
    grid-template-columns: 1fr;
  }

  .banner-date {
    white-space: normal;
  }
}
</style>