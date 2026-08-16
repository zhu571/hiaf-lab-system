<template>
  <div class="notify-panel">
    <!-- 全空（三组计数均 0）显示 el-empty；单组空显示「暂无」文本行（方案 §3.2 空态） -->
    <el-empty v-if="allEmpty" :description="t('notifications.empty')" :image-size="80" />
    <template v-else>
      <section class="notify-group">
        <header class="notify-group-head">
          <span class="notify-group-title">
            {{ t('notifications.groups.todo') }}<span class="notify-count">{{ todos.length }}</span>
          </span>
          <button type="button" class="notify-viewall" @click="go('/todos')">
            {{ t('notifications.viewAll') }}<el-icon><ArrowRight /></el-icon>
          </button>
        </header>
        <p v-if="todos.length === 0" class="notify-empty-row">{{ t('notifications.groupEmpty') }}</p>
        <button
          v-for="todo in todos.slice(0, 5)"
          :key="todo.id"
          type="button"
          class="notify-item"
          @click="go('/todos')"
        >
          <span class="notify-item-title">{{ todo.title }}</span>
          <StatusBadge domain="todoPriority" :value="todo.priority" />
        </button>
      </section>

      <section class="notify-group">
        <header class="notify-group-head">
          <span class="notify-group-title">
            {{ t('notifications.groups.alert') }}<span class="notify-count">{{ alertsTotal }}</span>
          </span>
          <button type="button" class="notify-viewall" @click="go('/alerts')">
            {{ t('notifications.viewAll') }}<el-icon><ArrowRight /></el-icon>
          </button>
        </header>
        <p v-if="alerts.length === 0" class="notify-empty-row">{{ t('notifications.groupEmpty') }}</p>
        <button
          v-for="alert in alerts.slice(0, 5)"
          :key="alert.id"
          type="button"
          class="notify-item"
          @click="go('/alerts')"
        >
          <span class="notify-item-title">{{ alert.title }}</span>
          <StatusBadge domain="alertLevel" :value="alert.level" />
        </button>
      </section>

      <!-- 待审组仅 maintainer/admin（canReviewAgent）；计数行无条目列表，查看全部 → /agent-candidates -->
      <section v-if="canReview" class="notify-group">
        <header class="notify-group-head">
          <span class="notify-group-title">
            {{ t('notifications.groups.review') }}<span class="notify-count">{{ pending }}</span>
          </span>
          <button type="button" class="notify-viewall" @click="go('/agent-candidates')">
            {{ t('notifications.viewAll') }}<el-icon><ArrowRight /></el-icon>
          </button>
        </header>
        <p v-if="pending === 0" class="notify-empty-row">{{ t('notifications.groupEmpty') }}</p>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ArrowRight } from '@element-plus/icons-vue'
import type { Todo } from '@/api/todos'
import type { AlertRecord } from '@/api/alerts'
import StatusBadge from '@/components/base/StatusBadge.vue'

// 通知面板主体（结构改版 R2 §3.2）：桌面 el-popover 与移动 el-drawer 两种容器共用此面板，
// 数据由 NotificationCenter 聚合后以 props 传入；全部文本插值渲染，无 v-html（§12 XSS 约定）。
const props = defineProps<{
  todos: Todo[]
  alerts: AlertRecord[]
  alertsTotal: number
  pending: number
  canReview: boolean
}>()
const emit = defineEmits<{ navigate: [] }>()

const { t } = useI18n()
const router = useRouter()

const allEmpty = computed(
  () => props.todos.length === 0 && props.alertsTotal === 0 && (!props.canReview || props.pending === 0)
)

function go(path: string) {
  emit('navigate')
  router.push(path)
}
</script>

<style scoped>
.notify-panel {
  display: grid;
}

.notify-group + .notify-group {
  border-top: 1px solid var(--border);
  margin-top: 6px;
  padding-top: 6px;
}

.notify-group-head {
  align-items: center;
  display: flex;
  justify-content: space-between;
  padding: 4px 2px 6px;
}

.notify-group-title {
  color: var(--text-2);
  font-size: 13px;
  font-weight: 600;
}

.notify-count {
  color: var(--text-3);
  font-weight: 400;
  margin-left: 6px;
}

.notify-viewall {
  align-items: center;
  background: none;
  border: none;
  border-radius: var(--radius-sm);
  color: var(--brand-600);
  cursor: pointer;
  display: inline-flex;
  font-family: inherit;
  font-size: 12px;
  gap: 2px;
  padding: 2px 4px;
}

.notify-viewall:hover {
  background: var(--surface-2);
}

.notify-empty-row {
  color: var(--text-3);
  font-size: 12px;
  margin: 0;
  padding: 2px 2px 8px;
}

.notify-item {
  align-items: center;
  background: none;
  border: none;
  border-radius: var(--radius-sm);
  cursor: pointer;
  display: flex;
  font-family: inherit;
  gap: 8px;
  padding: 7px 8px;
  text-align: left;
  transition: background 0.15s ease;
  width: 100%;
}

.notify-item:hover {
  background: var(--surface-2);
}

.notify-item-title {
  color: var(--text-1);
  flex: 1;
  font-size: 13px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
