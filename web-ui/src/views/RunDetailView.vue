<template>
  <div v-loading="loading" class="page detail-page">
    <section v-if="error" class="panel error-box">
      <el-alert :title="error" type="error" show-icon :closable="false" />
      <el-button @click="load">重试</el-button>
    </section>
    <el-empty v-else-if="!run && !loading" description="未找到实验批次" />
    <template v-else-if="run">
      <section class="panel">
        <div class="head-row">
          <el-button class="back-btn" @click="goBack">返回</el-button>
          <div class="title-block">
            <h2>{{ run.name }}</h2>
            <StatusBadge :value="run.status" />
          </div>
          <div v-if="canEdit" class="actions">
            <el-button v-for="t in transitions" :key="t.value" :type="t.type" :loading="transitioning" @click="doTransition(t)">
              {{ t.label }}
            </el-button>
            <el-button @click="openEdit">编辑元数据</el-button>
            <el-button type="danger" plain @click="remove">删除</el-button>
          </div>
        </div>
      </section>
      <el-tabs v-model="activeTab" class="page-tabs">
        <el-tab-pane label="概览" name="overview">
          <div class="tab-stack">
            <section class="panel">
              <h3 class="panel-title">元信息</h3>
              <el-descriptions :column="2" border>
                <el-descriptions-item label="Campaign">{{ run.campaign || '—' }}</el-descriptions-item>
                <el-descriptions-item label="类型">{{ runTypeLabel(run.run_type) }}</el-descriptions-item>
                <el-descriptions-item label="气体">{{ run.gas_type || '—' }}</el-descriptions-item>
                <el-descriptions-item label="束流">{{ run.has_beam ? '有' : '无' }}</el-descriptions-item>
                <el-descriptions-item label="目标温度">{{ numText(run.target_temp) }}</el-descriptions-item>
                <el-descriptions-item label="最低温度">{{ numText(run.min_temp) }}</el-descriptions-item>
                <el-descriptions-item label="压力范围">{{ pressureText }}</el-descriptions-item>
                <el-descriptions-item label="设备">
                  <template v-if="run.devices?.length">
                    <el-tag v-for="d in run.devices" :key="d" size="small" effect="plain" class="dev-tag">{{ d }}</el-tag>
                  </template>
                  <span v-else>—</span>
                </el-descriptions-item>
                <el-descriptions-item label="创建时间">{{ fmtTime(run.created_at) }}</el-descriptions-item>
                <el-descriptions-item label="开始时间">{{ fmtTime(run.started_at) }}</el-descriptions-item>
                <el-descriptions-item label="结束时间">{{ fmtTime(run.ended_at) }}</el-descriptions-item>
                <el-descriptions-item label="描述" :span="2">
                  <p class="desc">{{ run.description || '—' }}</p>
                </el-descriptions-item>
              </el-descriptions>
            </section>
            <section class="panel">
              <h3 class="panel-title">状态时间线</h3>
              <!-- 后端暂无状态历史接口，时间线仅由已知时间戳（created/started/ended）+ 当前状态构建 -->
              <el-timeline>
                <el-timeline-item
                  v-for="(item, i) in timeline"
                  :key="i"
                  :timestamp="item.time ? fmtTime(item.time) : ''"
                  :type="item.type"
                  :hollow="!item.time"
                >
                  {{ item.label }}
                </el-timeline-item>
              </el-timeline>
            </section>
          </div>
        </el-tab-pane>
        <el-tab-pane label="步骤" name="steps">
          <section class="panel">
            <div class="steps-toolbar">
              <h3 class="panel-title">实验步骤</h3>
              <div v-if="canEdit" class="steps-actions">
                <el-button size="small" type="primary" plain @click="aiDialog = true">AI 生成步骤</el-button>
                <el-button size="small" plain @click="openImport">从模板导入</el-button>
                <el-button size="small" type="primary" @click="openCreateStep">手动新建</el-button>
              </div>
            </div>
            <el-table v-loading="stepsLoading" :data="steps" size="small">
              <el-table-column label="#" width="50" align="center">
                <template #default="{ row }">{{ row.step_order }}</template>
              </el-table-column>
              <el-table-column prop="name" label="名称" min-width="140" />
              <el-table-column label="描述" min-width="180">
                <template #default="{ row }">{{ row.description || '—' }}</template>
              </el-table-column>
              <el-table-column label="状态" width="100">
                <template #default="{ row }"><StatusBadge :value="row.status" /></template>
              </el-table-column>
              <el-table-column label="依赖步骤" width="140">
                <template #default="{ row }">{{ depName(row.depends_on) }}</template>
              </el-table-column>
              <el-table-column v-if="canEdit" label="操作" width="280" fixed="right">
                <template #default="{ row }">
                  <el-button
                    v-for="a in stepTransitions[row.status] || []"
                    :key="a.value"
                    size="small"
                    :type="a.danger ? 'danger' : 'primary'"
                    plain
                    @click="onStepTransition(row, a)"
                  >
                    {{ a.label }}
                  </el-button>
                  <el-button size="small" type="danger" plain @click="removeStep(row)">删除</el-button>
                </template>
              </el-table-column>
              <template #empty>
                <el-empty description="暂无实验步骤" :image-size="60" />
              </template>
            </el-table>
          </section>
        </el-tab-pane>
        <el-tab-pane label="关联日报" name="reports">
          <section class="panel">
            <h3 class="panel-title">关联日报</h3>
            <!-- 后端暂无查询既有关联的端点，下列列表仅反映本次会话中的关联/解绑操作结果 -->
            <div v-if="canEdit" class="link-row">
              <el-select v-model="selectedReportId" class="report-select" filterable placeholder="选择日报" :loading="reportsLoading">
                <el-option v-for="r in reportOptions" :key="r.id" :label="reportLabel(r)" :value="r.id" />
              </el-select>
              <el-button type="primary" :disabled="!selectedReportId" :loading="linking" @click="link">关联</el-button>
            </div>
            <el-empty v-if="!linkedReportIds.length" description="暂无关联日报" :image-size="60" />
            <ul v-else class="link-list">
              <li v-for="id in linkedReportIds" :key="id">
                <span class="report-id">{{ id }}</span>
                <el-button v-if="canEdit" size="small" type="danger" plain @click="unlink(id)">解绑</el-button>
              </li>
            </ul>
          </section>
        </el-tab-pane>
        <el-tab-pane label="测试数据" name="testdata">
          <section class="panel">
            <h3 class="panel-title">关联测试数据</h3>
            <el-table v-loading="testDataLoading" :data="testData" size="small">
              <el-table-column prop="measurement" label="测量项" />
              <el-table-column prop="value" label="值" width="120" />
              <el-table-column prop="unit" label="单位" width="100" />
              <el-table-column prop="quality" label="质量" width="100" />
              <el-table-column label="测量时间" width="180">
                <template #default="{ row }">{{ row.measured_at ? fmtTime(row.measured_at) : '—' }}</template>
              </el-table-column>
              <template #empty>
                <el-empty description="暂无关联测试数据" :image-size="60" />
              </template>
            </el-table>
          </section>
        </el-tab-pane>
      </el-tabs>
    </template>
    <el-dialog v-model="editDialog" title="编辑元数据" width="620">
      <el-form label-position="top">
        <el-form-item label="名称（必填）"><el-input v-model="editDraft.name" /></el-form-item>
        <el-form-item label="Campaign"><el-input v-model="editDraft.campaign" /></el-form-item>
        <div class="form-row">
          <el-form-item label="类型">
            <el-select v-model="editDraft.run_type">
              <el-option v-for="t in runTypes" :key="t.value" :label="t.label" :value="t.value" />
            </el-select>
          </el-form-item>
          <el-form-item label="气体">
            <el-select v-model="editDraft.gas_type">
              <el-option v-for="g in gasTypes" :key="g" :label="g" :value="g" />
            </el-select>
          </el-form-item>
        </div>
        <div class="form-row">
          <el-form-item label="目标温度"><el-input-number v-model="editDraft.target_temp" :controls="false" placeholder="可选" /></el-form-item>
          <el-form-item label="最低温度"><el-input-number v-model="editDraft.min_temp" :controls="false" placeholder="可选" /></el-form-item>
        </div>
        <div class="form-row three">
          <el-form-item label="压力下限"><el-input-number v-model="editDraft.pressure_min" :controls="false" placeholder="可选" /></el-form-item>
          <el-form-item label="压力上限"><el-input-number v-model="editDraft.pressure_max" :controls="false" placeholder="可选" /></el-form-item>
          <el-form-item label="压力单位"><el-input v-model="editDraft.pressure_unit" /></el-form-item>
        </div>
        <el-form-item label="有束流"><el-switch v-model="editDraft.has_beam" /></el-form-item>
        <el-form-item label="设备">
          <el-select v-model="editDraft.devices" multiple placeholder="选择设备">
            <el-option v-for="d in deviceOptions" :key="d" :label="d" :value="d" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述"><el-input v-model="editDraft.description" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveEdit">保存</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="createStepDialog" title="新建步骤" width="520">
      <el-form label-position="top">
        <el-form-item label="名称" required><el-input v-model="stepDraft.name" maxlength="256" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="stepDraft.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="依赖步骤">
          <el-select v-model="stepDraft.depends_on" clearable placeholder="无依赖">
            <el-option v-for="s in steps" :key="s.id" :label="`${s.step_order}. ${s.name}`" :value="s.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createStepDialog = false">取消</el-button>
        <el-button type="primary" :loading="stepSaving" @click="createStep">保存</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="aiDialog" title="AI 生成步骤" width="760" @closed="resetAi">
      <div v-if="aiStage === 'input'" class="grid">
        <el-alert v-if="aiNotice" :type="aiNoticeType" :title="aiNotice" show-icon :closable="false" />
        <el-input
          v-model="aiPrompt"
          type="textarea"
          :rows="4"
          maxlength="4000"
          placeholder="用自然语言描述实验流程，例如：先降温到 4K 并稳定，再充气到目标压力，最后开始数据采集"
        />
      </div>
      <div v-else class="grid">
        <el-form label-position="top">
          <el-form-item label="模板名称（存模板时使用）">
            <el-input v-model="aiName" maxlength="256" />
          </el-form-item>
        </el-form>
        <StepItemsEditor :key="aiKey" v-model="aiItems" />
      </div>
      <template #footer>
        <template v-if="aiStage === 'input'">
          <el-button @click="aiDialog = false">取消</el-button>
          <el-button type="primary" :loading="aiGenerating" :disabled="!aiPrompt.trim()" @click="generateAiSteps">生成候选</el-button>
        </template>
        <template v-else>
          <el-button @click="aiStage = 'input'">返回修改</el-button>
          <el-button :loading="aiSubmitting" @click="applyInline">直接应用</el-button>
          <el-button v-if="canSaveTemplate" :loading="aiSubmitting" @click="saveTemplateOnly">存模板</el-button>
          <el-button v-if="canSaveTemplate" type="primary" :loading="aiSubmitting" @click="saveAndApply">存并应用</el-button>
        </template>
      </template>
    </el-dialog>
    <el-dialog v-model="importDialog" title="从模板导入" width="520">
      <div class="grid">
        <p class="import-tip">将所选实验模板的步骤追加到当前批次。</p>
        <el-select v-model="importTemplateId" v-loading="templatesLoading" filterable placeholder="选择实验步骤模板" class="import-select">
          <el-option v-for="t in templateOptions" :key="t.id" :label="t.name" :value="t.id" />
        </el-select>
      </div>
      <template #footer>
        <el-button @click="importDialog = false">取消</el-button>
        <el-button type="primary" :loading="importing" :disabled="!importTemplateId" @click="confirmImport">导入</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import StatusBadge from '../components/StatusBadge.vue'
