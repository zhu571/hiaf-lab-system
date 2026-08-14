import { computed, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { i18n } from '@/i18n'
import { showApiError } from '@/composables/useNotify'
import {
  applyRunTemplate,
  createRunStep,
  deleteRunStep,
  listRunSteps,
  transitionRun,
  updateRunStep,
  type RunStep
} from '@/api/runs'
import { createTemplate, generateSteps, listTemplates, type StepTemplate, type StepTemplateItem } from '@/api/stepTemplates'

// 批次详情「步骤」面板状态机与 AI/模板生成流程（重构方案 S5：从 RunDetailView 616 行 script 拆出）。
// 行为与拆分前逐字等价；i18n 走 i18n.global（useNotify 先例），纯逻辑可在无组件上下文的单测中直接调用。

type StepAction = { value: string; label: string; confirm?: boolean; danger?: boolean }

export type AiContext = { run_type?: string; gas_type?: string; devices?: string[] }

export function useRunSteps(runId: string, getAiContext?: () => AiContext | undefined) {
  const t = (key: string, params?: Record<string, unknown>) =>
    (params ? i18n.global.t(key, params) : i18n.global.t(key)) as string

  const steps = ref<RunStep[]>([])
  const stepsLoading = ref(false)
  const createStepDialog = ref(false)
  const stepSaving = ref(false)
  const stepDraft = reactive({ name: '', description: '', depends_on: '' })
  // AI 生成步骤对话框状态：input（输入自然语言）→ result（编辑候选）
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
  const importDialog = ref(false)
  const importTemplateId = ref('')
  const templateOptions = ref<StepTemplate[]>([])
  const templatesLoading = ref(false)
  const importing = ref(false)

  // 与后端 StepAllowedTransitions 保持一致：planned→start/cancel；in_progress→pause/complete/skip/cancel；paused→resume/cancel；skipped→start
  const stepTransitions = computed<Record<string, StepAction[]>>(() => ({
    planned: [
      { value: 'start', label: t('runDetail.action.start') },
      { value: 'cancel', label: t('runDetail.action.cancel'), confirm: true, danger: true }
    ],
    in_progress: [
      { value: 'pause', label: t('runDetail.action.pause') },
      { value: 'complete', label: t('runDetail.action.complete'), confirm: true },
      { value: 'skip', label: t('runDetail.action.skip'), confirm: true },
      { value: 'cancel', label: t('runDetail.action.cancel'), confirm: true, danger: true }
    ],
    paused: [
      { value: 'resume', label: t('runDetail.action.resume') },
      { value: 'cancel', label: t('runDetail.action.cancel'), confirm: true, danger: true }
    ],
    skipped: [{ value: 'start', label: t('runDetail.action.restart') }]
  }))

  // 步骤变更后只刷新步骤列表，不触发主 load()
  async function loadSteps() {
    stepsLoading.value = true
    try {
      const data = await listRunSteps(runId)
      steps.value = (data.items ?? []).slice().sort((a, b) => a.step_order - b.step_order)
    } catch (err) {
      showApiError(err, t('runDetail.stepsLoadFailed'))
    } finally {
      stepsLoading.value = false
    }
  }

  function depName(id?: string) {
    if (!id) return '—'
    const dep = steps.value.find((s) => s.id === id)
    return dep ? `${dep.step_order}. ${dep.name}` : '—'
  }

  // 步骤耗时：started_at → completed_at（无时间戳返回 —）
  function durationText(step: RunStep) {
    if (!step.started_at || !step.completed_at) return '—'
    const start = new Date(step.started_at).getTime()
    const end = new Date(step.completed_at).getTime()
    if (!(end >= start) || Number.isNaN(start) || Number.isNaN(end)) return '—'
    const totalSec = Math.floor((end - start) / 1000)
    const h = Math.floor(totalSec / 3600)
    const m = Math.floor((totalSec % 3600) / 60)
    const s = totalSec % 60
    if (h > 0) return `${h}h ${m}m`
    if (m > 0) return `${m}m ${s}s`
    return `${s}s`
  }

  async function onStepTransition(step: RunStep, action: StepAction) {
    if (action.confirm) {
      try {
        await ElMessageBox.confirm(t('runDetail.confirmStepTransition', { action: action.label, name: step.name }), t('runDetail.stepTransitionTitle'), { type: 'warning' })
      } catch {
        return
      }
    }
    try {
      await updateRunStep(step.id, { transition: action.value })
      ElMessage.success(t('runDetail.statusUpdated'))
      await loadSteps()
    } catch (err) {
      showApiError(err, t('runDetail.stepTransitionFailed'))
    }
  }

  function openCreateStep() {
    stepDraft.name = ''
    stepDraft.description = ''
    stepDraft.depends_on = ''
    createStepDialog.value = true
  }

  async function createStep() {
    if (!stepDraft.name.trim()) {
      ElMessage.warning(t('runDetail.stepNameRequired'))
      return
    }
    stepSaving.value = true
    try {
      // step_order 不传，由服务端自动取 max+1
      await createRunStep(runId, {
        name: stepDraft.name.trim(),
        description: stepDraft.description.trim() || undefined,
        depends_on: stepDraft.depends_on || undefined
      })
      createStepDialog.value = false
      ElMessage.success(t('runDetail.stepCreated'))
      await loadSteps()
    } catch (err) {
      showApiError(err, t('runDetail.stepCreateFailed'))
    } finally {
      stepSaving.value = false
    }
  }

  async function removeStep(step: RunStep) {
    try {
      await ElMessageBox.confirm(t('runDetail.confirmDeleteStep', { name: step.name }), t('runDetail.deleteStepTitle'), { type: 'warning' })
    } catch {
      return
    }
    try {
      await deleteRunStep(step.id)
      ElMessage.success(t('runDetail.stepDeleted'))
      await loadSteps()
    } catch (err) {
      showApiError(err, t('runDetail.stepDeleteFailed'))
    }
  }

  function resetAi() {
    aiStage.value = 'input'
    aiPrompt.value = ''
    aiNotice.value = ''
    aiName.value = ''
    aiItems.value = []
  }

  async function generateAiSteps() {
    aiGenerating.value = true
    aiNotice.value = ''
    try {
      // 带上批次上下文（类型/气体/设备），帮助模型生成更贴合的步骤
      const context = getAiContext?.()
      const res = await generateSteps('experiment', aiPrompt.value.trim(), context)
      if (res.status === 'ok') {
        aiItems.value = res.steps ?? []
        aiName.value = res.name_suggestion || ''
        aiKey.value += 1
        aiStage.value = 'result'
      } else if (res.status === 'clarify') {
        aiNoticeType.value = 'warning'
        aiNotice.value = res.question ? t('runDetail.clarifyNeeded', { question: res.question }) : t('runDetail.clarifyMore')
      } else {
        aiNoticeType.value = 'error'
        aiNotice.value = res.reason ? t('runDetail.generateFailedReason', { reason: res.reason }) : t('runDetail.generateFailed')
      }
    } catch (err) {
      showApiError(err, t('runDetail.aiFailed'))
    } finally {
      aiGenerating.value = false
    }
  }

  function validAiItems() {
    if (aiItems.value.length < 1 || aiItems.value.length > 30) {
      ElMessage.warning(t('runDetail.itemsCount'))
      return false
    }
    if (aiItems.value.some((s) => !s.name.trim())) {
      ElMessage.warning(t('runDetail.allNamesRequired'))
      return false
    }
    return true
  }

  async function applyInline() {
    if (!validAiItems()) return
    aiSubmitting.value = true
    try {
      await applyRunTemplate(runId, { steps: aiItems.value, source_prompt: aiPrompt.value.trim() })
      ElMessage.success(t('runDetail.stepsApplied'))
      aiDialog.value = false
      await loadSteps()
    } catch (err) {
      showApiError(err, t('runDetail.applyFailed'))
    } finally {
      aiSubmitting.value = false
    }
  }

  async function createTemplateFromAi() {
    if (!aiName.value.trim()) {
      ElMessage.warning(t('runDetail.templateNameRequired'))
      return null
    }
    if (!validAiItems()) return null
    return createTemplate({
      name: aiName.value.trim(),
      kind: 'experiment',
      items: aiItems.value,
      source_prompt: aiPrompt.value.trim(),
      ai_generated: true
    })
  }

  async function saveTemplateOnly() {
    aiSubmitting.value = true
    try {
      const tpl = await createTemplateFromAi()
      if (tpl) {
        ElMessage.success(t('runDetail.templateSaved'))
        aiDialog.value = false
      }
    } catch (err) {
      showApiError(err, t('runDetail.templateSaveFailed'))
    } finally {
      aiSubmitting.value = false
    }
  }

  async function saveAndApplyTemplate() {
    aiSubmitting.value = true
    try {
      const tpl = await createTemplateFromAi()
      if (!tpl) return
      await applyRunTemplate(runId, { template_id: tpl.id })
      ElMessage.success(t('runDetail.templateSavedApplied'))
      aiDialog.value = false
      await loadSteps()
    } catch (err) {
      showApiError(err, t('runDetail.saveApplyFailed'))
    } finally {
      aiSubmitting.value = false
    }
  }

  async function openImport() {
    importTemplateId.value = ''
    templateOptions.value = []
    importDialog.value = true
    templatesLoading.value = true
    try {
      const res = await listTemplates({ kind: 'experiment', per_page: 100 })
      templateOptions.value = res.items ?? []
    } catch (err) {
      showApiError(err, t('runDetail.templatesLoadFailed'))
    } finally {
      templatesLoading.value = false
    }
  }

  async function confirmImport() {
    if (!importTemplateId.value) return
    importing.value = true
    try {
      await applyRunTemplate(runId, { template_id: importTemplateId.value })
      ElMessage.success(t('runDetail.imported'))
      importDialog.value = false
      await loadSteps()
    } catch (err) {
      showApiError(err, t('runDetail.importFailed'))
    } finally {
      importing.value = false
    }
  }

  return {
    steps,
    stepsLoading,
    stepTransitions,
    stepDraft,
    createStepDialog,
    stepSaving,
    aiDialog,
    aiStage,
    aiPrompt,
    aiNotice,
    aiNoticeType,
    aiGenerating,
    aiSubmitting,
    aiName,
    aiItems,
    aiKey,
    importDialog,
    importTemplateId,
    templateOptions,
    templatesLoading,
    importing,
    loadSteps,
    depName,
    durationText,
    onStepTransition,
    openCreateStep,
    createStep,
    removeStep,
    resetAi,
    generateAiSteps,
    validAiItems,
    applyInline,
    createTemplateFromAi,
    saveTemplateOnly,
    saveAndApplyTemplate,
    openImport,
    confirmImport
  }
}
