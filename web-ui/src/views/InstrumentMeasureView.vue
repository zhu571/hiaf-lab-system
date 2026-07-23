<template>
  <div class="page">
    <div class="toolbar">
      <h2>测量仪器</h2>
      <el-button :icon="Refresh" circle title="刷新" @click="loadAll" />
    </div>

    <!-- 仪器卡片 -->
    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
      <el-button size="small" @click="loadInstruments">重试</el-button>
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
            {{ expandedId === ins.id ? '收起' : '详情' }}
          </el-button>
          <el-button size="small" type="primary" plain @click="openAI(ins)">AI 对话</el-button>
          <el-button size="small" type="danger" class="estop-btn" @click="onEmergencyStop(ins)">紧急停机</el-button>
        </div>

        <div v-if="expandedId === ins.id" v-loading="detailLoading" class="ins-detail">
          <template v-if="detailStatus">
            <div class="detail-row">
              <span class="muted">Worker 状态</span>
              <span>{{ stateLabel(detailStatus.state) }}</span>
            </div>
            <div class="detail-row">
              <span class="muted">限流</span>
              <span>{{ detailStatus.rate_limited ? '是' : '否' }}</span>
            </div>
          </template>

          <template v-if="canOperate">
            <el-divider class="detail-divider" />
            <h4 class="detail-subtitle">执行命令</h4>
            <el-select v-model="cmdName" placeholder="选择白名单命令" class="cmd-select" @change="onCommandPick">
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
              <el-button type="primary" :loading="cmdRunning" :disabled="!cmdName" @click="runCommand(ins)">执行</el-button>
            </template>
            <div v-if="cmdResult" class="cmd-result">
              <pre v-if="cmdResult.response" class="cmd-response">{{ cmdResult.response }}</pre>
              <p class="muted">命令 {{ cmdResult.command }} 完成，耗时 {{ (cmdResult.duration / 1e6).toFixed(1) }} ms</p>
            </div>
          </template>
          <p v-else class="muted cmd-desc">命令执行需要 maintainer 或 admin 权限</p>
        </div>
      </section>
      <el-empty v-if="!loading && !instruments.length && !error" description="暂无仪器" class="grid-empty" />
    </div>

    <!-- 命令白名单 -->
    <section class="panel">
      <div class="panel-head">
        <h3 class="panel-title">命令白名单</h3>
        <span class="muted hint">红色风险命令由后端拒绝执行</span>
      </div>
      <el-table v-loading="whitelistLoading" :data="whitelist">
        <el-table-column prop="name" label="命令" min-width="150" />
        <el-table-column prop="description" label="描述" min-width="180" show-overflow-tooltip />
        <el-table-column label="风险" width="90">
          <template #default="{ row }">
            <el-tag :type="riskTag(row.risk)" size="small" effect="light">{{ row.risk }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="SCPI 模板" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <code class="scpi-code">{{ row.scpi || row.build || '—' }}</code>
          </template>
        </el-table-column>
        <el-table-column label="超时" width="90">
          <template #default="{ row }">{{ row.timeout_ms ? `${row.timeout_ms} ms` : '—' }}</template>
        </el-table-column>
        <template #empty>
          <el-empty description="白名单为空" />
        </template>
      </el-table>
    </section>

    <el-drawer v-model="aiOpen" :title="`${aiInstrument?.name || ''} · AI 对话`" :size="isMobile ? '100%' : '440px'">
      <div class="chat-shell">
        <div class="chat-list">
          <el-empty v-if="!aiMessages.length" description="描述想执行的操作，例如“读取仪器标识”" :image-size="72" />
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
                    :title="message.candidate.validation?.reasons?.join('；') || '参数校验未通过'"
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
                    >执行</el-button>
                    <el-button size="small" :disabled="message.done" @click="message.done = true">放弃</el-button>
                  </div>
                </template>
                <el-alert
                  v-else
                  :title="message.candidate.question || message.candidate.reason || '无法生成候选命令'"
                  :type="message.candidate.status === 'rejected' ? 'error' : 'info'"
                  :closable="false"
                />
                <p v-if="message.requestId" class="request-id">request_id: {{ message.requestId }}</p>
              </div>
            </div>
          </div>
          <p v-if="aiLoading" class="muted chat-loading">正在翻译并校验…</p>
        </div>
        <el-alert v-if="aiError" :title="aiError" type="error" :closable="false" show-icon />
        <div class="chat-input">
          <el-input
            v-model="aiInput"
            type="textarea"
            :rows="3"
            maxlength="1000"
            show-word-limit
            placeholder="输入自然语言命令"
            @keydown.ctrl.enter.prevent="sendAI"
          />
          <el-button type="primary" :loading="aiLoading" :disabled="!aiInput.trim()" @click="sendAI">发送</el-button>
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import {
  emergencyStop,
  executeCommand,
  executeCommandWithMeta,
  getStatus,
  getWhitelist,
  interpretCommand,
  listInstruments,
  type CommandParamDef,
  type CommandResult,
  type InstrumentStatus,
  type InstrumentSummary,
  type NLCommandCandidate,
  type WhitelistCommand
} from '../api/instruments'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'
import { useMobile } from '../composables/useMobile'

const auth = useAuthStore()
// 与后端 RequireRole(maintainer, admin) 对应，前端隐藏只是 UX，后端仍强校验
const canOperate = computed(() => ['maintainer', 'admin'].includes(auth.user?.role || ''))
const isMobile = useMobile()

type ChatMessage = {
  role: 'user' | 'assistant'
  content: string
  candidate?: NLCommandCandidate
  requestId?: string
  running?: boolean
  done?: boolean
}

const aiOpen = ref(false)
const aiInstrument = ref<InstrumentSummary | null>(null)
const aiInput = ref('')
const aiLoading = ref(false)
const aiError = ref('')
const aiMessages = ref<ChatMessage[]>([])

const instruments = ref<InstrumentSummary[]>([])
const whitelist = ref<WhitelistCommand[]>([])
const loading = ref(false)
const whitelistLoading = ref(false)
const error = ref('')

const expandedId = ref('')
const detailStatus = ref<InstrumentStatus | null>(null)
const detailLoading = ref(false)

const cmdName = ref('')
const cmdParams = reactive<Record<string, any>>({})
const cmdRunning = ref(false)
const cmdResult = ref<CommandResult | null>(null)

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
    error.value = err instanceof Error ? err.message : '仪器列表加载失败'
    showApiError(err, '仪器列表加载失败')
  } finally {
    loading.value = false
  }
}

