<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('instrument.title') }}</h2>
      <el-button plain @click="whitelistOpen = true">{{ t('instrument.whitelist') }}</el-button>
      <el-button :icon="Refresh" circle :title="t('instrument.refresh')" @click="loadAll" />
    </div>

    <!-- 仪器卡片 -->
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
      <el-button size="small" @click="loadInstruments">{{ t('instrument.retry') }}</el-button>
    </el-alert>
    <div v-loading="loading" class="card-grid">
      <section v-for="ins in instruments" :key="ins.id" class="panel ins-card">
        <header class="ins-head">
          <span class="state-dot" :class="{ pulse: ins.state === 'running' }" :style="{ background: stateColor(ins.state) }" />
          <div class="ins-title">
            <h3>{{ ins.name }}</h3>
            <p class="muted">{{ ins.id }}</p>
          </div>
          <el-tag :type="stateTag(ins.state)" size="small" round effect="light">{{ stateLabel(ins.state) }}</el-tag>
        </header>
        <div class="ins-actions">
          <el-button size="small" :type="expandedId === ins.id ? 'primary' : 'default'" plain @click="toggleExpand(ins)">
            {{ expandedId === ins.id ? t('instrument.collapse') : t('instrument.details') }}
          </el-button>
          <el-button size="small" type="primary" plain @click="openAI(ins)">{{ t('instrument.aiChat') }}</el-button>
          <el-button
            :size="isMobile ? 'default' : 'small'"
            type="danger"
            class="estop-btn"
            @click="onEmergencyStop(ins)"
          >{{ t('instrument.emergencyStop') }}</el-button>
        </div>

        <div v-if="expandedId === ins.id" v-loading="detailLoading" class="ins-detail">
          <template v-if="detailStatus">
            <div class="detail-row">
              <span class="muted">{{ t('instrument.workerStatus') }}</span>
              <span>{{ stateLabel(detailStatus.state) }}</span>
            </div>
            <div class="detail-row">
              <span class="muted">{{ t('instrument.rateLimited') }}</span>
              <span>{{ detailStatus.rate_limited ? t('instrument.yes') : t('instrument.no') }}</span>
            </div>
          </template>

          <template v-if="canOperate">
            <el-divider class="detail-divider" />
            <h4 class="detail-subtitle">{{ t('instrument.executeCommand') }}</h4>
            <el-select v-model="cmdName" :placeholder="t('instrument.selectWhitelistCommand')" class="cmd-select" @change="onCommandPick">
              <el-option v-for="c in executableCommands" :key="c.name" :label="c.name" :value="c.name">
                <div class="cmd-option">
                  <span>{{ c.name }}</span>
                  <el-tag :type="riskTag(c.risk)" size="small" effect="plain">{{ c.risk }}</el-tag>
                </div>
              </el-option>
            </el-select>
            <template v-if="cmdDef">
              <p class="muted cmd-desc">{{ cmdDef.description }}</p>
              <el-form label-position="top" @submit.prevent>
                <el-form-item v-for="[pname, pdef] in paramEntries(cmdDef)" :key="pname" :label="paramLabel(pname, pdef)">
                  <el-select v-if="pdef.enum" v-model="cmdParams[pname]">
                    <el-option v-for="opt in pdef.enum" :key="String(opt)" :label="String(opt)" :value="opt" />
                  </el-select>
                  <el-input-number
                    v-else-if="pdef.type === 'float' || pdef.type === 'int'"
                    v-model="cmdParams[pname]"
                    :min="numOrUndef(pdef.min)"
                    :max="numOrUndef(pdef.max)"
                    :controls="false"
                    class="param-number"
                  />
                  <el-input v-else v-model="cmdParams[pname]" />
                </el-form-item>
              </el-form>
              <el-button type="primary" :loading="cmdRunning" :disabled="!cmdName" @click="runCommand(ins)">{{ t('instrument.execute') }}</el-button>
            </template>
          </template>
          <p v-else class="muted cmd-desc">{{ t('instrument.noPermission') }}</p>
        </div>
      </section>
      <el-empty v-if="!loading && !instruments.length && !error" :description="t('instrument.noInstruments')" class="grid-empty" />
    </div>

    <!-- 执行结果 / 扫频曲线（常驻） -->
    <section class="panel">
      <div class="panel-head">
        <h3 class="panel-title">{{ t('instrument.executionResult') }}</h3>
      </div>
      <div v-if="cmdResult" class="cmd-result">
        <div v-if="parsedResult?.type === 'sweep_xy' && parsedResult.points?.length" class="parsed-chart">
          <canvas ref="chartCanvas" :aria-label="t('instrument.chartLabel')"></canvas>
          <p v-if="parsedResult.x_label || parsedResult.y_label" class="muted chart-caption">
            {{ parsedResult.x_label || 'x' }} / {{ parsedResult.y_label || 'y' }}
          </p>
        </div>
        <p v-else-if="parsedResult?.type === 'single_value'" class="parsed-value">{{ parsedResult.value }}</p>
        <pre v-if="cmdResult.response" class="cmd-response">{{ cmdResult.response }}</pre>
        <div class="cmd-result-footer">
          <p class="muted">{{ t('instrument.cmdResultInfo', { cmd: cmdResult.command, duration: (cmdResult.duration / 1e6).toFixed(1) }) }}</p>
          <el-button v-if="!isViewer" size="small" plain @click="openSave({ instrumentId: cmdResultInsId, command: cmdResult.command, response: cmdResult.response, parsed: parsedResult })">
            {{ t('instrument.saveToTestData') }}
          </el-button>
        </div>
      </div>
      <el-empty v-else :description="t('instrument.noResult')" :image-size="60" />
    </section>

    <!-- AI 对话（常驻） -->
    <section ref="aiPanelRef" class="panel">
      <div class="panel-head">
        <h3 class="panel-title">{{ t('instrument.aiChat') }}</h3>
        <el-select v-model="aiInstrumentId" :placeholder="t('instrument.selectAiInstrument')" class="ai-ins-select" @change="resetAIChat">
          <el-option v-for="ins in instruments" :key="ins.id" :label="ins.name" :value="ins.id" />
        </el-select>
      </div>
      <div class="chat-shell">
        <div class="chat-list">
          <el-empty v-if="!aiMessages.length" :description="t('instrument.aiPlaceholder')" :image-size="72" />
          <div v-for="(message, index) in aiMessages" :key="index" class="chat-message" :class="message.role">
            <div class="chat-bubble">
              <p v-if="message.content">{{ message.content }}</p>
              <div v-if="message.candidate" class="candidate-card">
                <template v-if="message.candidate.status === 'ok'">
                  <div class="candidate-title">
                    <code>{{ message.candidate.command }}</code>
                    <el-tag :type="riskTag(message.candidate.risk || '')" size="small">{{ message.candidate.risk }}</el-tag>
                  </div>
                  <p v-if="message.candidate.explanation">{{ message.candidate.explanation }}</p>
                  <pre class="candidate-json">{{ JSON.stringify(message.candidate.params || {}, null, 2) }}</pre>
                  <pre v-if="message.candidate.scpi_preview" class="candidate-scpi">{{ message.candidate.scpi_preview }}</pre>
                  <el-alert
                    v-if="!message.candidate.validation?.ok"
                    :title="message.candidate.validation?.reasons?.join('\uff1b') || t('instrument.validationFailed')"
                    type="error"
                    :closable="false"
                  />
                  <div class="candidate-actions">
                    <el-button
                      size="small"
                      type="primary"
                      :loading="message.running"
                      :disabled="!canOperate || !message.candidate.validation?.ok || message.done"
                      @click="runAICandidate(message)"
                    >{{ t('instrument.execute') }}</el-button>
                    <el-button size="small" :disabled="message.done" @click="message.done = true">{{ t('instrument.discard') }}</el-button>
                  </div>
                </template>
                <el-alert
                  v-else
                  :title="message.candidate.question || message.candidate.reason || t('instrument.cannotGenerate')"
                  :type="message.candidate.status === 'rejected' ? 'error' : 'info'"
                  :closable="false"
                />
                <p v-if="message.requestId" class="request-id">request_id: {{ message.requestId }}</p>
              </div>
              <div v-if="message.exec && !isViewer" class="exec-actions">
                <el-button size="small" plain @click="openSave(message.exec!)">{{ t('instrument.saveToTestData') }}</el-button>
              </div>
            </div>
          </div>
          <p v-if="aiLoading" class="muted chat-loading">{{ t('instrument.aiTranslating') }}</p>
        </div>
        <el-alert v-if="aiError" :title="aiError" type="error" :closable="false" show-icon />
        <div class="chat-input">
          <el-input
            v-model="aiInput"
            type="textarea"
            :rows="3"
            maxlength="1000"
            show-word-limit
            :placeholder="t('instrument.aiInputPlaceholder')"
            @keydown.ctrl.enter.prevent="sendAI"
          />
          <el-button type="primary" :loading="aiLoading" :disabled="!aiInstrument || !aiInput.trim()" @click="sendAI">{{ t('instrument.send') }}</el-button>
        </div>
      </div>
    </section>

    <!-- 命令白名单（默认收起，点击顶栏按钮弹出） -->
    <el-dialog v-model="whitelistOpen" :title="t('instrument.whitelist')" :width="isMobile ? '100%' : '900px'">
      <p class="muted hint whitelist-hint">{{ t('instrument.whitelistHint') }}</p>
      <ResponsiveTable :rows="whitelist" :loading="whitelistLoading">
        <el-table-column prop="name" :label="t('instrument.command')" min-width="150" />
        <el-table-column prop="description" :label="t('instrument.description')" min-width="180" show-overflow-tooltip />
        <el-table-column :label="t('instrument.risk')" width="90">
          <template #default="{ row }">
            <el-tag :type="riskTag(row.risk)" size="small" effect="light">{{ row.risk }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('instrument.scpiTemplate')" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <code class="scpi-code">{{ row.scpi || row.build || '—' }}</code>
          </template>
        </el-table-column>
        <el-table-column :label="t('instrument.timeout')" width="90">
          <template #default="{ row }">{{ row.timeout_ms ? `${row.timeout_ms} ms` : '—' }}</template>
        </el-table-column>
        <template #empty>
          <el-empty :description="t('instrument.whitelistEmpty')" />
        </template>
        <template #card="{ row }">
          <div class="wl-card">
            <span class="card-title">{{ row.name }}</span>
            <div class="card-fields">
              <span>{{ row.description || '—' }}</span>
              <el-tag :type="riskTag(row.risk)" size="small" effect="light">{{ row.risk }}</el-tag>
            </div>
            <div v-if="canOperate && row.risk !== 'red'" class="card-actions">
              <el-button size="small" type="primary" plain @click="executeFromWhitelist(row)">{{ t('instrument.execute') }}</el-button>
            </div>
          </div>
        </template>
      </ResponsiveTable>
    </el-dialog>

    <!-- 保存到测试数据 -->
    <el-dialog v-model="saveOpen" :title="t('instrument.saveDialogTitle')" :width="isMobile ? '100%' : '480px'">
      <el-form label-position="top" @submit.prevent>
        <el-form-item :label="t('instrument.project')" required>
          <el-select v-model="saveForm.project_id" :placeholder="t('instrument.selectProject')" :loading="saveProjectsLoading" @change="onSaveProjectChange">
            <el-option v-for="p in saveProjects" :key="p.id" :label="`${p.name}（${p.code}）`" :value="p.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('instrument.run')">
          <el-select v-model="saveForm.run_id" :placeholder="t('instrument.selectRun')" clearable :disabled="!saveForm.project_id">
            <el-option v-for="r in saveRuns" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('instrument.dataType')" required>
          <el-select v-model="saveForm.data_type" :placeholder="t('instrument.selectDataType')">
            <el-option v-for="t in dataTypes" :key="t" :label="t" :value="t" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('instrument.measurement')" required>
          <el-input v-model="saveForm.measurement" :placeholder="t('instrument.measurementPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('instrument.valueLabel')" required>
          <el-input-number v-model="saveForm.value" :controls="false" class="save-number" :placeholder="t('instrument.valueLabel')" />
        </el-form-item>
        <el-form-item :label="t('instrument.unitLabel')">
          <el-input v-model="saveForm.unit" :placeholder="t('instrument.unitPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('instrument.measuredAt')">
          <el-date-picker v-model="saveForm.measured_at" type="datetime" :placeholder="t('instrument.selectTime')" />
        </el-form-item>
        <el-form-item :label="t('instrument.notesLabel')">
          <el-input v-model="saveForm.notes" :placeholder="t('instrument.notesPlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="saveOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saveLoading" @click="submitSave">{{ t('instrument.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { Refresh } from '@element-plus/icons-vue'
import {
  Chart,
  type ChartDataset
} from 'chart.js'
import {
  emergencyStop,
  executeCommand,
  executeCommandWithMeta,
  getStatus,
  getWhitelist,
  interpretCommand,
  listInstruments,
  parseResult,
  type CommandParamDef,
  type CommandResult,
  type InstrumentStatus,
  type InstrumentSummary,
  type NLCommandCandidate,
  type ParsedResult,
  type WhitelistCommand
} from '../api/instruments'
import { createTestData, type TestDataPayload } from '../api/testdata'
import { listProjects, type Project } from '../api/projects'
import { listRuns, type ExperimentRun } from '../api/runs'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'
import { useMobile } from '../composables/useMobile'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import { chartPalette } from '../utils/chartTheme'

// Chart.register 已收口到 utils/chartTheme.ts setupChartDefaults()（美术方案 §3.7，main.ts 调用一次）

const { t } = useI18n()

const auth = useAuthStore()
// 与后端 RequireRole(maintainer, admin) 对应，前端隐藏只是 UX，后端仍强校验
const canOperate = computed(() => ['maintainer', 'admin'].includes(auth.user?.role || ''))
const isViewer = computed(() => auth.user?.role === 'viewer')
const isMobile = useMobile()

// 一次命令执行的结构化结果，cmdResult 区块和 AI 对话执行结果共用同一个保存对话框
type ExecRecord = {
  instrumentId: string
  command: string
  response?: string
  parsed?: ParsedResult | null
}

type ChatMessage = {
  role: 'user' | 'assistant'
  content: string
  candidate?: NLCommandCandidate
  requestId?: string
  running?: boolean
  done?: boolean
  exec?: ExecRecord
}

const aiInstrumentId = ref('')
const aiInstrument = computed(() => instruments.value.find((i) => i.id === aiInstrumentId.value) ?? null)
const aiInput = ref('')
const aiLoading = ref(false)
const aiError = ref('')
const aiMessages = ref<ChatMessage[]>([])
const aiPanelRef = ref<HTMLElement>()

const instruments = ref<InstrumentSummary[]>([])
const whitelist = ref<WhitelistCommand[]>([])
const loading = ref(false)
const whitelistLoading = ref(false)
const whitelistOpen = ref(false)
const error = ref('')

const expandedId = ref('')
const detailStatus = ref<InstrumentStatus | null>(null)
const detailLoading = ref(false)

const cmdName = ref('')
const cmdParams = reactive<Record<string, any>>({})
const cmdRunning = ref(false)
const cmdResult = ref<CommandResult | null>(null)
// 常驻结果面板需要知道结果来自哪台仪器（保存到测试数据时作为 instrumentId）
const cmdResultInsId = ref('')

// 执行结果解析与可视化
const parsedResult = ref<ParsedResult | null>(null)
const chartCanvas = ref<HTMLCanvasElement>()
let chart: Chart | undefined

// 保存到测试数据对话框
const dataTypes = ['cryo', 'pressure', 'voltage', 'rf_voltage', 'efficiency']
const saveOpen = ref(false)
const saveLoading = ref(false)
const saveProjects = ref<Project[]>([])
const saveProjectsLoading = ref(false)
const saveRuns = ref<ExperimentRun[]>([])
const saveForm = reactive({
  project_id: '',
  run_id: '',
  data_type: '',
  measurement: '',
  value: undefined as number | undefined,
  unit: '',
  measured_at: null as Date | null,
  notes: ''
})

// red 命令后端拒绝（command_not_allowed），同名命令多台仪器复用，按名称去重
const executableCommands = computed(() => {
  const seen = new Set<string>()
  const out: WhitelistCommand[] = []
  for (const c of whitelist.value) {
    if (c.risk === 'red' || seen.has(c.name)) continue
    seen.add(c.name)
    out.push(c)
  }
  return out
})

const cmdDef = computed(() => executableCommands.value.find((c) => c.name === cmdName.value))

onMounted(() => {
  loadAll()
})

function loadAll() {
  loadInstruments()
  loadWhitelist()
}

async function loadInstruments() {
  loading.value = true
  error.value = ''
  try {
    instruments.value = await listInstruments()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('instrument.loadFailed')
    showApiError(err, t('instrument.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function loadWhitelist() {
  whitelistLoading.value = true
  try {
    whitelist.value = await getWhitelist()
  } catch (err) {
    showApiError(err, t('instrument.whitelistLoadFailed'))
  } finally {
    whitelistLoading.value = false
  }
}

async function toggleExpand(ins: InstrumentSummary) {
  if (expandedId.value === ins.id) {
    expandedId.value = ''
    return
  }
  expandedId.value = ins.id
  cmdName.value = ''
  resetParams()
  detailLoading.value = true
  detailStatus.value = null
  try {
    detailStatus.value = await getStatus(ins.id)
  } catch (err) {
    showApiError(err, t('instrument.statusLoadFailed'))
  } finally {
    detailLoading.value = false
  }
}

function resetParams() {
  for (const key of Object.keys(cmdParams)) delete cmdParams[key]
}

function onCommandPick() {
  resetParams()
  if (!cmdDef.value) return
  for (const [name, def] of paramEntries(cmdDef.value)) {
    if (def.default === undefined || def.default === null) continue
    cmdParams[name] = (def.enum || def.type === 'string') ? String(def.default) : Number(def.default)
  }
}

// 白名单卡片「执行」：把命令带入仪器卡执行表单（展开首台仪器并预填参数），
// 真正的执行仍走 runCommand 的既有二次确认流程，一步不少。
async function executeFromWhitelist(cmd: WhitelistCommand) {
  const target = instruments.value.find((i) => i.id === expandedId.value) ?? instruments.value[0]
  if (!target) return
  whitelistOpen.value = false
  if (expandedId.value !== target.id) {
    await toggleExpand(target)
  }
  cmdName.value = cmd.name
  onCommandPick()
  await nextTick()
  document.querySelector('.ins-card')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function paramEntries(def: WhitelistCommand): [string, CommandParamDef][] {
  return Object.entries(def.params || {})
}

function paramLabel(name: string, def: CommandParamDef) {
  return def.unit ? `${name} (${def.unit})` : name
}

function numOrUndef(v: unknown) {
  const n = Number(v)
  return Number.isFinite(n) ? n : undefined
}

function resetAIChat() {
  aiMessages.value = []
  aiInput.value = ''
  aiError.value = ''
}

// 卡片上的「AI 对话」按钮：在常驻面板中选中该仪器并滚动到面板
function openAI(ins: InstrumentSummary) {
  aiInstrumentId.value = ins.id
  resetAIChat()
  aiPanelRef.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

async function sendAI() {
  const ins = aiInstrument.value
  const input = aiInput.value.trim()
  if (!ins || !input || aiLoading.value) return
  const history = aiMessages.value
    .filter((message) => message.content)
    .map((message) => ({ role: message.role, content: message.content }))
  aiMessages.value.push({ role: 'user', content: input })
  aiInput.value = ''
  aiError.value = ''
  aiLoading.value = true
  try {
    const response = await interpretCommand(ins.id, input, history)
    aiMessages.value.push({
      role: 'assistant',
      content: response.data.explanation || response.data.question || response.data.reason || '',
      candidate: response.data,
      requestId: response.requestId
    })
  } catch (err) {
    aiError.value = err instanceof Error ? err.message : t('instrument.aiTranslateFailed')
  } finally {
    aiLoading.value = false
  }
}

async function runAICandidate(message: ChatMessage) {
  const ins = aiInstrument.value
  const candidate = message.candidate
  if (!ins || !candidate?.command || !candidate.validation?.ok || message.done) return
  if (candidate.risk === 'yellow') {
    try {
      await ElMessageBox.confirm(t('instrument.confirmExecute', { command: candidate.command }), t('instrument.manualConfirm'), {
        confirmButtonText: t('instrument.execute'), cancelButtonText: t('common.cancel'), type: 'warning'
      })
    } catch {
      return
    }
  }
  message.running = true
  try {
    const response = await executeCommandWithMeta(ins.id, candidate.command, candidate.params || {})
    message.done = true
    const parsed = await parseExecution(ins.id, candidate.command, response.data.response)
    aiMessages.value.push({
      role: 'assistant',
      content: `${response.data.response || t('instrument.commandExecuted')}\nrequest_id: ${response.requestId}`,
      exec: { instrumentId: ins.id, command: candidate.command, response: response.data.response, parsed }
    })
  } catch (err) {
    aiError.value = err instanceof Error ? err.message : t('instrument.commandExecFailed')
  } finally {
    message.running = false
  }
}

async function runCommand(ins: InstrumentSummary) {
  const def = cmdDef.value
  if (!def) return
  if (def.risk === 'yellow') {
    try {
      await ElMessageBox.confirm(t('instrument.confirmWrite', { name: def.name }), t('instrument.writeConfirm'), {
        confirmButtonText: t('instrument.execute'),
        cancelButtonText: t('common.cancel'),
        type: 'warning'
      })
    } catch {
      return
    }
  }
  cmdRunning.value = true
  clearCmdResult()
  try {
    cmdResult.value = await executeCommand(ins.id, def.name, { ...cmdParams })
    cmdResultInsId.value = ins.id
    ElMessage.success(t('instrument.executeSuccess', { name: def.name }))
    parsedResult.value = await parseExecution(ins.id, cmdResult.value.command, cmdResult.value.response)
    if (parsedResult.value?.type === 'sweep_xy') {
      await nextTick()
      renderChart(cmdResult.value.command, parsedResult.value)
    }
  } catch (err) {
    showApiError(err, t('instrument.commandExecFailed'))
  } finally {
    cmdRunning.value = false
  }
}

function clearCmdResult() {
  cmdResult.value = null
  parsedResult.value = null
  destroyChart()
}

// 命令未配置 result_parser 时后端返回 null，属正常情况；配置了 parser 但解析失败时必须让用户知道
async function parseExecution(instrumentId: string, command: string, response?: string): Promise<ParsedResult | null> {
  if (!response) return null
  try {
    return await parseResult(instrumentId, command, response)
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err)
    console.warn(`[instruments] parse-result failed: command=${command} reason=${reason}`)
    const hasParser = Boolean(whitelist.value.find((c) => c.name === command)?.result_parser)
    if (hasParser) {
      ElMessage.warning(t('instrument.parseFailed', { reason }))
    }
    return null
  }
}

function destroyChart() {
  chart?.destroy()
  chart = undefined
}

function renderChart(command: string, parsed: ParsedResult) {
  destroyChart()
  if (!chartCanvas.value || !parsed.points?.length) return
  // 系列语义色（美术方案 §3.7）：测量曲线 --chart-1、极值点 --chart-3
  const p = chartPalette()
  const datasets: ChartDataset<'scatter'>[] = [
    {
      label: parsed.y_label || 'value',
      data: parsed.points,
      showLine: true,
      borderColor: p[0],
      backgroundColor: p[0],
      pointRadius: 2
    }
  ]
  // 命令名含 min/max 时标注扫频数据中的对应极值点
  const lower = command.toLowerCase()
  const extreme = lower.includes('min')
    ? { label: 'min', point: parsed.points.reduce((a, b) => (b.y < a.y ? b : a)) }
    : lower.includes('max')
      ? { label: 'max', point: parsed.points.reduce((a, b) => (b.y > a.y ? b : a)) }
      : null
  if (extreme) {
    datasets.push({
      label: `${extreme.label} (${extreme.point.x}, ${extreme.point.y})`,
      data: [extreme.point],
      borderColor: p[2],
      backgroundColor: p[2],
      pointRadius: 6,
      pointStyle: 'triangle'
    })
  }
  chart = new Chart(chartCanvas.value, {
    type: 'scatter',
    data: { datasets },
    options: {
      animation: false,
      responsive: true,
      maintainAspectRatio: false,
      plugins: { legend: { display: true } },
      scales: {
        x: { type: 'linear', title: { display: !!parsed.x_label, text: parsed.x_label || '' } },
        y: { type: 'linear', title: { display: !!parsed.y_label, text: parsed.y_label || '' } }
      }
    }
  })
}

onBeforeUnmount(destroyChart)

function openSave(exec: ExecRecord) {
  saveForm.project_id = ''
  saveForm.run_id = ''
  saveRuns.value = []
  saveForm.data_type = ''
  saveForm.measurement = exec.command
  saveForm.value = exec.parsed?.type === 'single_value' ? exec.parsed.value : undefined
  saveForm.unit = ''
  saveForm.measured_at = new Date()
  saveForm.notes = `instrument=${exec.instrumentId} command=${exec.command}`
  saveOpen.value = true
  loadSaveProjects()
}

async function loadSaveProjects() {
  if (saveProjects.value.length || saveProjectsLoading.value) return
  saveProjectsLoading.value = true
  try {
    saveProjects.value = await listProjects()
  } catch (err) {
    showApiError(err, t('instrument.projectsLoadFailed'))
  } finally {
    saveProjectsLoading.value = false
  }
}

async function onSaveProjectChange(projectId: string) {
  saveForm.run_id = ''
  saveRuns.value = []
  if (!projectId) return
  try {
    const data = await listRuns(projectId, { per_page: 100 })
    saveRuns.value = data.items ?? []
  } catch (err) {
    showApiError(err, t('instrument.runsLoadFailed'))
  }
}

async function submitSave() {
  if (!saveForm.project_id) {
    ElMessage.warning(t('instrument.pleaseSelectProject'))
    return
  }
  if (!saveForm.data_type) {
    ElMessage.warning(t('instrument.pleaseSelectDataType'))
    return
  }
  if (!saveForm.measurement.trim()) {
    ElMessage.warning(t('instrument.pleaseEnterMeasurement'))
    return
  }
  if (saveForm.value === undefined || Number.isNaN(saveForm.value)) {
    ElMessage.warning(t('instrument.pleaseEnterValue'))
    return
  }
  // 后端开启 DisallowUnknownFields，只提交白名单内字段
  const payload: TestDataPayload = {
    data_type: saveForm.data_type,
    measurement: saveForm.measurement.trim(),
    value: saveForm.value
  }
  const unit = saveForm.unit.trim()
  if (unit) payload.unit = unit
  if (saveForm.measured_at) payload.measured_at = new Date(saveForm.measured_at).toISOString()
  if (saveForm.run_id) payload.run_id = saveForm.run_id
  const notes = saveForm.notes.trim()
  if (notes) payload.notes = notes
  saveLoading.value = true
  try {
    await createTestData(saveForm.project_id, payload)
    ElMessage.success(t('instrument.testDataSaved'))
    saveOpen.value = false
  } catch (err) {
    showApiError(err, t('instrument.saveFailed'))
  } finally {
    saveLoading.value = false
  }
}

async function onEmergencyStop(ins: InstrumentSummary) {
  try {
    await ElMessageBox.confirm(
      t('instrument.emergencyStopConfirm', { name: ins.name }),
      t('instrument.emergencyStopTitle'),
      {
        confirmButtonText: t('instrument.emergencyStopTitle'),
        cancelButtonText: t('common.cancel'),
        type: 'error',
        confirmButtonClass: 'el-button--danger'
      }
    )
  } catch {
    return
  }
  try {
    await emergencyStop(ins.id)
    ElMessage.success(t('instrument.emergencyStopSent'))
    await loadInstruments()
  } catch (err) {
    showApiError(err, t('instrument.emergencyStopFailed'))
  }
}

const STATE_META: Record<string, { color: string; tag: 'success' | 'warning' | 'danger' | 'info' }> = {
  running: { color: 'var(--ok)', tag: 'success' },
  rate_limited: { color: 'var(--warn)', tag: 'warning' },
  needs_reconnect: { color: 'var(--warn)', tag: 'warning' },
  error: { color: 'var(--danger)', tag: 'danger' }
}

function stateColor(s: string) {
  return STATE_META[s]?.color || 'var(--text-3)'
}

function stateLabel(s: string) {
  const map: Record<string, string> = {
    running: 'instrument.stateRunning',
    rate_limited: 'instrument.stateRateLimited',
    needs_reconnect: 'instrument.stateNeedsReconnect',
    error: 'instrument.stateError'
  }
  return t(map[s] || '') || s
}

function stateTag(s: string) {
  return STATE_META[s]?.tag || 'info'
}

function riskTag(risk: string): 'success' | 'warning' | 'danger' | 'info' {
  if (risk === 'green') return 'success'
  if (risk === 'yellow') return 'warning'
  if (risk === 'red') return 'danger'
  return 'info'
}

</script>

<style scoped>
.panel-head {
  align-items: baseline;
  justify-content: space-between;
}

.hint {
  font-size: 12px;
}

.card-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
}

.grid-empty {
  grid-column: 1 / -1;
}

.ins-card {
  display: grid;
  gap: 14px;
}

.ins-head {
  align-items: center;
  display: flex;
  gap: 10px;
}

.ins-head h3 {
  font-size: 15px;
}

.ins-title {
  min-width: 0;
}

.ins-title p {
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ins-head .el-tag {
  margin-left: auto;
}

.state-dot {
  border-radius: 50%;
  flex-shrink: 0;
  height: 10px;
  width: 10px;
}

.state-dot.pulse {
  animation: dot-pulse 2s ease-in-out infinite;
}

@keyframes dot-pulse {
  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(77, 158, 107, 0.35);
  }
  50% {
    box-shadow: 0 0 0 5px rgba(77, 158, 107, 0);
  }
}

.ins-actions {
  display: flex;
  gap: 10px;
}

.ins-actions .el-button {
  margin-left: 0;
}

.estop-btn {
  font-weight: 650;
  margin-left: auto;
}

.ins-detail {
  border-top: 1px solid var(--border);
  display: grid;
  gap: 10px;
  padding-top: 14px;
}

.detail-row {
  display: flex;
  font-size: 13px;
  justify-content: space-between;
}

.detail-divider {
  margin: 4px 0;
}

.detail-subtitle {
  font-size: 13px;
}

.cmd-select {
  width: 100%;
}

.cmd-option {
  align-items: center;
  display: flex;
  justify-content: space-between;
}

.cmd-desc {
  font-size: 12px;
}

.param-number {
  width: 100%;
}

.cmd-result {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-size: 12px;
  padding: 10px 12px;
}

.cmd-response {
  margin: 0 0 6px;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.parsed-chart {
  height: 320px;
  margin-bottom: 8px;
}

.parsed-chart canvas {
  max-height: 290px;
}

.chart-caption {
  font-size: 11px;
  margin: 4px 0 0;
  text-align: center;
}

.parsed-value {
  font-size: 28px;
  font-weight: 650;
  margin: 0 0 8px;
}

.cmd-result-footer {
  align-items: center;
  display: flex;
  gap: 10px;
  justify-content: space-between;
}

.cmd-result-footer p {
  margin: 0;
}

.exec-actions {
  margin-top: 8px;
}

.save-number {
  width: 100%;
}

.scpi-code {
  font-size: 12px;
  white-space: pre-wrap;
}

.chat-shell {
  display: grid;
  gap: var(--space-3);
}

.chat-list {
  max-height: 360px;
  overflow-y: auto;
}

.chat-message {
  display: flex;
  margin-bottom: 12px;
}

.chat-message.user {
  justify-content: flex-end;
}

.chat-bubble {
  background: var(--surface-2);
  border-radius: var(--radius-sm);
  max-width: 92%;
  padding: 10px 12px;
  white-space: pre-wrap;
}

.chat-message.user .chat-bubble {
  background: var(--brand-100);
}

.candidate-card,
.candidate-actions,
.candidate-title,
.chat-input {
  display: flex;
  gap: 8px;
}

.candidate-card {
  flex-direction: column;
}

.candidate-title,
.chat-input {
  align-items: center;
  justify-content: space-between;
}

.candidate-json,
.candidate-scpi {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  margin: 0;
  overflow-x: auto;
  padding: 8px;
  white-space: pre-wrap;
}

.request-id,
.chat-loading {
  color: var(--text-3);
  font-size: 11px;
}

.chat-input .el-textarea {
  flex: 1;
}

.ai-ins-select {
  max-width: 240px;
}

.whitelist-hint {
  display: block;
  margin-bottom: 10px;
}

@media (max-width: 768px) {
  .ins-actions {
    flex-wrap: wrap;
  }

  .estop-btn {
    flex-basis: 100%;
    margin-left: 0;
  }

  .parsed-chart {
    height: 260px;
  }
}
</style>
