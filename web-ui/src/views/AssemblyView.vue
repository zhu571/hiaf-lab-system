<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('assembly.title') }}</h2>
      <el-select v-model="statusFilter" class="status-select" :placeholder="t('assembly.allStatus')" clearable>
        <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
      </el-select>
      <el-button v-if="canOperate" type="primary" plain @click="aiDialog = true">{{ t('assembly.aiGenerate') }}</el-button>
      <RouterLink to="/step-templates"><el-button>{{ t('assembly.templateLibrary') }}</el-button></RouterLink>
      <el-button v-if="canOperate" type="primary" @click="createDialog = true">{{ t('assembly.create') }}</el-button>
    </div>
    <section class="panel">
      <el-alert v-if="loadError" class="load-error" type="error" :title="loadError" show-icon :closable="false">
        <el-button size="small" @click="load">{{ t('assembly.retry') }}</el-button>
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
          <span v-if="canReorder && !statusFilter" class="drag-handle" :title="t('assembly.dragSort')">
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
              <span>{{ t('assembly.metaAssignee') }}{{ memberName(step.assigned_to) }}</span>
              <span>{{ t('assembly.metaDependency') }}{{ depName(step.depends_on) }}</span>
              <span>{{ t('assembly.metaStarted') }}{{ fmtTime(step.started_at) }}</span>
              <span>{{ t('assembly.metaCompleted') }}{{ fmtTime(step.completed_at) }}</span>
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
            <el-button size="small" type="danger" plain @click="remove(step)">{{ t('assembly.delete') }}</el-button>
          </div>
        </div>
        <el-empty v-if="!loading && !loadError && filteredSteps.length === 0" :description="t('assembly.empty')" />
      </div>
    </section>
    <el-dialog v-model="createDialog" :title="t('assembly.create')" width="560">
      <el-form label-position="top">
        <el-form-item :label="t('assembly.name')" required><el-input v-model="draft.name" /></el-form-item>
        <el-form-item :label="t('assembly.description')"><el-input v-model="draft.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item :label="t('assembly.dependsOn')">
          <el-select v-model="draft.depends_on" clearable :placeholder="t('assembly.noDependency')">
            <el-option v-for="s in steps" :key="s.id" :label="`${s.step_order}. ${s.name}`" :value="s.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('assembly.assignee')">
          <el-select v-model="draft.assigned_to" clearable :placeholder="t('assembly.unassigned')">
            <el-option v-for="m in members" :key="m.user_id" :label="m.username || m.user_id" :value="m.user_id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="create">{{ t('assembly.save') }}</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="aiDialog" :title="t('assembly.aiGenerate')" width="760" @closed="resetAi">
      <div v-if="aiStage === 'input'" class="grid">
        <el-alert v-if="aiNotice" :type="aiNoticeType" :title="aiNotice" show-icon :closable="false" />
        <el-input
          v-model="aiPrompt"
          type="textarea"
          :rows="4"
          maxlength="4000"
          :placeholder="t('assembly.aiPromptPlaceholder')"
        />
      </div>
      <div v-else class="grid">
        <el-form label-position="top">
          <el-form-item :label="t('assembly.templateName')">
            <el-input v-model="aiName" maxlength="256" />
          </el-form-item>
        </el-form>
        <StepItemsEditor :key="aiKey" v-model="aiItems" />
      </div>
      <template #footer>
        <template v-if="aiStage === 'input'">
          <el-button @click="aiDialog = false">{{ t('common.cancel') }}</el-button>
          <el-button type="primary" :loading="aiGenerating" :disabled="!aiPrompt.trim()" @click="generate">{{ t('assembly.generate') }}</el-button>
        </template>
        <template v-else>
          <el-button @click="aiStage = 'input'">{{ t('assembly.backToEdit') }}</el-button>
          <el-button :loading="aiSubmitting" @click="applyInline">{{ t('assembly.apply') }}</el-button>
          <el-button v-if="canSaveTemplate" :loading="aiSubmitting" @click="saveTemplateOnly">{{ t('assembly.saveTemplate') }}</el-button>
          <el-button v-if="canSaveTemplate" type="primary" :loading="aiSubmitting" @click="saveAndApply">{{ t('assembly.saveAndApply') }}</el-button>
        </template>
      </template>
    </el-dialog>
    <el-dialog v-model="overrideDialog" :title="t('assembly.overrideTitle')" width="480">
      <div class="grid">
        <p class="override-tip">
          {{ t('assembly.overrideTip', { name: overrideTarget?.dep.name || '' }) }}
          <StatusBadge :value="overrideTarget?.dep.status || ''" />
        </p>
        <el-input
          v-model="overrideReason"
          type="textarea"
          :rows="3"
          :placeholder="t('assembly.overridePlaceholder')"
        />
      </div>
      <template #footer>
        <el-button @click="overrideDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="submitOverride">{{ t('assembly.overrideSubmit') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Rank } from '@element-plus/icons-vue'
