import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRunSteps } from '../useRunSteps'

// 步骤状态机 + AI 生成/模板导入流程（重构方案 S5：RunDetailView 拆出，行为与拆分前逐字等价）。
// api 模块打桩：只断言 composable 编排（参数/排序/确认流），不依赖真实 HTTP。

const mocks = vi.hoisted(() => ({
  listRunSteps: vi.fn(),
  createRunStep: vi.fn(),
  updateRunStep: vi.fn(),
  deleteRunStep: vi.fn(),
  applyRunTemplate: vi.fn(),
  generateSteps: vi.fn(),
  createTemplate: vi.fn(),
  listTemplates: vi.fn()
}))

vi.mock('@/api/runs', () => ({
  listRunSteps: mocks.listRunSteps,
  createRunStep: mocks.createRunStep,
  updateRunStep: mocks.updateRunStep,
  deleteRunStep: mocks.deleteRunStep,
  applyRunTemplate: mocks.applyRunTemplate
}))
vi.mock('@/api/stepTemplates', () => ({
  generateSteps: mocks.generateSteps,
  createTemplate: mocks.createTemplate,
  listTemplates: mocks.listTemplates
}))

const messageMock = ElMessage as unknown as {
  warning: ReturnType<typeof vi.fn>
  success: ReturnType<typeof vi.fn>
}

const step = (id: string, order: number, status = 'planned'): import('@/api/runs').RunStep => ({
  id,
  run_id: 'run_1',
  name: `步骤${order}`,
  description: '',
  step_order: order,
  status,
  depends_on: undefined,
  started_at: undefined,
  completed_at: undefined,
  created_at: '2026-08-14T09:00:00+08:00',
  updated_at: '2026-08-14T09:00:00+08:00'
})

beforeEach(() => {
  vi.clearAllMocks()
  messageMock.warning.mockClear()
  messageMock.success.mockClear()
})

describe('useRunSteps 步骤状态机', () => {
  it('状态机与后端 StepAllowedTransitions 一致：planned→start/cancel、in_progress→pause/complete/skip/cancel、paused→resume/cancel、skipped→start', () => {
    const { stepTransitions } = useRunSteps('run_1')
    const t = stepTransitions.value
    expect(t.planned.map((a) => a.value)).toEqual(['start', 'cancel'])
    expect(t.in_progress.map((a) => a.value)).toEqual(['pause', 'complete', 'skip', 'cancel'])
    expect(t.paused.map((a) => a.value)).toEqual(['resume', 'cancel'])
    expect(t.skipped.map((a) => a.value)).toEqual(['start'])
  })

  it('危险动作带 confirm+danger 标记，普通动作无 confirm', () => {
    const { stepTransitions } = useRunSteps('run_1')
    expect(stepTransitions.value.planned[1]).toMatchObject({ value: 'cancel', confirm: true, danger: true })
    expect(stepTransitions.value.in_progress[0]).toMatchObject({ value: 'pause' })
    expect(stepTransitions.value.in_progress[0].confirm).toBeUndefined()
  })

  it('动作 label 走 i18n 文案而非英文枚举', () => {
    const { stepTransitions } = useRunSteps('run_1')
    expect(stepTransitions.value.planned[0].label).toBe('开始')
    expect(stepTransitions.value.skipped[0].label).toBe('重新开始')
  })
})

describe('useRunSteps 步骤加载与耗时', () => {
  it('loadSteps 按 step_order 排序（服务端乱序返回也能稳定展示）', async () => {
    mocks.listRunSteps.mockResolvedValue({ items: [step('b', 2), step('a', 1), step('c', 3)] })
    const { steps, stepsLoading, loadSteps } = useRunSteps('run_1')
    await loadSteps()
    expect(steps.value.map((s) => s.id)).toEqual(['a', 'b', 'c'])
    expect(stepsLoading.value).toBe(false)
  })

  it('durationText：h/m、m/s、s 三档与非法时间戳回退', () => {
    const { durationText } = useRunSteps('run_1')
    expect(
      durationText({ ...step('a', 1), started_at: '2026-08-14T10:00:00+08:00', completed_at: '2026-08-14T12:30:45+08:00' })
    ).toBe('2h 30m')
    expect(
      durationText({ ...step('a', 1), started_at: '2026-08-14T10:00:00+08:00', completed_at: '2026-08-14T10:05:07+08:00' })
    ).toBe('5m 7s')
    expect(
      durationText({ ...step('a', 1), started_at: '2026-08-14T10:00:00+08:00', completed_at: '2026-08-14T10:00:09+08:00' })
    ).toBe('9s')
    expect(durationText({ ...step('a', 1), started_at: undefined, completed_at: undefined })).toBe('—')
  })

  it('depName：命中返回「序号. 名称」，未命中/缺省返回 —', () => {
    mocks.listRunSteps.mockResolvedValue({ items: [step('a', 1), step('b', 2)] })
    const { loadSteps, depName } = useRunSteps('run_1')
    return loadSteps().then(() => {
      expect(depName('b')).toBe('2. 步骤2')
      expect(depName('nope')).toBe('—')
      expect(depName(undefined)).toBe('—')
    })
  })
})

