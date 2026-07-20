<template>
  <div class="page">
    <div class="toolbar">
      <h2>装配步骤</h2>
      <el-select v-model="statusFilter" class="status-select" placeholder="全部状态" clearable>
        <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
      </el-select>
      <el-button v-if="canOperate" type="primary" @click="createDialog = true">新建步骤</el-button>
    </div>
    <section class="panel">
      <el-alert v-if="loadError" class="load-error" type="error" :title="loadError" show-icon :closable="false">
        <el-button size="small" @click="load">重试</el-button>
      </el-alert>
      <div v-loading="loading" class="step-list">
        <div
          v-for="(step, index) in filteredSteps"
          :key="step.id"
          class="step-row"
          :class="{ dragging: dragIndex === index }"
          :draggable="canReorder && !statusFilter"
          @dragstart="onDragStart(index)"
          @dragover.prevent
          @drop="onDrop(index)"
        >
          <span v-if="canReorder && !statusFilter" class="drag-handle" title="拖拽排序">
            <el-icon><Rank /></el-icon>
          </span>
          <span class="order-dot">{{ step.step_order }}</span>
          <div class="step-main">
            <div class="step-title">
              <strong>{{ step.name }}</strong>
              <StatusBadge :value="step.status" />
            </div>
            <p v-if="step.description" class="step-desc">{{ step.description }}</p>
            <p class="step-meta">
              <span>执行人：{{ memberName(step.assigned_to) }}</span>
              <span>依赖：{{ depName(step.depends_on) }}</span>
              <span>开始：{{ fmtTime(step.started_at) }}</span>
              <span>完成：{{ fmtTime(step.completed_at) }}</span>
            </p>
          </div>
          <div v-if="canOperate" class="step-actions">
            <el-button
              v-for="action in transitionsByStatus[step.status] || []"
              :key="action.transition"
              size="small"
              :type="action.danger ? 'danger' : 'primary'"
              plain
              @click="onTransition(step, action)"
            >
              {{ action.label }}
            </el-button>
            <el-button size="small" type="danger" plain @click="remove(step)">删除</el-button>
          </div>
        </div>
        <el-empty v-if="!loading && !loadError && filteredSteps.length === 0" description="暂无装配步骤" />
      </div>
    </section>
    <el-dialog v-model="createDialog" title="新建步骤" width="560">
      <el-form label-position="top">
        <el-form-item label="名称" required><el-input v-model="draft.name" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="draft.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="依赖步骤">
          <el-select v-model="draft.depends_on" clearable placeholder="无依赖">
            <el-option v-for="s in steps" :key="s.id" :label="`${s.step_order}. ${s.name}`" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="执行人">
          <el-select v-model="draft.assigned_to" clearable placeholder="未分配">
            <el-option v-for="m in members" :key="m.user_id" :label="m.username || m.user_id" :value="m.user_id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">取消</el-button>
        <el-button type="primary" @click="create">保存</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="overrideDialog" title="前置步骤未完成" width="480">
      <div class="grid">
        <p class="override-tip">
          前置步骤《{{ overrideTarget?.dep.name }}》当前状态为
          <StatusBadge :value="overrideTarget?.dep.status || ''" />
        </p>
        <el-input
          v-model="overrideReason"
          type="textarea"
          :rows="3"
          placeholder="前置步骤已取消时可填写理由强制绕过（override_reason）"
        />
      </div>
      <template #footer>
        <el-button @click="overrideDialog = false">取消</el-button>
        <el-button type="primary" @click="submitOverride">继续流转</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Rank } from '@element-plus/icons-vue'
import StatusBadge from '../components/StatusBadge.vue'
import {
  createAssemblyStep,
  deleteAssemblyStep,
  listAssemblySteps,
  reorderAssemblySteps,
  transitionAssemblyStep,
  type AssemblyStep
} from '../api/assembly'
import { listMembers, type ProjectMember } from '../api/projects'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