import StepItemsEditor from '../components/StepItemsEditor.vue'
import {
  addReportLink,
  applyRunTemplate,
  createRunStep,
  deleteRun,
  deleteRunStep,
  getRun,
  listRunSteps,
  removeReportLink,
  transitionRun,
  updateRun,
  updateRunStep,
  type ExperimentRun,
  type RunPayload,
  type RunStep
} from '../api/runs'
import { createTemplate, generateSteps, listTemplates, type StepTemplate, type StepTemplateItem } from '../api/stepTemplates'
import { listReports, type DailyReport } from '../api/logs'
import { listTestData, type TestData } from '../api/testdata'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const runId = String(route.params.id || '')
const run = ref<ExperimentRun | null>(null)
const loading = ref(false)
const error = ref('')
const transitioning = ref(false)
const editDialog = ref(false)
const saving = ref(false)
const activeTab = ref('overview')

const reportOptions = ref<DailyReport[]>([])
const reportsLoading = ref(false)
const selectedReportId = ref('')
const linkedReportIds = ref<string[]>([])
const linking = ref(false)

const testData = ref<TestData[]>([])
const testDataLoading = ref(false)

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

const statusLabels: Record<string, string> = {
  planned: '计划中',
  active: '进行中',
  paused: '已暂停',
  completed: '已完成',
  aborted: '已中止'
}
const runTypes = [
  { value: 'cooldown', label: '冷却' },
  { value: 'warmup', label: '升温' },
  { value: 'steady_state', label: '稳态' },
  { value: 'test', label: '测试' }
]
const gasTypes = ['He', 'Ar', 'Xe']
const deviceOptions = ['rf_carpet', 'rfq', 'qpig']