import StatusBadge from '../components/StatusBadge.vue'
import StepItemsEditor from '../components/StepItemsEditor.vue'
import {
  applyAssemblyTemplate,
  createAssemblyStep,
  deleteAssemblyStep,
  listAssemblySteps,
  reorderAssemblySteps,
  transitionAssemblyStep,
  type AssemblyStep
} from '../api/assembly'
import { createTemplate, generateSteps, type StepTemplateItem } from '../api/stepTemplates'
import { listMembers, type ProjectMember } from '../api/projects'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

const { t } = useI18n()

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
// AI generation dialog state: input (enter natural language) → result (edit candidates)
const aiDialog = ref(false)
const aiStage = ref<'input' | 'result'>('input')
const aiPrompt = ref('')
const aiNotice = ref('')
const aiNoticeType = ref<'warning' | 'error'>('warning')
const aiGenerating = ref(false)
const aiSubmitting = ref(false)
const aiName = ref('')
const aiItems = ref<StepTemplateItem[]>([])
const aiKey = ref(0)
const statuses = computed(() => [
  { value: 'planned', label: t('assembly.status.planned') },
  { value: 'in_progress', label: t('assembly.status.in_progress') },
  { value: 'paused', label: t('assembly.status.paused') },
  { value: 'completed', label: t('assembly.status.completed') },
  { value: 'skipped', label: t('assembly.status.skipped') },
  { value: 'cancelled', label: t('assembly.status.cancelled') }
])
// Consistent with the backend state machine: planned→start/cancel; in_progress→pause/complete/skip/cancel; paused→resume/cancel; skipped→start
const transitionsByStatus = computed<Record<string, TransitionAction[]>>(() => ({
  planned: [
    { transition: 'start', label: t('assembly.action.start'), confirm: false },
    { transition: 'cancel', label: t('assembly.action.cancel'), confirm: true, danger: true }
  ],
  in_progress: [
    { transition: 'pause', label: t('assembly.action.pause'), confirm: false },
    { transition: 'complete', label: t('assembly.action.complete'), confirm: true },
    { transition: 'skip', label: t('assembly.action.skip'), confirm: true },
    { transition: 'cancel', label: t('assembly.action.cancel'), confirm: true, danger: true }
  ],
  paused: [
    { transition: 'resume', label: t('assembly.action.resume'), confirm: false },
    { transition: 'cancel', label: t('assembly.action.cancel'), confirm: true, danger: true }
  ],
  skipped: [{ transition: 'start', label: t('assembly.action.restart'), confirm: false }]
}))

const canOperate = computed(() => ['admin', 'maintainer', 'member'].includes(auth.user?.role || ''))
const canReorder = computed(() => ['admin', 'maintainer'].includes(auth.user?.role || ''))
// Only admin/maintainer can create step templates on the backend
const canSaveTemplate = computed(() => ['admin', 'maintainer'].includes(auth.user?.role || ''))
// projectId's single source of truth is the route param (guaranteed by ProjectLayout)
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
    steps.value = (data.items ?? []).slice().sort((a, b) => a.step_order - b.step_order)
    members.value = memberList ?? []
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : t('assembly.loadFailed')
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

function fmtTime(time?: string) {
  return time ? new Date(time).toLocaleString(undefined, { hour12: false }) : '—'
}