async function loadWhitelist() {
  whitelistLoading.value = true
  try {
    whitelist.value = await getWhitelist()
  } catch (err) {
    showApiError(err, '白名单加载失败')
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
  cmdResult.value = null
  resetParams()
  detailLoading.value = true
  detailStatus.value = null
  try {
    detailStatus.value = await getStatus(ins.id)
  } catch (err) {
    showApiError(err, '仪器状态获取失败')
  } finally {
    detailLoading.value = false
  }
}

function resetParams() {
  for (const key of Object.keys(cmdParams)) delete cmdParams[key]
}

function onCommandPick() {
  resetParams()
  cmdResult.value = null
  if (!cmdDef.value) return
  for (const [name, def] of paramEntries(cmdDef.value)) {
    if (def.default === undefined || def.default === null) continue
    cmdParams[name] = (def.enum || def.type === 'string') ? String(def.default) : Number(def.default)
  }
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

function openAI(ins: InstrumentSummary) {
  aiInstrument.value = ins
  aiMessages.value = []
  aiInput.value = ''
  aiError.value = ''
  aiOpen.value = true
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
    aiError.value = err instanceof Error ? err.message : 'AI 翻译失败'
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
      await ElMessageBox.confirm(`「${candidate.command}」将改变仪器状态，确认执行候选参数吗？`, '人工确认', {
        confirmButtonText: '执行', cancelButtonText: '取消', type: 'warning'
      })
    } catch {
      return
    }
  }
  message.running = true
  try {
    const response = await executeCommandWithMeta(ins.id, candidate.command, candidate.params || {})
    message.done = true
    aiMessages.value.push({
      role: 'assistant',
      content: `${response.data.response || '命令执行完成'}\nrequest_id: ${response.requestId}`
    })
  } catch (err) {
    aiError.value = err instanceof Error ? err.message : '命令执行失败'
  } finally {
    message.running = false
  }
}

async function runCommand(ins: InstrumentSummary) {
  const def = cmdDef.value
  if (!def) return
  if (def.risk === 'yellow') {
    try {
      await ElMessageBox.confirm(`「${def.name}」是写入类命令（yellow），将改变仪器状态，确定执行吗？`, '写入确认', {
        confirmButtonText: '执行',
        cancelButtonText: '取消',
        type: 'warning'
      })
    } catch {
      return
    }
  }
  cmdRunning.value = true
  cmdResult.value = null
  try {
    cmdResult.value = await executeCommand(ins.id, def.name, { ...cmdParams })
    ElMessage.success(`命令 ${def.name} 执行成功`)
  } catch (err) {
    showApiError(err, '命令执行失败')
  } finally {
    cmdRunning.value = false
  }
}

async function onEmergencyStop(ins: InstrumentSummary) {
  try {
    await ElMessageBox.confirm(
      `确定对「${ins.name}」执行紧急停机吗？仪器命令队列将立即停止，该操作会记录审计日志并触发告警。`,
      '紧急停机',
      {
        confirmButtonText: '紧急停机',
        cancelButtonText: '取消',
        type: 'error',
        confirmButtonClass: 'el-button--danger'
      }
    )
  } catch {
    return
  }
  try {
    await emergencyStop(ins.id)
    ElMessage.success('紧急停机指令已下发')
    await loadInstruments()
  } catch (err) {
    showApiError(err, '紧急停机失败')
  }
}

const STATE_META: Record<string, { label: string; color: string; tag: 'success' | 'warning' | 'danger' | 'info' }> = {
  running: { label: '运行中', color: 'var(--ok)', tag: 'success' },
  rate_limited: { label: '限流中', color: 'var(--warn)', tag: 'warning' },
  needs_reconnect: { label: '待重连', color: 'var(--warn)', tag: 'warning' },
  error: { label: '错误', color: 'var(--danger)', tag: 'danger' }
}

function stateColor(s: string) {
  return STATE_META[s]?.color || 'var(--text-3)'
}

function stateLabel(s: string) {
  return STATE_META[s]?.label || s
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
  display: flex;
  gap: 10px;
  justify-content: space-between;
  margin-bottom: 14px;
}

.panel-title {
  font-size: 15px;
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

.scpi-code {
  font-size: 12px;
  white-space: pre-wrap;
}

.chat-shell {
  display: grid;
  gap: 12px;
  height: 100%;
  grid-template-rows: minmax(0, 1fr) auto auto;
}

.chat-list {
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
</style>