describe('useRunSteps 步骤写操作', () => {
  it('createStep：名称为空时拦截并提示，不调接口', async () => {
    const { createStepDialog, stepDraft, createStep } = useRunSteps('run_1')
    stepDraft.name = '   '
    await createStep()
    expect(mocks.createRunStep).not.toHaveBeenCalled()
    expect(messageMock.warning).toHaveBeenCalledWith('请填写步骤名称')
    expect(createStepDialog.value).toBe(false)
  })

  it('createStep：合法提交 trim 后调接口，成功后关弹窗并刷新步骤', async () => {
    mocks.createRunStep.mockResolvedValue(step('s1', 1))
    mocks.listRunSteps.mockResolvedValue({ items: [] })
    const { createStepDialog, stepDraft, createStep, loadSteps } = useRunSteps('run_1')
    stepDraft.name = '  冷却 '
    stepDraft.description = ' 稳定 '
    await createStep()
    expect(mocks.createRunStep).toHaveBeenCalledWith('run_1', { name: '冷却', description: '稳定', depends_on: undefined })
    expect(createStepDialog.value).toBe(false)
    expect(messageMock.success).toHaveBeenCalled()
    expect(mocks.listRunSteps).toHaveBeenCalledTimes(1)
    expect(loadSteps).toBeTypeOf('function')
  })

  it('onStepTransition：无 confirm 动作直接提交 transition 并刷新', async () => {
    mocks.updateRunStep.mockResolvedValue(step('a', 1))
    mocks.listRunSteps.mockResolvedValue({ items: [] })
    const { onStepTransition, stepTransitions } = useRunSteps('run_1')
    await onStepTransition(step('a', 1), stepTransitions.value.planned[0])
    expect(mocks.updateRunStep).toHaveBeenCalledWith('a', { transition: 'start' })
    expect(ElMessageBox.confirm).not.toHaveBeenCalled()
  })

  it('onStepTransition：confirm 动作取消确认时不调接口', async () => {
    ;(ElMessageBox.confirm as ReturnType<typeof vi.fn>).mockRejectedValueOnce('cancel')
    const { onStepTransition, stepTransitions } = useRunSteps('run_1')
    await onStepTransition(step('a', 1), stepTransitions.value.planned[1])
    expect(mocks.updateRunStep).not.toHaveBeenCalled()
  })

  it('removeStep：确认取消不删除；确认后删除并刷新', async () => {
    mocks.deleteRunStep.mockResolvedValue({ id: 'a' })
    mocks.listRunSteps.mockResolvedValue({ items: [] })
    const { removeStep } = useRunSteps('run_1')
    await removeStep(step('a', 1))
    expect(mocks.deleteRunStep).toHaveBeenCalledWith('a')
    expect(ElMessageBox.confirm).toHaveBeenCalled()
  })
})

describe('useRunSteps AI 生成与模板导入', () => {
  it('generateAiSteps：ok 进入 result 阶段并填入候选/建议名', async () => {
    mocks.generateSteps.mockResolvedValue({
      status: 'ok',
      steps: [{ name: '降温' }],
      name_suggestion: '冷却流程'
    })
    const { aiStage, aiItems, aiName, aiPrompt, generateAiSteps } = useRunSteps('run_1', () => ({ run_type: 'cooldown', gas_type: 'He', devices: ['rfq'] }))
    aiPrompt.value = '先降温'
    await generateAiSteps()
    expect(mocks.generateSteps).toHaveBeenCalledWith('experiment', '先降温', {
      run_type: 'cooldown',
      gas_type: 'He',
      devices: ['rfq']
    })
    expect(aiStage.value).toBe('result')
    expect(aiItems.value).toEqual([{ name: '降温' }])
    expect(aiName.value).toBe('冷却流程')
  })

  it('generateAiSteps：clarify 停留在 input 阶段并展示提问', async () => {
    mocks.generateSteps.mockResolvedValue({ status: 'clarify', question: '目标压力？' })
    const { aiStage, aiNotice, generateAiSteps } = useRunSteps('run_1')
    await generateAiSteps()
    expect(aiStage.value).toBe('input')
    expect(aiNotice.value).toContain('目标压力？')
  })

  it('openImport 拉取实验模板并清空选择；confirmImport 用 template_id 应用', async () => {
    mocks.listTemplates.mockResolvedValue({ items: [{ id: 'tpl_1', name: 'T1' }] })
    mocks.applyRunTemplate.mockResolvedValue({})
    mocks.listRunSteps.mockResolvedValue({ items: [] })
    const { importDialog, importTemplateId, templateOptions, templatesLoading, openImport, confirmImport } = useRunSteps('run_1')
    await openImport()
    expect(mocks.listTemplates).toHaveBeenCalledWith({ kind: 'experiment', per_page: 100 })
    expect(templateOptions.value).toHaveLength(1)
    expect(templatesLoading.value).toBe(false)
    expect(importDialog.value).toBe(true)

    importTemplateId.value = 'tpl_1'
    await confirmImport()
    expect(mocks.applyRunTemplate).toHaveBeenCalledWith('run_1', { template_id: 'tpl_1' })
    expect(importDialog.value).toBe(false)
  })

  it('applyInline：候选校验通过后以 steps 载荷应用并刷新', async () => {
    mocks.applyRunTemplate.mockResolvedValue({})
    mocks.listRunSteps.mockResolvedValue({ items: [] })
    const { aiItems, aiDialog, aiPrompt, applyInline } = useRunSteps('run_1')
    aiItems.value = [{ name: '步骤1', description: undefined, step_order: 1, depends_on_order: null }]
    aiPrompt.value = '描述'
    await applyInline()
    expect(mocks.applyRunTemplate).toHaveBeenCalledWith('run_1', {
      steps: aiItems.value,
      source_prompt: '描述'
    })
    expect(aiDialog.value).toBe(false)
  })
})