type RunForm = {
  name: string
  campaign: string
  run_type: string
  gas_type: string
  target_temp: number | undefined
  min_temp: number | undefined
  pressure_min: number | undefined
  pressure_max: number | undefined
  pressure_unit: string
  has_beam: boolean
  devices: string[]
  description: string
}

const editDraft = reactive<RunForm>({
  name: '',
  campaign: '',
  run_type: 'cooldown',
  gas_type: 'He',
  target_temp: undefined,
  min_temp: undefined,
  pressure_min: undefined,
  pressure_max: undefined,
  pressure_unit: 'mbar',
  has_beam: false,
  devices: [],
  description: ''
})

// 状态机：planned→start/abort；active→pause/complete/abort；paused→resume/abort
type TransitionAction = { value: string; label: string; type: 'primary' | 'success' | 'warning' | 'danger'; confirm?: boolean }
const transitionMap: Record<string, TransitionAction[]> = {
  planned: [
    { value: 'start', label: '开始', type: 'primary' },
    { value: 'abort', label: '中止', type: 'danger', confirm: true }
  ],
  active: [
    { value: 'pause', label: '暂停', type: 'warning' },
    { value: 'complete', label: '完成', type: 'success', confirm: true },
    { value: 'abort', label: '中止', type: 'danger', confirm: true }
  ],
  paused: [
    { value: 'resume', label: '恢复', type: 'primary' },
    { value: 'abort', label: '中止', type: 'danger', confirm: true }
  ]
}