async function onTransition(step: AssemblyStep, action: TransitionAction) {
  // start/resume check dependency: show override dialog when prerequisite is not completed
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
      await ElMessageBox.confirm(
        t('assembly.confirmTransition', { action: action.label, name: step.name }),
        t('assembly.transitionTitle'),
        { type: 'warning' }
      )
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
    ElMessage.success(t('assembly.statusUpdated'))
    await load()
  } catch (err) {
    showApiError(err, t('assembly.transitionFailed'))
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
    ElMessage.success(t('assembly.orderUpdated'))
    await load()
  } catch (err) {
    showApiError(err, t('assembly.reorderFailed'))
    await load()
  }
}

async function create() {
  if (!draft.name.trim()) {
    ElMessage.warning(t('assembly.nameRequired'))
    return
  }
  try {
    // step_order is not sent; the server assigns max+1 automatically
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
    ElMessage.success(t('assembly.created'))
    await load()
  } catch (err) {
    showApiError(err, t('assembly.createFailed'))
  }
}

async function remove(step: AssemblyStep) {
  try {
    await ElMessageBox.confirm(
      t('assembly.confirmDelete', { name: step.name }),
      t('assembly.deleteTitle'),
      { type: 'warning' }
    )
  } catch {
    return
  }
  try {
    await deleteAssemblyStep(step.id)
    ElMessage.success(t('assembly.deleted'))
    await load()
  } catch (err) {
    showApiError(err, t('assembly.deleteFailed'))
  }
}

function resetAi() {
  aiStage.value = 'input'
  aiPrompt.value = ''
  aiNotice.value = ''
  aiName.value = ''
  aiItems.value = []
}

async function generate() {
  aiGenerating.value = true
  aiNotice.value = ''
  try {
    const res = await generateSteps('assembly', aiPrompt.value.trim())
    if (res.status === 'ok') {
      aiItems.value = res.steps ?? []
      aiName.value = res.name_suggestion || ''
      aiKey.value += 1
      aiStage.value = 'result'
    } else if (res.status === 'clarify') {
      aiNoticeType.value = 'warning'
      aiNotice.value = res.question
        ? t('assembly.clarifyNeeded', { question: res.question })
        : t('assembly.clarifyMore')
    } else {
      aiNoticeType.value = 'error'
      aiNotice.value = res.reason
        ? t('assembly.generateFailedReason', { reason: res.reason })
        : t('assembly.generateFailed')
    }
  } catch (err) {
    showApiError(err, t('assembly.aiFailed'))
  } finally {
    aiGenerating.value = false
  }
}

function validAiItems() {
  if (aiItems.value.length < 1 || aiItems.value.length > 30) {
    ElMessage.warning(t('assembly.itemsCount'))
    return false
  }
  if (aiItems.value.some((s) => !s.name.trim())) {
    ElMessage.warning(t('assembly.allNamesRequired'))
    return false
  }
  return true
}

async function applyInline() {
  if (!validAiItems()) return
  aiSubmitting.value = true
  try {
    await applyAssemblyTemplate(projectId.value, { steps: aiItems.value, source_prompt: aiPrompt.value.trim() })
    ElMessage.success(t('assembly.applied'))
    aiDialog.value = false
    await load()
  } catch (err) {
    showApiError(err, t('assembly.applyFailed'))
  } finally {
    aiSubmitting.value = false
  }
}

async function createTemplateFromAi() {
  if (!aiName.value.trim()) {
    ElMessage.warning(t('assembly.templateNameRequired'))
    return null
  }
  if (!validAiItems()) return null
  return createTemplate({
    name: aiName.value.trim(),
    kind: 'assembly',
    items: aiItems.value,
    source_prompt: aiPrompt.value.trim(),
    ai_generated: true
  })
}

async function saveTemplateOnly() {
  aiSubmitting.value = true
  try {
    const templ = await createTemplateFromAi()
    if (templ) {
      ElMessage.success(t('assembly.templateSaved'))
      aiDialog.value = false
    }
  } catch (err) {
    showApiError(err, t('assembly.templateSaveFailed'))
  } finally {
    aiSubmitting.value = false
  }
}

async function saveAndApply() {
  aiSubmitting.value = true
  try {
    const templ = await createTemplateFromAi()
    if (!templ) return
    await applyAssemblyTemplate(projectId.value, { template_id: templ.id })
    ElMessage.success(t('assembly.templateSavedApplied'))
    aiDialog.value = false
    await load()
  } catch (err) {
    showApiError(err, t('assembly.saveApplyFailed'))
  } finally {
    aiSubmitting.value = false
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
