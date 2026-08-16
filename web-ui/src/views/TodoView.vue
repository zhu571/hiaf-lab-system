<template>
  <div class="page">
    <!-- 列表骨架统一 base/ListPage（结构改版 R3）：三筛选 + 新建在 actions 槽；ntfy 订阅 panel 非列表骨架，保留在 ListPage 外 -->
    <ListPage
      :title="t('todos.title')"
      :loading="loading && !todos"
      :error="loadError"
      :empty="!loading && !todos?.length"
      :error-text="t('todos.loadFailed')"
      :empty-text="t('todos.empty')"
      @retry="load"
    >
      <template #actions>
        <el-date-picker v-model="filters.date" type="date" value-format="YYYY-MM-DD" :clearable="false" @change="load" />
        <el-select v-model="filters.scope" :placeholder="t('todos.scope')" @change="load">
          <el-option :label="t('todos.scopeAll')" value="all" />
          <el-option :label="t('todos.scopeMine')" value="mine" />
          <el-option :label="t('todos.scopeShared')" value="shared" />
        </el-select>
        <el-select v-model="filters.status" :placeholder="t('todos.status')" @change="load">
          <el-option :label="t('todos.statusOpen')" value="open" />
          <el-option :label="t('todos.statusDone')" value="done" />
          <el-option :label="t('todos.statusCancelled')" value="cancelled" />
          <el-option :label="t('todos.statusAll')" value="all" />
        </el-select>
        <el-button type="primary" :icon="Plus" @click="openCreate">{{ t('todos.add') }}</el-button>
      </template>
      <!-- 列表三态经 ListPage 内 StateBlock：首屏骨架 > 错误重试 > 空态 > 列表；操作级错误仍走 showApiError -->
      <div class="todo-list">
        <div v-for="item in todos ?? []" :key="item.id" class="todo-row" :class="{ done: item.status === 'done' }">
          <el-checkbox
            :model-value="item.status === 'done'"
            :disabled="item.status === 'cancelled' || !canComplete(item)"
            @change="onToggleDone(item)"
          />
          <span class="todo-priority" :class="item.priority">{{ priorityLabel(item.priority) }}</span>
          <span class="todo-title">{{ item.title }}</span>
          <el-tag v-if="item.status === 'deferred'" size="small" type="warning">{{ t('todos.deferredTag') }}</el-tag>
          <el-tag v-if="item.status === 'cancelled'" size="small" type="info">{{ t('todos.cancelledTag') }}</el-tag>
          <span class="todo-source">{{ sourceLabel(item) }}</span>
          <span class="todo-actions">
            <el-button v-if="canDefer(item)" size="small" @click="onDefer(item)">{{ t('todos.defer') }}</el-button>
            <el-button v-if="canEdit(item)" size="small" @click="openEdit(item)">{{ t('todos.edit') }}</el-button>
            <el-button v-if="canEdit(item)" size="small" type="danger" plain @click="onDelete(item)">{{ t('todos.delete') }}</el-button>
          </span>
        </div>
      </div>
    </ListPage>

    <section class="panel subscribe-panel">
      <div class="panel-head">
        <span class="panel-icon"><el-icon><Bell /></el-icon></span>
        <h3>{{ t('todos.subscribeTitle') }}</h3>
      </div>
      <p class="subscribe-hint">{{ t('todos.subscribeHint') }}</p>
      <div v-if="topic" class="subscribe-row">
        <span class="subscribe-label">{{ t('todos.topic') }}:</span>
        <code>{{ topic }}</code>
        <el-button size="small" @click="copyText(topic)">{{ t('todos.copy') }}</el-button>
      </div>
      <div v-if="subscribeUrl" class="subscribe-row">
        <span class="subscribe-label">{{ t('todos.subscribeUrl') }}:</span>
        <code>{{ subscribeUrl }}</code>
        <el-button size="small" @click="copyText(subscribeUrl)">{{ t('todos.copy') }}</el-button>
      </div>
      <el-button v-if="!redeemed" type="primary" plain :loading="provisioning" @click="onProvision">
        {{ t('todos.provision') }}
      </el-button>
      <div v-if="provisionToken" class="subscribe-row">
        <span class="subscribe-label">{{ t('todos.provisionToken') }}:</span>
        <code>{{ provisionToken }}</code>
        <el-button size="small" type="primary" :loading="redeeming" @click="onRedeem">{{ t('todos.redeem') }}</el-button>
      </div>
      <div v-if="redeemed" class="redeemed-box">
        <p><strong>{{ t('todos.account') }}:</strong> <code>{{ redeemed.username }}</code></p>
        <p><strong>{{ t('todos.password') }}:</strong> <code>{{ redeemed.password }}</code></p>
        <el-button size="small" @click="copyText(redeemed.password)">{{ t('todos.copy') }}</el-button>
        <p class="subscribe-hint">{{ t('todos.ntfyAppHint') }}</p>
      </div>
      <el-alert v-if="subscribeError" class="subscribe-error" type="error" :title="subscribeError" show-icon :closable="false" />
    </section>

    <FormDialog v-model="createDialog" :title="t('todos.add')" width="480" :loading="saving" @submit="onCreate">
      <el-form-item :label="t('todos.titleLabel')" required>
        <el-input v-model="createForm.title" :placeholder="t('todos.titlePlaceholder')" maxlength="256" show-word-limit />
      </el-form-item>
      <el-form-item :label="t('todos.priority')">
        <el-radio-group v-model="createForm.priority">
          <el-radio value="high">{{ t('todos.priorityHigh') }}</el-radio>
          <el-radio value="medium">{{ t('todos.priorityMedium') }}</el-radio>
          <el-radio value="low">{{ t('todos.priorityLow') }}</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item :label="t('todos.shareToProject')">
        <el-select v-model="createForm.project_id" clearable :placeholder="t('todos.noShare')">
          <el-option v-for="p in projects" :key="p.id" :label="p.name" :value="p.id" />
        </el-select>
      </el-form-item>
    </FormDialog>

    <FormDialog v-model="editDialog" :title="t('todos.edit')" width="480" :loading="saving" @submit="onEditSave">
      <el-form-item :label="t('todos.titleLabel')" required>
        <el-input v-model="editForm.title" maxlength="256" show-word-limit />
      </el-form-item>
      <el-form-item :label="t('todos.priority')">
        <el-radio-group v-model="editForm.priority">
          <el-radio value="high">{{ t('todos.priorityHigh') }}</el-radio>
          <el-radio value="medium">{{ t('todos.priorityMedium') }}</el-radio>
          <el-radio value="low">{{ t('todos.priorityLow') }}</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item :label="t('todos.shareToProject')">
        <el-select v-model="editForm.project_id" clearable :placeholder="t('todos.noShare')">
          <el-option v-for="p in projects" :key="p.id" :label="p.name" :value="p.id" />
        </el-select>
      </el-form-item>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Bell, Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createTodo, deleteTodo, deferTodo, doneTodo, getNotificationTopic,
  listTodos, provisionTopic, redeemTopic, updateTodo,
  type Todo
} from '@/api/todos'
import { listProjects, type Project } from '@/api/projects'
import ListPage from '@/components/base/ListPage.vue'
import FormDialog from '@/components/base/FormDialog.vue'
import { useAsyncData } from '@/composables/useAsyncData'
import { showApiError } from '@/composables/useNotify'
import { statusMetaFor } from '@/utils/statusMeta'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const filters = reactive({ date: todayStr(), scope: 'all', status: 'open' })
// 列表加载收敛 useAsyncData（重构 S4）：内建竞态 seq + 卸载丢弃；error 只写 ref 不 toast，三态走 StateBlock
const { data: todos, loading, error: loadError, run: load } = useAsyncData<Todo[]>(() =>
  listTodos({ date: filters.date, scope: filters.scope as never, status: filters.status as never })
)
const projects = ref<Project[]>([])