// viewer 只读，隐藏状态转移/编辑/删除/关联入口（后端仍强校验）
const canEdit = computed(() => !!auth.user && auth.user.role !== 'viewer')
const transitions = computed(() => (run.value ? transitionMap[run.value.status] || [] : []))
// 后端 steptemplates 创建模板仅允许 admin/maintainer
const canSaveTemplate = computed(() => ['admin', 'maintainer'].includes(auth.user?.role || ''))

// 与后端 StepAllowedTransitions 保持一致：planned→start/cancel；in_progress→pause/complete/skip/cancel；paused→resume/cancel；skipped→start
type StepAction = { value: string; label: string; confirm?: boolean; danger?: boolean }
const stepTransitions: Record<string, StepAction[]> = {
  planned: [
    { value: 'start', label: '开始' },
    { value: 'cancel', label: '取消', confirm: true, danger: true }
  ],
  in_progress: [
    { value: 'pause', label: '暂停' },
    { value: 'complete', label: '完成', confirm: true },
    { value: 'skip', label: '跳过', confirm: true },
    { value: 'cancel', label: '取消', confirm: true, danger: true }
  ],
  paused: [
    { value: 'resume', label: '恢复' },
    { value: 'cancel', label: '取消', confirm: true, danger: true }
  ],
  skipped: [{ value: 'start', label: '重新开始' }]
}

const pressureText = computed(() => {
  if (!run.value) return '—'
  const { pressure_min, pressure_max, pressure_unit } = run.value
  if (pressure_min === undefined && pressure_max === undefined) return '—'
  return `${pressure_min ?? '?'} ~ ${pressure_max ?? '?'} ${pressure_unit || ''}`.trim()
})

