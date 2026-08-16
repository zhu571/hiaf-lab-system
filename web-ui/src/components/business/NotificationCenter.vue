<template>
  <!-- 桌面：铃铛 + el-popover 面板（380px）；移动端：el-drawer 形态（本批双端可用，移动入口随 R7 接线） -->
  <el-popover
    v-if="!isMobile"
    v-model:visible="popoverOpen"
    placement="bottom-end"
    :width="380"
    trigger="click"
    popper-class="notify-popover"
    @show="refreshAll"
  >
    <template #reference>
      <button class="notify-trigger" type="button" :aria-label="t('notifications.title')">
        <el-badge :value="total" :max="99" :hidden="total === 0" class="notify-badge">
          <el-icon class="notify-bell"><Bell /></el-icon>
        </el-badge>
      </button>
    </template>
    <NotificationPanel
      :todos="todos"
      :alerts="alerts"
      :alerts-total="alertsTotal"
      :pending="reviewPending"
      :can-review="canReview"
      @navigate="closePanels"
    />
  </el-popover>
  <template v-else>
    <button class="notify-trigger" type="button" :aria-label="t('notifications.title')" @click="drawerOpen = true">
      <el-badge :value="total" :max="99" :hidden="total === 0" class="notify-badge">
        <el-icon class="notify-bell"><Bell /></el-icon>
      </el-badge>
    </button>
    <el-drawer v-model="drawerOpen" :title="t('notifications.title')" size="92%" class="notify-drawer" @open="refreshAll">
      <NotificationPanel
        :todos="todos"
        :alerts="alerts"
        :alerts-total="alertsTotal"
        :pending="reviewPending"
        :can-review="canReview"
        @navigate="closePanels"
      />
    </el-drawer>
  </template>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Bell } from '@element-plus/icons-vue'
import { listTodos, type Todo } from '@/api/todos'
import { listAlerts, type AlertRecord } from '@/api/alerts'
import { useAuthStore } from '@/stores/auth'
import { useAgentPending } from '@/composables/useAgentPending'
import { useMobile } from '@/composables/useMobile'
import { usePolling } from '@/composables/usePolling'
import NotificationPanel from '@/components/business/NotificationPanel.vue'

// 通知中心（结构改版 R2 §3.2）：顶栏铃铛聚合「待办 / 活跃告警 / 待审候选」三类待处理事项。
// 只读既有 GET 端点、零后端改动；各组数据本就由后端鉴权，前端仅聚合入口，无越权面。
const { t } = useI18n()
const isMobile = useMobile()
const auth = useAuthStore()
// 待审组复用 useAgentPending 单例计数（侧栏 badge 同一轮询，本组件不新增轮询）
const { agentPending, refreshAgentPending } = useAgentPending()

// 数据口径（逐条对账 §3.2）：
// 待办组 = listTodos({ date: 今日, scope: 'all', status: 'open' })（与 TodoView :155-158 默认口径一致）
// 告警组 = listAlerts({ status: 'active', limit: 6 })（AlertCenterView :161-169 active tab 同口径，limit 由 100 调小为 6）
const todos = ref<Todo[]>([])
const alerts = ref<AlertRecord[]>([])
const alertsTotal = ref(0)

// 与 TodoView todayStr 同口径（本地时区 YYYY-MM-DD）
function todayStr() {
  const d = new Date()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${m}-${day}`
}

async function refreshTodos() {
  try {
    todos.value = await listTodos({ date: todayStr(), scope: 'all', status: 'open' })
  } catch {
    // 聚合入口拉取失败静默降级，下一轮轮询再试，不打断顶栏
  }
}

async function refreshAlerts() {
  try {
    const res = await listAlerts({ status: 'active', limit: 6 })
    alerts.value = res.items ?? []
    alertsTotal.value = res.total ?? 0
  } catch {
    // 同上，静默降级
  }
}

const canReview = computed(() => auth.canReviewAgent)
const reviewPending = computed(() => (canReview.value ? agentPending.value : 0))
// badge 总数口径（§3.2）：今日待办 open 数 + active 告警数 + 待审数（viewer 无待审组则不计）；max 99 沿用侧栏惯例
const total = computed(() => todos.value.length + alertsTotal.value + reviewPending.value)

function refreshAll() {
  void refreshTodos()
  void refreshAlerts()
  void refreshAgentPending()
}

// 告警/待办组独立 usePolling 60s、pauseOnHidden（§3.2）；挂载即拉一次，避免首开面板空闪
const todoPolling = usePolling(refreshTodos, 60000)
const alertPolling = usePolling(refreshAlerts, 60000)
onMounted(() => {
  refreshAll()
  todoPolling.start()
  alertPolling.start()
})

const popoverOpen = ref(false)
const drawerOpen = ref(false)
function closePanels() {
  popoverOpen.value = false
  drawerOpen.value = false
}
</script>

<style scoped>
.notify-trigger {
  align-items: center;
  background: none;
  border: none;
  border-radius: var(--radius-md);
  color: var(--text-2);
  cursor: pointer;
  display: inline-flex;
  height: 36px;
  justify-content: center;
  padding: 0;
  transition:
    background 0.15s ease,
    color 0.15s ease;
  width: 36px;
}

.notify-trigger:hover {
  background: var(--surface-2);
  color: var(--text-1);
}

.notify-bell {
  font-size: 17px;
}
</style>