const createDialog = ref(false)
const editDialog = ref(false)
const saving = ref(false)
const createForm = reactive({ title: '', priority: 'medium' as Todo['priority'], project_id: undefined as string | undefined })
const editForm = reactive({ id: '', title: '', priority: 'medium' as Todo['priority'], project_id: undefined as string | undefined, updated_at: '' })
let editingTodo: Todo | null = null

const topic = ref('')
const subscribeUrl = ref('')
const provisionToken = ref('')
const provisioned = ref(false)
const provisioning = ref(false)
const redeeming = ref(false)
const redeemed = ref<{ username: string; password: string } | null>(null)
const subscribeError = ref('')

function todayStr() {
  const d = new Date()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${m}-${day}`
}

onMounted(() => {
  // 列表首跑由 useAsyncData immediate 承担，此处仅加载辅助数据
  loadProjects()
  loadTopic()
})

async function loadProjects() {
  try {
    projects.value = await listProjects()
  } catch (err) {
    showApiError(err, t('todos.loadProjectsFailed'))
  }
}

function canComplete(item: Todo) {
  return item.status !== 'done' && item.status !== 'cancelled'
}

// 推迟/编辑/删除仅添加者（owner）可做；后端强校验，前端按钮仅为 UX。
function isOwner(item: Todo) {
  return auth.user?.id === item.created_by
}

function canDefer(item: Todo) {
  return item.status === 'pending' && isOwner(item)
}

function canEdit(item: Todo) {
  return item.status !== 'cancelled' && isOwner(item)
}

// 优先级文案走 statusMeta 注册表 labelKey，未命中回退原文（对齐 IssuesView statusLabel 先例）
function priorityLabel(p: string) {
  const m = statusMetaFor('todoPriority', p)
  return m ? t(m.labelKey) : p
}

function sourceLabel(item: Todo) {
  if (item.source === 'issue') return t('todos.sourceIssue')
  if (item.source === 'daily_llm') return t('todos.sourceDaily')
  if (item.source === 'llm') return t('todos.sourceLLM')
  return t('todos.sourceManual')
}

async function onToggleDone(item: Todo) {
  try {
    await doneTodo(item.id)
    ElMessage.success(t('todos.doneSaved'))
    load()
  } catch (err) {
    showApiError(err, t('todos.doneFailed'))
    load()
  }
}

async function onDefer(item: Todo) {
  try {
    await deferTodo(item.id)
    ElMessage.success(t('todos.deferred'))
    load()
  } catch (err) {
    showApiError(err, t('todos.deferFailed'))
  }
}

async function onDelete(item: Todo) {
  try {
    await ElMessageBox.confirm(t('todos.confirmDelete'), t('todos.deleteTitle'), { type: 'warning' })
    await deleteTodo(item.id)
    ElMessage.success(t('todos.deleted'))
    load()
  } catch (err) {
    if (err === 'cancel' || err === 'close') return
    showApiError(err, t('todos.deleteFailed'))
  }
}

function openCreate() {
  createForm.title = ''
  createForm.priority = 'medium'
  createForm.project_id = undefined
  createDialog.value = true
}

async function onCreate() {
  const title = createForm.title.trim()
  if (!title) {
    ElMessage.warning(t('todos.titleRequired'))
    return
  }
  saving.value = true
  try {
    await createTodo({ title, priority: createForm.priority, project_id: createForm.project_id })
    ElMessage.success(t('todos.created'))
    createDialog.value = false
    load()
  } catch (err) {
    showApiError(err, t('todos.createFailed'))
  } finally {
    saving.value = false
  }
}

function openEdit(item: Todo) {
  editingTodo = item
  editForm.id = item.id
  editForm.title = item.title
  editForm.priority = item.priority
  editForm.project_id = item.project_id || undefined
  editForm.updated_at = item.updated_at
  editDialog.value = true
}

async function onEditSave() {
  if (!editingTodo) return
  const title = editForm.title.trim()
  if (!title) {
    ElMessage.warning(t('todos.titleRequired'))
    return
  }
  saving.value = true
  try {
    await updateTodo(editForm.id, {
      updated_at: editForm.updated_at,
      title,
      priority: editForm.priority,
      // 空串 = 取消共享（后端置 NULL）；字段缺席 = 不变
      project_id: editForm.project_id || ''
    })
    ElMessage.success(t('todos.saved'))
    editDialog.value = false
    load()
  } catch (err) {
    const e = err as Error
    if (e.message.includes('version_conflict') || e.message.includes('已被修改')) {
      ElMessage.warning(t('todos.versionConflict'))
    } else {
      showApiError(err, t('todos.saveFailed'))
    }
    load()
  } finally {
    saving.value = false
  }
}

async function loadTopic() {
  try {
    const info = await getNotificationTopic()
    topic.value = info.topic
    subscribeUrl.value = info.subscribe_url
  } catch (err) {
    subscribeError.value = (err as Error).message || t('todos.topicLoadFailed')
  }
}

async function onProvision() {
  provisioning.value = true
  subscribeError.value = ''
  try {
    const resp = await provisionTopic()
    provisionToken.value = resp.provision_token
    provisioned.value = true
    redeemed.value = null
    ElMessage.success(t('todos.provisioned'))
  } catch (err) {
    showApiError(err, t('todos.provisionFailed'))
  } finally {
    provisioning.value = false
  }
}

async function onRedeem() {
  redeeming.value = true
  subscribeError.value = ''
  try {
    const resp = await redeemTopic(provisionToken.value)
    redeemed.value = { username: resp.username, password: resp.password }
    provisionToken.value = ''
    provisioned.value = false
    ElMessage.success(t('todos.redeemed'))
  } catch (err) {
    showApiError(err, t('todos.redeemFailed'))
    provisionToken.value = ''
    provisioned.value = false
  } finally {
    redeeming.value = false
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(t('todos.copied'))
  } catch {
    ElMessage.warning(t('todos.copyFailed'))
  }
}
</script>

<style scoped>
.todo-list {
  min-height: 120px;
}
.todo-row {
  align-items: center;
  border-bottom: 1px solid var(--border);
  display: flex;
  gap: 10px;
  padding: 10px 12px;
}
.todo-row.done .todo-title {
  color: var(--text-3);
  text-decoration: line-through;
}
/* 优先级 pill 样式已上提 utilities.css 公共类（S4，两处一致合并；statusMeta todoPriority 族） */
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
.todo-actions {
  display: flex;
  gap: 6px;
}
.subscribe-panel {
  margin-top: 16px;
}
.subscribe-row {
  align-items: center;
  display: flex;
  gap: 8px;
  margin: 8px 0;
}
.subscribe-label {
  color: var(--text-3);
  white-space: nowrap;
}
.subscribe-row code {
  background: var(--bg);
  border-radius: 4px;
  padding: 2px 6px;
  word-break: break-all;
}
.redeemed-box {
  background: var(--bg);
  border-radius: 8px;
  margin-top: 8px;
  padding: 12px;
}
.redeemed-box p {
  margin: 6px 0;
}
.subscribe-hint {
  color: var(--text-3);
  font-size: 13px;
}
.subscribe-error {
  margin-top: 8px;
}

@media (max-width: 768px) {
  .todo-row {
    flex-wrap: wrap;
  }

  .todo-row .el-checkbox {
    order: 1;
  }

  .todo-title {
    flex: 1 1 auto;
    order: 2;
  }

  .todo-priority,
  .todo-row .el-tag,
  .todo-source {
    order: 3;
  }

  .todo-actions {
    margin-left: auto;
    order: 4;
  }
}
</style>