type TimelineItem = { time?: string; label: string; type: 'primary' | 'success' | 'warning' | 'danger' | 'info' }
// 后端无状态历史接口，只展示已知时间戳节点，末尾追加当前状态节点
const timeline = computed<TimelineItem[]>(() => {
  if (!run.value) return []
  const r = run.value
  const items: TimelineItem[] = []
  if (r.created_at) items.push({ time: r.created_at, label: '创建批次', type: 'info' })
  if (r.started_at) items.push({ time: r.started_at, label: '批次开始', type: 'primary' })
  if (r.ended_at) {
    const aborted = r.status === 'aborted'
    items.push({ time: r.ended_at, label: aborted ? '批次中止' : '批次结束', type: aborted ? 'danger' : 'success' })
  }
  items.push({ label: `当前状态：${statusLabels[r.status] || r.status}`, type: 'warning' })
  return items
})

onMounted(load)

async function load() {
  loading.value = true
  error.value = ''
  try {
    run.value = await getRun(runId)
    // 三个面板独立加载，失败不影响主内容
    await Promise.all([loadReports(), loadTestData(), loadSteps()])
  } catch (err) {
    error.value = err instanceof Error ? err.message : '批次加载失败'
    showApiError(err, '批次加载失败')
  } finally {
    loading.value = false
  }
}

async function loadReports() {
  reportsLoading.value = true
  try {
    const data = await listReports({ per_page: 50 })
    reportOptions.value = data.items ?? []
  } catch (err) {
    showApiError(err, '日报列表加载失败')
  } finally {
    reportsLoading.value = false
  }
}

async function loadTestData() {
  if (!run.value) return
  testDataLoading.value = true
  try {
    const data = await listTestData(run.value.project_id, { run_id: run.value.id, per_page: 5 })
    testData.value = data.items ?? []
  } catch (err) {
    showApiError(err, '测试数据加载失败')
  } finally {
    testDataLoading.value = false
  }
}

// 步骤变更后只刷新步骤列表，不触发主 load()
async function loadSteps() {
  stepsLoading.value = true
  try {
    const data = await listRunSteps(runId)
    steps.value = (data.items ?? []).slice().sort((a, b) => a.step_order - b.step_order)
  } catch (err) {
    showApiError(err, '步骤列表加载失败')
  } finally {
    stepsLoading.value = false
  }
}

function depName(id?: string) {
  if (!id) return '—'
  const dep = steps.value.find((s) => s.id === id)
  return dep ? `${dep.step_order}. ${dep.name}` : '—'
}

async function onStepTransition(step: RunStep, action: StepAction) {
  if (action.confirm) {
    try {
      await ElMessageBox.confirm(`确认${action.label}步骤「${step.name}」？`, '状态流转', { type: 'warning' })
    } catch {
      return
    }
  }
  try {
    await updateRunStep(step.id, { transition: action.value })
    ElMessage.success('状态已更新')
    await loadSteps()
  } catch (err) {
    showApiError(err, '状态流转失败')
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
    ElMessage.warning('请填写步骤名称')
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
    ElMessage.success('步骤已创建')
    await loadSteps()
  } catch (err) {
    showApiError(err, '步骤创建失败')
  } finally {
    stepSaving.value = false
  }
}