type TransitionAction = { transition: string; label: string; confirm: boolean; danger?: boolean }

const route = useRoute()
const auth = useAuthStore()
const steps = ref<AssemblyStep[]>([])
const members = ref<ProjectMember[]>([])
const loading = ref(false)
const loadError = ref('')
const statusFilter = ref('')
const createDialog = ref(false)
const overrideDialog = ref(false)
const overrideReason = ref('')
const overrideTarget = ref<{ step: AssemblyStep; transition: string; dep: AssemblyStep } | null>(null)
const dragIndex = ref(-1)
const draft = reactive({ name: '', description: '', depends_on: '', assigned_to: '' })
const statuses = [
  { value: 'planned', label: '计划中' },
  { value: 'in_progress', label: '进行中' },
  { value: 'paused', label: '已暂停' },
  { value: 'completed', label: '已完成' },
  { value: 'skipped', label: '已跳过' },
  { value: 'cancelled', label: '已取消' }
]
// 与后端状态机保持一致：planned→start/cancel；in_progress→pause/complete/skip/cancel；paused→resume/cancel；skipped→start
const transitionsByStatus: Record<string, TransitionAction[]> = {
  planned: [
    { transition: 'start', label: '开始', confirm: false },
    { transition: 'cancel', label: '取消', confirm: true, danger: true }
  ],
  in_progress: [
    { transition: 'pause', label: '暂停', confirm: false },
    { transition: 'complete', label: '完成', confirm: true },
    { transition: 'skip', label: '跳过', confirm: true },
    { transition: 'cancel', label: '取消', confirm: true, danger: true }
  ],
  paused: [
    { transition: 'resume', label: '恢复', confirm: false },
    { transition: 'cancel', label: '取消', confirm: true, danger: true }
  ],
  skipped: [{ transition: 'start', label: '重新开始', confirm: false }]
}

const canOperate = computed(() => ['admin', 'maintainer', 'member'].includes(auth.user?.role || ''))
const canReorder = computed(() => ['admin', 'maintainer'].includes(auth.user?.role || ''))
// projectId 的唯一事实来源是路由参数（由 ProjectLayout 保证存在）
const projectId = computed(() => String(route.params.id || ''))
const filteredSteps = computed(() => (statusFilter.value ? steps.value.filter((s) => s.status === statusFilter.value) : steps.value))
const memberMap = computed(() => Object.fromEntries(members.value.map((m) => [m.user_id, m.username || m.user_id])) as Record<string, string>)

onMounted(load)
watch(projectId, load)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    if (!projectId.value) return
    const [data, memberList] = await Promise.all([listAssemblySteps(projectId.value, { per_page: 100 }), listMembers(projectId.value)])
    steps.value = data.items.slice().sort((a, b) => a.step_order - b.step_order)
    members.value = memberList
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : '装配步骤加载失败'
  } finally {
    loading.value = false
  }
}

function memberName(userId?: string) {
  if (!userId) return '—'
  return memberMap.value[userId] || userId
}

function depName(id?: string) {
  if (!id) return '—'
  return steps.value.find((s) => s.id === id)?.name || '—'
}

function fmtTime(t?: string) {
  return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '—'
}

async function onTransition(step: AssemblyStep, action: TransitionAction) {
  // start/resume 时检查依赖门槛：前置步骤非 completed 时走 override 对话框
  if (action.transition === 'start' || action.transition === 'resume') {
    const dep = step.depends_on ? steps.value.find((s) => s.id === step.depends_on) : undefined
    if (dep && dep.status !== 'completed') {
      overrideTarget.value = { step, transition: action.transition, dep }
      overrideReason.value = ''
      overrideDialog.value = true
      return
    }
  }
  if (action.confirm) {
    try {
      await ElMessageBox.confirm(`确认${action.label}步骤「${step.name}」？`, '状态流转', { type: 'warning' })
    } catch {
      return
    }
  }
  await doTransition(step.id, action.transition)
}

