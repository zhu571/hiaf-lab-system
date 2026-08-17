<template>
  <DashboardPanel
    class="todo-panel"
    :title="t('dashboard.todayTodos')"
    :icon="Tickets"
    :meta="t('dashboard.todosCount', { n: todos.length })"
    divided
  >
    <template #actions>
      <el-button class="todo-more" size="small" text type="primary" @click="router.push('/todos')">
        {{ t('dashboard.todosMore') }}
      </el-button>
    </template>
    <div class="todo-list">
      <StateBlock
        :loading="loadingTodos && !todosData"
        :error="todosError"
        :empty="!todos.length"
        :error-text="t('dashboard.todosLoadFailed')"
        :empty-text="t('dashboard.noTodos')"
        @retry="loadTodos"
      >
        <div v-for="item in todos" :key="item.id" class="todo-row" :class="{ done: item.status === 'done' }">
          <el-checkbox :model-value="item.status === 'done'" @change="toggleTodo(item)" />
          <span class="todo-priority" :class="item.priority">{{ todoPriorityLabel(item.priority) }}</span>
          <span class="todo-title">{{ item.title }}</span>
          <span class="todo-source">{{ todoSourceLabel(item) }}</span>
          <el-button v-if="item.status === 'pending'" size="small" text type="warning" @click="deferTodoItem(item)">
            {{ t('dashboard.defer') }}
          </el-button>
        </div>
      </StateBlock>
    </div>
    <div class="todo-add-row">
      <el-input v-model="manualTitle" :placeholder="t('dashboard.todoAddPlaceholder')" maxlength="256" clearable @keyup.enter="addManualTodo" />
      <el-button type="primary" :loading="addingTodo" @click="addManualTodo">{{ t('dashboard.todoAdd') }}</el-button>
    </div>
    <div class="todo-add-row">
      <el-input v-model="llmText" :placeholder="t('dashboard.todoLLMPlaceholder')" maxlength="2000" clearable @keyup.enter="parseLLMTodo" />
      <el-button type="success" plain :loading="parsingLLM" @click="parseLLMTodo">{{ t('dashboard.todoLLM') }}</el-button>
    </div>
  </DashboardPanel>
</template>

<script setup lang="ts">
// 首页今日待办块（结构改版 R6 §7.1 拆分）：DashboardView 待办 panel 等价平移，
// useAsyncData/StateBlock 逻辑原样迁移，数据口径零变化。
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Tickets } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createTodo, deferTodo, doneTodo, listTodos, llmAdd, llmParse, type Todo } from '@/api/todos'
import { showApiError } from '@/composables/useNotify'
import { useAsyncData } from '@/composables/useAsyncData'
import { statusMetaFor } from '@/utils/statusMeta'
import StateBlock from '@/components/base/StateBlock.vue'
import DashboardPanel from '@/components/base/DashboardPanel.vue'

const router = useRouter()
const { t } = useI18n()

const {
  data: todosData,
  loading: loadingTodos,
  error: todosError,
  run: loadTodos
} = useAsyncData(() => listTodos({ status: 'open' }))
const todos = computed(() => todosData.value ?? [])
const manualTitle = ref('')
const llmText = ref('')
const addingTodo = ref(false)
const parsingLLM = ref(false)
const llmDraft = ref<{ title: string; priority: Todo['priority']; reason?: string | null } | null>(null)

async function toggleTodo(item: Todo) {
  try {
    await doneTodo(item.id)
    ElMessage.success(t('dashboard.todoDone'))
    loadTodos()
  } catch (err) {
    showApiError(err, t('dashboard.todoDoneFailed'))
  }
}

async function deferTodoItem(item: Todo) {
  try {
    await deferTodo(item.id)
    ElMessage.success(t('dashboard.todoDeferred'))
    loadTodos()
  } catch (err) {
    showApiError(err, t('dashboard.todoDeferFailed'))
  }
}

async function addManualTodo() {
  const title = manualTitle.value.trim()
  if (!title) return
  addingTodo.value = true
  try {
    await createTodo({ title })
    ElMessage.success(t('dashboard.todoAdded'))
    manualTitle.value = ''
    loadTodos()
  } catch (err) {
    showApiError(err, t('dashboard.todoAddFailed'))
  } finally {
    addingTodo.value = false
  }
}

async function parseLLMTodo() {
  const text = llmText.value.trim()
  if (!text) return
  parsingLLM.value = true
  try {
    const draft = await llmParse(text)
    if (draft.status === 'rejected') {
      ElMessage.info(draft.reason || t('dashboard.todoLLMRejected', { reason: '' }))
      return
    }
    llmDraft.value = { title: draft.title, priority: draft.priority, reason: draft.reason }
    if (draft.reason) ElMessage.info(t('dashboard.todoLLMRejected', { reason: draft.reason }))
    const confirmed = await ElMessageBox.confirm(
      `${t('dashboard.todoDraftTitle')}：${draft.title}`,
      t('dashboard.todoDraftConfirm'),
      { confirmButtonText: t('dashboard.todoDraftSave'), cancelButtonText: t('common.cancel'), type: 'info' }
    )
    if (confirmed !== 'confirm') return
    await llmAdd({ title: draft.title, priority: draft.priority })
    ElMessage.success(t('dashboard.todoAdded'))
    llmText.value = ''
    llmDraft.value = null
    loadTodos()
  } catch (err) {
    if (err === 'cancel' || err === 'close') return
    showApiError(err, t('dashboard.todoLLMFailed'))
  } finally {
    parsingLLM.value = false
  }
}

// 优先级文案走 statusMeta 注册表 labelKey，未命中回退原文（对齐 IssuesView statusLabel 先例）
function todoPriorityLabel(p: string) {
  const m = statusMetaFor('todoPriority', p)
  return m ? t(m.labelKey) : p
}

function todoSourceLabel(item: Todo) {
  if (item.source === 'issue') return t('todos.sourceIssue')
  if (item.source === 'daily_llm') return t('todos.sourceDaily')
  if (item.source === 'llm') return t('todos.sourceLLM')
  return t('todos.sourceManual')
}
</script>

<style scoped>
.todo-panel {
  margin-bottom: 12px;
}

.todo-more {
  margin-left: auto;
}

.todo-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-height: 48px;
}

.todo-row {
  align-items: center;
  border-bottom: 1px solid var(--border);
  display: flex;
  gap: 10px;
  padding: 6px 8px;
}

.todo-row.done .todo-title {
  color: var(--text-3);
  text-decoration: line-through;
}

.todo-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.todo-source {
  color: var(--text-3);
  font-size: 12px;
  white-space: nowrap;
}

.todo-add-row {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.todo-add-row .el-input {
  flex: 1;
}
</style>