async function removeStep(step: RunStep) {
  try {
    await ElMessageBox.confirm(`确认删除步骤「${step.name}」？`, '删除步骤', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteRunStep(step.id)
    ElMessage.success('步骤已删除')
    await loadSteps()
  } catch (err) {
    showApiError(err, '步骤删除失败')
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
    const context = run.value
      ? { run_type: run.value.run_type, gas_type: run.value.gas_type, devices: run.value.devices ?? [] }
      : undefined
    const res = await generateSteps('experiment', aiPrompt.value.trim(), context)
    if (res.status === 'ok') {
      aiItems.value = res.steps ?? []
      aiName.value = res.name_suggestion || ''
      aiKey.value += 1
      aiStage.value = 'result'
    } else if (res.status === 'clarify') {
      aiNoticeType.value = 'warning'
      aiNotice.value = res.question ? `需要补充信息：${res.question}` : '需要补充更多信息，请完善描述后重试'
    } else {
      aiNoticeType.value = 'error'
      aiNotice.value = res.reason ? `无法生成：${res.reason}` : '无法生成步骤，请调整描述后重试'
    }
  } catch (err) {
    showApiError(err, 'AI 生成失败')
  } finally {
    aiGenerating.value = false
  }
}

function validAiItems() {
  if (aiItems.value.length < 1 || aiItems.value.length > 30) {
    ElMessage.warning('候选步骤数需在 1-30 之间')
    return false
  }
  if (aiItems.value.some((s) => !s.name.trim())) {
    ElMessage.warning('请填写所有步骤名称')
    return false
  }
  return true
}

async function applyInline() {
  if (!validAiItems()) return
  aiSubmitting.value = true
  try {
    await applyRunTemplate(runId, { steps: aiItems.value, source_prompt: aiPrompt.value.trim() })
    ElMessage.success('步骤已应用到当前批次')
    aiDialog.value = false
    await loadSteps()
  } catch (err) {
    showApiError(err, '应用失败')
  } finally {
    aiSubmitting.value = false
  }
}

async function createTemplateFromAi() {
  if (!aiName.value.trim()) {
    ElMessage.warning('请填写模板名称')
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
    const t = await createTemplateFromAi()
    if (t) {
      ElMessage.success('模板已保存，可在模板库中查看')
      aiDialog.value = false
    }
  } catch (err) {
    showApiError(err, '模板保存失败')
  } finally {
    aiSubmitting.value = false
  }
}

async function saveAndApply() {
  aiSubmitting.value = true
  try {
    const t = await createTemplateFromAi()
    if (!t) return
    await applyRunTemplate(runId, { template_id: t.id })
    ElMessage.success('模板已保存并应用到当前批次')
    aiDialog.value = false
    await loadSteps()
  } catch (err) {
    showApiError(err, '保存并应用失败')
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
    showApiError(err, '模板列表加载失败')
  } finally {
    templatesLoading.value = false
  }
}

async function confirmImport() {
  if (!importTemplateId.value) return
  importing.value = true
  try {
    await applyRunTemplate(runId, { template_id: importTemplateId.value })
    ElMessage.success('模板步骤已导入')
    importDialog.value = false
    await loadSteps()
  } catch (err) {
    showApiError(err, '导入失败')
  } finally {
    importing.value = false
  }
}

function goBack() {
  router.back()
}

async function doTransition(t: TransitionAction) {
  if (!run.value) return
  if (t.confirm) {
    try {
      await ElMessageBox.confirm(`确认对批次「${run.value.name}」执行「${t.label}」操作？`, '状态变更', {
        type: 'warning',
        confirmButtonText: '确认',
        cancelButtonText: '取消'
      })
    } catch {
      return
    }
  }
  transitioning.value = true
  try {
    // transition 必须与元数据分开提交，走独立的 PATCH 请求
    await transitionRun(run.value.id, t.value)
    ElMessage.success('状态已更新')
    await load()
  } catch (err) {
    showApiError(err, '状态更新失败')
  } finally {
    transitioning.value = false
  }
}

function openEdit() {
  if (!run.value) return
  Object.assign(editDraft, {
    name: run.value.name,
    campaign: run.value.campaign || '',
    run_type: run.value.run_type,
    gas_type: run.value.gas_type || 'He',
    target_temp: run.value.target_temp,
    min_temp: run.value.min_temp,
    pressure_min: run.value.pressure_min,
    pressure_max: run.value.pressure_max,
    pressure_unit: run.value.pressure_unit || 'mbar',
    has_beam: run.value.has_beam,
    devices: [...(run.value.devices || [])],
    description: run.value.description || ''
  })
  editDialog.value = true
}

// 只提交有值的字段，空字符串/空数组转为 undefined（JSON 序列化时会被丢弃）
function toPayload(form: RunForm): RunPayload {
  return {
    name: form.name.trim(),
    campaign: form.campaign.trim() || undefined,
    run_type: form.run_type,
    gas_type: form.gas_type,
    target_temp: form.target_temp,
    min_temp: form.min_temp,
    pressure_min: form.pressure_min,
    pressure_max: form.pressure_max,
    pressure_unit: form.pressure_unit.trim() || undefined,
    has_beam: form.has_beam,
    devices: form.devices.length ? [...form.devices] : undefined,
    description: form.description.trim() || undefined
  }
}

async function saveEdit() {
  if (!run.value) return
  if (!editDraft.name.trim()) {
    ElMessage.warning('请填写批次名称')
    return
  }
  saving.value = true
  try {
    await updateRun(run.value.id, toPayload(editDraft))
    ElMessage.success('元数据已保存')
    editDialog.value = false
    await load()
  } catch (err) {
    showApiError(err, '保存失败')
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!run.value) return
  const target = run.value
  try {
    await ElMessageBox.confirm(`确认删除批次「${target.name}」？该操作不可恢复。`, '删除批次', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  try {
    await deleteRun(target.id)
    ElMessage.success('批次已删除')
    router.push(`/projects/${target.project_id}/experiment-runs`)
  } catch (err) {
    showApiError(err, '删除失败')
  }
}

async function link() {
  if (!run.value || !selectedReportId.value) return
  linking.value = true
  try {
    const res = await addReportLink(run.value.id, selectedReportId.value)
    // 响应的 report_ids 是全量列表，直接覆盖本地状态
    linkedReportIds.value = res.report_ids
    selectedReportId.value = ''
    ElMessage.success('日报已关联')
  } catch (err) {
    showApiError(err, '关联失败')
  } finally {
    linking.value = false
  }
}

async function unlink(reportId: string) {
  if (!run.value) return
  try {
    const res = await removeReportLink(run.value.id, reportId)
    linkedReportIds.value = res.report_ids
    ElMessage.success('已解绑')
  } catch (err) {
    showApiError(err, '解绑失败')
  }
}

function reportLabel(r: DailyReport) {
  const summary = (r.summary || '').trim()
  const short = summary.length > 24 ? `${summary.slice(0, 24)}…` : summary
  return short ? `${r.report_date} · ${short}` : r.report_date
}

function runTypeLabel(value: string) {
  return runTypes.find((t) => t.value === value)?.label || value
}

function numText(n?: number) {
  return n ?? '—'
}

function fmtTime(x?: string) {
  if (!x) return '—'
  return new Date(x).toLocaleString('zh-CN', { hour12: false })
}
</script>

<style scoped>
.detail-page {
  min-height: 240px;
}

.error-box {
  display: grid;
  gap: 12px;
  justify-items: center;
  padding: 32px 0;
}

.head-row {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.title-block {
  align-items: center;
  display: flex;
  gap: 10px;
}

.title-block h2 {
  font-size: 20px;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-left: auto;
}

.page-tabs :deep(.el-tabs__header) {
  margin-bottom: 16px;
}

.tab-stack {
  display: grid;
  gap: 20px;
}

.panel-title {
  font-size: 15px;
  margin-bottom: 14px;
}

.steps-toolbar {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: space-between;
  margin-bottom: 14px;
}

.steps-toolbar .panel-title {
  margin-bottom: 0;
}

.steps-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.steps-actions .el-button + .el-button {
  margin-left: 0;
}

.import-tip {
  color: var(--text-2);
  font-size: 13px;
}

.import-select {
  width: 100%;
}

.dev-tag {
  margin-right: 6px;
}

.desc {
  color: var(--text-2);
  white-space: pre-wrap;
}

.link-row {
  display: flex;
  gap: 10px;
  margin-bottom: 12px;
}

.report-select {
  flex: 1;
  min-width: 0;
}

.link-list {
  display: grid;
  gap: 8px;
  list-style: none;
  margin: 0;
  padding: 0;
}

.link-list li {
  align-items: center;
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 8px;
  display: flex;
  justify-content: space-between;
  padding: 8px 12px;
}

.report-id {
  color: var(--text-3);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.form-row {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.form-row.three {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.form-row .el-input-number,
.form-row .el-select {
  width: 100%;
}

@media (max-width: 768px) {
  .actions {
    margin-left: 0;
  }
}
</style>
