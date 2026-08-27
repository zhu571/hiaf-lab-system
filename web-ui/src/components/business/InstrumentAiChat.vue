<template>
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
                  :title="message.candidate.validation?.reasons?.join(t('common.listSeparator')) || t('instrument.validationFailed')"
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
              <el-button size="small" plain @click="emit('save', message.exec!)">{{ t('instrument.saveToTestData') }}</el-button>
            </div>
            <div v-if="message.flow" class="flow-card">
              <p>{{ t('instrument.flowStatus') }}: {{ message.flow.status }}</p>
              <p>{{ message.flow.point_count }}/{{ message.flow.limits.max_points }} · {{ message.flow.step_count }}/{{ message.flow.limits.max_commands }}</p>
              <p class="request-id">flow_id: {{ message.flow.id }}</p>
              <div class="candidate-actions">
                <!-- API-only 试点（M1）：审批走后端 API/命令行，创建者面板不放自审批按钮（后端强校验审批人分离，自审批必 403）。 -->
                <el-button v-if="['queued', 'running'].includes(message.flow.status)" size="small" @click="stopInstrumentFlow(message)">{{ t('instrument.stopFlow') }}</el-button>
              </div>
              <ol v-if="message.flow.steps?.length" class="flow-steps">
                <li v-for="step in message.flow.steps" :key="step.step_no">#{{ step.step_no }} {{ step.command }} — {{ step.status }}<span v-if="step.error_code"> ({{ step.error_code }})</span></li>
              </ol>
              <svg v-if="message.flow.result?.points?.length" viewBox="0 0 400 160" role="img" :aria-label="t('instrument.chartLabel')" class="flow-chart">
                <polyline :points="flowPolyline(message.flow.result.points)" />
              </svg>
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
</template>

<script lang="ts">
/* 纯函数/类型导出（SensorTrendChart.vue 独立 script 块先例）：供 InstrumentMeasureView 复用 */

import type { ParsedResult } from '@/api/instruments'

export type ExecRecord = {
  instrumentId: string
  command: string
  response?: string
  parsed?: ParsedResult | null
}

export function riskTag(risk: string): 'success' | 'warning' | 'danger' | 'info' {
  if (risk === 'green') return 'success'
  if (risk === 'yellow') return 'warning'
  if (risk === 'red') return 'danger'
  return 'info'
}
</script>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessageBox } from 'element-plus'
import {
  executeCommandWithMeta,
  createFlow,
  createLease,
  getFlow,
  interpretCommand,
  stopFlow,
  type FlowSession,
  type InstrumentSummary,
  type NLCommandCandidate
} from '@/api/instruments'

// 自然语言对话面板（重构方案 S5：从 InstrumentMeasureView 拆出）。
// 职责边界：对话状态/发送/候选执行自持；「保存到测试数据」经 save 事件交给父视图（saveForm 属视图层）。
// 卡片「AI 对话」入口经 defineExpose 的 selectInstrument 选中并重置。

const props = defineProps<{
  instruments: InstrumentSummary[]
  canOperate: boolean
  isViewer: boolean
  parseExecution: (instrumentId: string, command: string, response?: string) => Promise<ParsedResult | null>
}>()

const emit = defineEmits<{ save: [exec: ExecRecord] }>()

const { t } = useI18n()

type ChatMessage = {
  role: 'user' | 'assistant'
  content: string
  candidate?: NLCommandCandidate
  requestId?: string
  running?: boolean
  done?: boolean
  exec?: ExecRecord
  flow?: FlowSession
}

const aiInstrumentId = ref('')
const aiInstrument = computed(() => props.instruments.find((i) => i.id === aiInstrumentId.value) ?? null)
const aiInput = ref('')
const aiLoading = ref(false)
const aiError = ref('')
const aiMessages = ref<ChatMessage[]>([])
const aiPanelRef = ref<HTMLElement>()
const pollers = new Map<string, number>()

function resetAIChat() {
  aiMessages.value = []
  aiInput.value = ''
  aiError.value = ''
}

// 卡片上的「AI 对话」按钮：选中该仪器、重置对话并滚动到本面板（卡片入口专用，区别于下拉切换）
function openAiFor(ins: InstrumentSummary) {
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
    if (ins.id === 'hioki_im3536' && /扫频|sweep/i.test(input)) {
      const lease = await createLease(ins.id, input)
      const response = await createFlow(ins.id, input, lease.data.id)
      const message: ChatMessage = { role: 'assistant', content: t('instrument.flowCreated'), flow: response.data, requestId: response.requestId }
      aiMessages.value.push(message)
      return
    }
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

function startFlowPolling(message: ChatMessage) {
  const flow = message.flow
  if (!flow || pollers.has(flow.id)) return
  const timer = window.setInterval(async () => {
    try {
      const latest = await getFlow(flow.instrument_id, flow.id)
      message.flow = latest
      if (!['queued', 'running'].includes(latest.status)) {
        window.clearInterval(timer)
        pollers.delete(flow.id)
      }
    } catch { /* keep the last trusted GET snapshot */ }
  }, 1000)
  pollers.set(flow.id, timer)
}

async function stopInstrumentFlow(message: ChatMessage) {
  if (!message.flow) return
  await stopFlow(message.flow.instrument_id, message.flow.id)
  startFlowPolling(message)
}

function flowPolyline(points: { x: number; y: number }[]) {
  if (points.length < 2) return ''
  const xs = points.map((p) => Math.log10(p.x)), ys = points.map((p) => p.y)
  const minX = Math.min(...xs), maxX = Math.max(...xs), minY = Math.min(...ys), maxY = Math.max(...ys)
  return points.map((p, i) => `${20 + (xs[i] - minX) / (maxX - minX || 1) * 360},${140 - (p.y - minY) / (maxY - minY || 1) * 120}`).join(' ')
}

onBeforeUnmount(() => pollers.forEach((timer) => window.clearInterval(timer)))

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
    const parsed = await props.parseExecution(ins.id, candidate.command, response.data.response)
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

defineExpose({ openAiFor })
</script>

<style scoped>
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

.flow-card { border: 1px solid var(--border); border-radius: var(--radius-sm); padding: var(--space-3); }
.flow-steps { max-height: 160px; overflow-y: auto; padding-left: var(--space-5); }
.flow-chart { background: var(--surface); width: 100%; }
.flow-chart polyline { fill: none; stroke: var(--brand-500); stroke-width: 2; }

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
</style>
