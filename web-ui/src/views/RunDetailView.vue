<template>
  <div v-loading="loading" class="page detail-page">
    <section v-if="error" class="panel error-box">
      <el-alert :title="error" type="error" show-icon :closable="false" />
      <el-button @click="load">{{ t('runDetail.retry') }}</el-button>
    </section>
    <el-empty v-else-if="!run && !loading" :description="t('runDetail.runNotFound')" />
    <template v-else-if="run">
      <section class="panel">
        <div class="head-row">
          <el-button class="back-btn" @click="goBack">{{ t('runDetail.back') }}</el-button>
          <div class="title-block">
            <h2>{{ run.name }}</h2>
            <StatusBadge :value="run.status" />
          </div>
          <div v-if="canEdit" class="actions">
            <el-button type="primary" plain @click="aiDialog = true">{{ t('runDetail.aiGenerateSteps') }}</el-button>
            <el-button plain @click="openImport">{{ t('runDetail.importFromTemplate') }}</el-button>
            <el-button type="primary" @click="openCreateStep">{{ t('runDetail.createManually') }}</el-button>
            <el-button v-for="t in transitions" :key="t.value" :type="t.type" :loading="transitioning" @click="doTransition(t)">
              {{ t.label }}
            </el-button>
            <el-button @click="openEdit">{{ t('runDetail.editMetadata') }}</el-button>
            <el-button type="danger" plain @click="remove">{{ t('runDetail.delete') }}</el-button>
          </div>
        </div>
      </section>
      <el-tabs v-model="activeTab" class="page-tabs">
        <el-tab-pane :label="t('runDetail.overview')" name="overview">
          <div class="tab-stack">
            <section class="panel">
              <h3 class="panel-title">{{ t('runDetail.metaInfo') }}</h3>
              <el-descriptions :column="2" border>
                <el-descriptions-item label="Campaign">{{ run.campaign || '—' }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.type')">{{ runTypeLabel(run.run_type) }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.gas')">{{ run.gas_type || '—' }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.hasBeam')">{{ run.has_beam ? t('runDetail.yes') : t('runDetail.no') }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.targetTemp')">{{ numText(run.target_temp) }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.minTemp')">{{ numText(run.min_temp) }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.pressureRange')">{{ pressureText }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.devices')">
                  <template v-if="run.devices?.length">
                    <el-tag v-for="d in run.devices" :key="d" size="small" effect="plain" class="dev-tag">{{ d }}</el-tag>
                  </template>
                  <span v-else>—</span>
                </el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.createdAt')">{{ fmtTime(run.created_at) }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.startedAt')">{{ fmtTime(run.started_at) }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.endedAt')">{{ fmtTime(run.ended_at) }}</el-descriptions-item>
                <el-descriptions-item :label="t('runDetail.description')" :span="2">
                  <p class="desc">{{ run.description || '—' }}</p>
                </el-descriptions-item>
              </el-descriptions>
            </section>
            <section class="panel">
              <h3 class="panel-title">{{ t('runDetail.statusTimeline') }}</h3>
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
        <el-tab-pane :label="t('runDetail.stepsTab')" name="steps">
          <section class="panel">
            <h3 class="panel-title">{{ t('runDetail.experimentSteps') }}</h3>
            <el-table v-loading="stepsLoading" :data="steps" size="small">
              <el-table-column label="#" width="50" align="center">
                <template #default="{ row }">{{ row.step_order }}</template>
              </el-table-column>
              <el-table-column prop="name" :label="t('runDetail.name')" min-width="140" />
              <el-table-column :label="t('runDetail.description')" min-width="180">
                <template #default="{ row }">{{ row.description || '—' }}</template>
              </el-table-column>
              <el-table-column :label="t('runDetail.status')" width="100">
                <template #default="{ row }"><StatusBadge :value="row.status" /></template>
              </el-table-column>
              <el-table-column :label="t('runDetail.dependsOn')" width="140">
                <template #default="{ row }">{{ depName(row.depends_on) }}</template>
              </el-table-column>
              <el-table-column v-if="canEdit" :label="t('runDetail.actions')" width="280" fixed="right">
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
                  <el-button size="small" type="danger" plain @click="removeStep(row)">{{ t('runDetail.delete') }}</el-button>
                </template>
              </el-table-column>
              <template #empty>
                <el-empty :description="t('runDetail.noSteps')" :image-size="60" />
              </template>
            </el-table>
          </section>
        </el-tab-pane>
        <el-tab-pane :label="t('runDetail.linkedReports')" name="reports">
          <section class="panel">
            <h3 class="panel-title">{{ t('runDetail.linkedReports') }}</h3>
            <!-- 后端暂无查询既有关联的端点，下列列表仅反映本次会话中的关联/解绑操作结果 -->
            <div v-if="canEdit" class="link-row">
              <el-select v-model="selectedReportId" class="report-select" filterable :placeholder="t('runDetail.selectReport')" :loading="reportsLoading">
                <el-option v-for="r in reportOptions" :key="r.id" :label="reportLabel(r)" :value="r.id" />
              </el-select>
              <el-button type="primary" :disabled="!selectedReportId" :loading="linking" @click="link">{{ t('runDetail.link') }}</el-button>
            </div>
            <el-empty v-if="!linkedReportIds.length" :description="t('runDetail.noLinkedReports')" :image-size="60" />
            <ul v-else class="link-list">
              <li v-for="id in linkedReportIds" :key="id">
                <span class="report-id">{{ id }}</span>
                <el-button v-if="canEdit" size="small" type="danger" plain @click="unlink(id)">{{ t('runDetail.unlink') }}</el-button>
              </li>
            </ul>
          </section>
        </el-tab-pane>
        <el-tab-pane :label="t('runDetail.testData')" name="testdata">
          <section class="panel">
            <h3 class="panel-title">{{ t('runDetail.linkedTestData') }}</h3>
            <el-table v-loading="testDataLoading" :data="testData" size="small">
              <el-table-column prop="measurement" :label="t('runDetail.measurement')" />
              <el-table-column prop="value" :label="t('runDetail.value')" width="120" />
              <el-table-column prop="unit" :label="t('runDetail.unit')" width="100" />
              <el-table-column prop="quality" :label="t('runDetail.quality')" width="100" />
              <el-table-column :label="t('runDetail.measuredAt')" width="180">
                <template #default="{ row }">{{ row.measured_at ? fmtTime(row.measured_at) : '—' }}</template>
              </el-table-column>
              <template #empty>
                <el-empty :description="t('runDetail.noTestData')" :image-size="60" />
              </template>
            </el-table>
          </section>
        </el-tab-pane>
      </el-tabs>
    </template>
    <el-dialog v-model="editDialog" :title="t('runDetail.editMetadata')" width="620">
      <el-form label-position="top">
        <el-form-item :label="t('runDetail.nameRequired')"><el-input v-model="editDraft.name" /></el-form-item>
        <el-form-item label="Campaign"><el-input v-model="editDraft.campaign" /></el-form-item>
        <div class="form-row">
          <el-form-item :label="t('runDetail.type')">
            <el-select v-model="editDraft.run_type">
              <el-option v-for="t in runTypes" :key="t.value" :label="t.label" :value="t.value" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('runDetail.gas')">
            <el-select v-model="editDraft.gas_type">
              <el-option v-for="g in gasTypes" :key="g" :label="g" :value="g" />
            </el-select>
          </el-form-item>
        </div>
        <div class="form-row">
          <el-form-item :label="t('runDetail.targetTemp')"><el-input-number v-model="editDraft.target_temp" :controls="false" :placeholder="t('runDetail.optional')" /></el-form-item>
          <el-form-item :label="t('runDetail.minTemp')"><el-input-number v-model="editDraft.min_temp" :controls="false" :placeholder="t('runDetail.optional')" /></el-form-item>
        </div>
        <div class="form-row three">
          <el-form-item :label="t('runDetail.pressureMin')"><el-input-number v-model="editDraft.pressure_min" :controls="false" :placeholder="t('runDetail.optional')" /></el-form-item>
          <el-form-item :label="t('runDetail.pressureMax')"><el-input-number v-model="editDraft.pressure_max" :controls="false" :placeholder="t('runDetail.optional')" /></el-form-item>
          <el-form-item :label="t('runDetail.pressureUnit')"><el-input v-model="editDraft.pressure_unit" /></el-form-item>
        </div>
        <el-form-item :label="t('runDetail.hasBeam')"><el-switch v-model="editDraft.has_beam" /></el-form-item>
        <el-form-item :label="t('runDetail.devices')">
          <el-select v-model="editDraft.devices" multiple :placeholder="t('runDetail.selectDevices')">
            <el-option v-for="d in deviceOptions" :key="d" :label="d" :value="d" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('runDetail.description')"><el-input v-model="editDraft.description" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialog = false">{{ t('runDetail.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveEdit">{{ t('runDetail.save') }}</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="createStepDialog" :title="t('runDetail.createStep')" width="520">
      <el-form label-position="top">
        <el-form-item :label="t('runDetail.name')" required><el-input v-model="stepDraft.name" maxlength="256" /></el-form-item>
        <el-form-item :label="t('runDetail.description')"><el-input v-model="stepDraft.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item :label="t('runDetail.dependsOn')">
          <el-select v-model="stepDraft.depends_on" clearable :placeholder="t('runDetail.noDependency')">
            <el-option v-for="s in steps" :key="s.id" :label="`${s.step_order}. ${s.name}`" :value="s.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createStepDialog = false">{{ t('runDetail.cancel') }}</el-button>
        <el-button type="primary" :loading="stepSaving" @click="createStep">{{ t('runDetail.save') }}</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="aiDialog" :title="t('runDetail.aiGenerateSteps')" width="760" @closed="resetAi">
      <div v-if="aiStage === 'input'" class="grid">
        <el-alert v-if="aiNotice" :type="aiNoticeType" :title="aiNotice" show-icon :closable="false" />
        <el-input
          v-model="aiPrompt"
          type="textarea"
          :rows="4"
          maxlength="4000"
          :placeholder="t('runDetail.aiPlaceholder')"
        />
      </div>
      <div v-else class="grid">
        <el-form label-position="top">
          <el-form-item :label="t('runDetail.templateName')">
            <el-input v-model="aiName" maxlength="256" />
          </el-form-item>
        </el-form>
        <StepItemsEditor :key="aiKey" v-model="aiItems" />
      </div>
      <template #footer>
        <template v-if="aiStage === 'input'">
          <el-button @click="aiDialog = false">{{ t('runDetail.cancel') }}</el-button>
          <el-button type="primary" :loading="aiGenerating" :disabled="!aiPrompt.trim()" @click="generateAiSteps">{{ t('runDetail.generate') }}</el-button>
        </template>
        <template v-else>
          <el-button @click="aiStage = 'input'">{{ t('runDetail.backToEdit') }}</el-button>
          <el-button :loading="aiSubmitting" @click="applyInline">{{ t('runDetail.apply') }}</el-button>
          <el-button v-if="canSaveTemplate" :loading="aiSubmitting" @click="saveTemplateOnly">{{ t('runDetail.saveTemplate') }}</el-button>
          <el-button v-if="canSaveTemplate" type="primary" :loading="aiSubmitting" @click="saveAndApplyTemplate">{{ t('runDetail.saveAndApply') }}</el-button>
        </template>
      </template>
    </el-dialog>
    <el-dialog v-model="importDialog" :title="t('runDetail.importFromTemplate')" width="520">
      <div class="grid">
        <p class="import-tip">{{ t('runDetail.importTip') }}</p>
        <el-select v-model="importTemplateId" v-loading="templatesLoading" filterable :placeholder="t('runDetail.selectTemplate')" class="import-select">
          <el-option v-for="t in templateOptions" :key="t.id" :label="t.name" :value="t.id" />
        </el-select>
      </div>
      <template #footer>
        <el-button @click="importDialog = false">{{ t('runDetail.cancel') }}</el-button>
        <el-button type="primary" :loading="importing" :disabled="!importTemplateId" @click="confirmImport">{{ t('runDetail.import') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
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
const { t } = useI18n()
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

const statusLabels = computed(() => ({
  planned: t('runDetail.runStatus.planned'),
  active: t('runDetail.runStatus.active'),
  paused: t('runDetail.runStatus.paused'),
  completed: t('runDetail.runStatus.completed'),
  aborted: t('runDetail.runStatus.aborted')
}))
const runTypes = computed(() => [
  { value: 'cooldown', label: t('runDetail.runType.cooldown') },
  { value: 'warmup', label: t('runDetail.runType.warmup') },
  { value: 'steady_state', label: t('runDetail.runType.steady_state') },
  { value: 'test', label: t('runDetail.runType.test') }
])
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
const transitionMap = computed<Record<string, TransitionAction[]>>(() => ({
  planned: [
    { value: 'start', label: t('runDetail.action.start'), type: 'primary' },
    { value: 'abort', label: t('runDetail.action.abort'), type: 'danger', confirm: true }
  ],
  active: [
    { value: 'pause', label: t('runDetail.action.pause'), type: 'warning' },
    { value: 'complete', label: t('runDetail.action.complete'), type: 'success', confirm: true },
    { value: 'abort', label: t('runDetail.action.abort'), type: 'danger', confirm: true }
  ],
  paused: [
    { value: 'resume', label: t('runDetail.action.resume'), type: 'primary' },
    { value: 'abort', label: t('runDetail.action.abort'), type: 'danger', confirm: true }
  ]
}))

// viewer 只读，隐藏状态转移/编辑/删除/关联入口（后端仍强校验）
const canEdit = computed(() => !!auth.user && auth.user.role !== 'viewer')
const transitions = computed(() => (run.value ? transitionMap.value[run.value.status] || [] : []))
// 后端 steptemplates 创建模板仅允许 admin/maintainer
const canSaveTemplate = computed(() => ['admin', 'maintainer'].includes(auth.user?.role || ''))

// 与后端 StepAllowedTransitions 保持一致：planned→start/cancel；in_progress→pause/complete/skip/cancel；paused→resume/cancel；skipped→start
type StepAction = { value: string; label: string; confirm?: boolean; danger?: boolean }
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
    if (r.created_at) items.push({ time: r.created_at, label: t('runDetail.created'), type: 'info' })
    if (r.started_at) items.push({ time: r.started_at, label: t('runDetail.started'), type: 'primary' })
    if (r.ended_at) items.push({ time: r.ended_at, label: t('runDetail.ended'), type: 'success' })
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
    error.value = err instanceof Error ? err.message : t('runDetail.loadFailed')
    showApiError(err, t('runDetail.loadFailed'))
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
    showApiError(err, t('runDetail.reportsLoadFailed'))
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
    showApiError(err, t('runDetail.testDataLoadFailed'))
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
  return runTypes.value.find((item) => item.value === value)?.label || value
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

  .form-row, .form-row.three {
    grid-template-columns: 1fr;
  }
}
</style>