async function submitOverride() {
  if (!overrideTarget.value) return
  const { step, transition } = overrideTarget.value
  overrideDialog.value = false
  await doTransition(step.id, transition, overrideReason.value.trim())
}

async function doTransition(id: string, transition: string, reason = '') {
  try {
    await transitionAssemblyStep(id, transition, reason)
    ElMessage.success('状态已更新')
    await load()
  } catch (err) {
    showApiError(err, '状态流转失败')
  }
}

function onDragStart(index: number) {
  dragIndex.value = index
}

async function onDrop(index: number) {
  const from = dragIndex.value
  dragIndex.value = -1
  if (from < 0 || from === index) return
  const reordered = [...steps.value]
  const [moved] = reordered.splice(from, 1)
  reordered.splice(index, 0, moved)
  try {
    await reorderAssemblySteps({ project_id: projectId.value, steps: reordered.map((s, i) => ({ id: s.id, step_order: i + 1 })) })
    ElMessage.success('顺序已更新')
    await load()
  } catch (err) {
    showApiError(err, '排序失败')
    await load()
  }
}

async function create() {
  if (!draft.name.trim()) {
    ElMessage.warning('请填写步骤名称')
    return
  }
  try {
    // step_order 不传，由服务端自动取 max+1
    await createAssemblyStep(projectId.value, {
      name: draft.name.trim(),
      description: draft.description.trim() || undefined,
      depends_on: draft.depends_on || undefined,
      assigned_to: draft.assigned_to || undefined
    })
    createDialog.value = false
    draft.name = ''
    draft.description = ''
    draft.depends_on = ''
    draft.assigned_to = ''
    ElMessage.success('步骤已创建')
    await load()
  } catch (err) {
    showApiError(err, '步骤创建失败')
  }
}

async function remove(step: AssemblyStep) {
  try {
    await ElMessageBox.confirm(`确认删除步骤「${step.name}」？`, '删除步骤', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteAssemblyStep(step.id)
    ElMessage.success('步骤已删除')
    await load()
  } catch (err) {
    showApiError(err, '步骤删除失败')
  }
}
</script>

<style scoped>
.status-select {
  max-width: 160px;
}

.load-error {
  margin-bottom: 16px;
}

.step-list {
  display: grid;
  gap: 10px;
  min-height: 80px;
}

.step-row {
  align-items: center;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 10px;
  display: flex;
  gap: 12px;
  padding: 12px 14px;
  transition:
    border-color 0.15s ease,
    box-shadow 0.15s ease;
}

.step-row:hover {
  border-color: var(--brand-100);
  box-shadow: var(--shadow-md);
}

.step-row.dragging {
  opacity: 0.5;
}

.drag-handle {
  color: var(--text-3);
  cursor: grab;
  display: inline-flex;
}

.order-dot {
  align-items: center;
  background: var(--brand-500);
  border-radius: 50%;
  color: #fff;
  display: inline-flex;
  flex-shrink: 0;
  font-size: 12px;
  font-weight: 600;
  height: 26px;
  justify-content: center;
  min-width: 26px;
}

.step-main {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.step-title {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.step-title strong {
  color: var(--text-1);
  font-size: 14px;
}

.step-desc {
  color: var(--text-2);
  font-size: 13px;
  white-space: pre-wrap;
}

.step-meta {
  color: var(--text-3);
  display: flex;
  flex-wrap: wrap;
  font-size: 12px;
  gap: 4px 16px;
}

.step-actions {
  display: flex;
  flex-shrink: 0;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-end;
  margin-left: auto;
}

.step-actions .el-button + .el-button {
  margin-left: 0;
}

.override-tip {
  align-items: center;
  color: var(--text-2);
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

@media (max-width: 768px) {
  .step-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .step-actions {
    justify-content: flex-start;
    margin-left: 0;
  }
}
</style>
