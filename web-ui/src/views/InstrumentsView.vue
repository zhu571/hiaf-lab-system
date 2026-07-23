<template>
  <div class="page">
    <div class="toolbar">
      <h2>仪器控制</h2>
      <el-button :icon="Refresh" circle title="刷新" @click="loadAll" />
    </div>

    <!-- Piezo 控制面板 -->
    <section class="panel">
      <div class="panel-head">
        <h3 class="panel-title">Piezo 压电控制</h3>
        <span class="muted hint">状态每 3 秒自动刷新</span>
      </div>
      <el-alert v-if="piezoError" :title="piezoError" type="error" show-icon :closable="false" class="piezo-alert">
        <el-button size="small" @click="refreshPiezo">重试</el-button>
      </el-alert>
      <template v-if="piezo">
        <div class="piezo-stats">
          <div class="piezo-stat">
            <span class="stat-label">A1 读数</span>
            <strong class="stat-value">{{ fmtValue(piezo.a1) }}</strong>
          </div>
          <div class="piezo-stat">
            <span class="stat-label">阀门设定</span>
            <strong class="stat-value">{{ fmtValue(piezo.valve_sp) }}</strong>
          </div>
          <div class="piezo-stat">
            <span class="stat-label">运行状态</span>
            <el-tag :type="piezo.running ? 'success' : 'info'" size="small" round effect="light">
              {{ piezo.running ? '运行中' : '已停止' }}
            </el-tag>
          </div>
          <div v-if="piezo.error" class="piezo-stat">
            <span class="stat-label">错误</span>
            <span class="stat-error">{{ piezo.error }}</span>
          </div>
        </div>
        <div v-if="canOperate" class="piezo-controls">
          <el-input-number v-model="setpoint" :controls="false" placeholder="设定值" class="setpoint-input" />
          <el-button type="primary" plain :loading="piezoBusy" @click="applySetpoint">设定</el-button>
          <el-button v-if="!piezo.running" type="success" :loading="piezoBusy" @click="onPiezoStart">启动</el-button>
          <el-button v-else type="warning" :loading="piezoBusy" @click="onPiezoStop">停止</el-button>
        </div>
      </template>
      <el-skeleton v-else-if="!piezoError" :rows="2" animated />
    </section>

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
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import {
  emergencyStop,
  executeCommand,
  getStatus,
  getWhitelist,
  listInstruments,
  piezoSetpoint,
  piezoStart,
  piezoStatus,
  piezoStop,
  type CommandParamDef,
  type CommandResult,
  type InstrumentStatus,
  type InstrumentSummary,
  type PiezoStatus,
  type WhitelistCommand
} from '../api/instruments'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

const auth = useAuthStore()
// 与后端 RequireRole(maintainer, admin) 对应，前端隐藏只是 UX，后端仍强校验
const canOperate = computed(() => ['maintainer', 'admin'].includes(auth.user?.role || ''))

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

const piezo = ref<PiezoStatus | null>(null)
const piezoError = ref('')
const piezoBusy = ref(false)
const setpoint = ref<number>()
let piezoTimer: number | undefined

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
  refreshPiezo()
  piezoTimer = window.setInterval(refreshPiezo, 3000)
})

onBeforeUnmount(() => {
  window.clearInterval(piezoTimer)
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
    cmdParams[name] = def.enum || def.type === 'string' ? String(def.default) : Number(def.default)
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

async function refreshPiezo() {
  try {
    piezo.value = await piezoStatus()
    piezoError.value = ''
  } catch (err) {
    // 轮询失败只内联提示，不打 toast 刷屏
    piezoError.value = err instanceof Error ? err.message : 'Piezo 状态获取失败'
  }
}

async function applySetpoint() {
  if (setpoint.value === undefined || Number.isNaN(setpoint.value)) {
    ElMessage.warning('请填写设定值')
    return
  }
  piezoBusy.value = true
  try {
    await piezoSetpoint(setpoint.value)
    ElMessage.success('设定值已下发')
    await refreshPiezo()
  } catch (err) {
    showApiError(err, '设定失败')
  } finally {
    piezoBusy.value = false
  }
}

async function onPiezoStart() {
  piezoBusy.value = true
  try {
    await piezoStart()
    ElMessage.success('Piezo 已启动')
    await refreshPiezo()
  } catch (err) {
    showApiError(err, '启动失败')
  } finally {
    piezoBusy.value = false
  }
}

async function onPiezoStop() {
  piezoBusy.value = true
  try {
    await piezoStop()
    ElMessage.success('Piezo 已停止')
    await refreshPiezo()
  } catch (err) {
    showApiError(err, '停止失败')
  } finally {
    piezoBusy.value = false
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

function fmtValue(v: number) {
  if (v === 0) return '0'
  const abs = Math.abs(v)
  if (abs >= 10000 || abs < 0.01) return v.toExponential(3)
  return String(Number(v.toPrecision(4)))
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

.piezo-alert {
  margin-bottom: 12px;
}

.piezo-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 12px 32px;
}

.piezo-stat {
  align-items: center;
  display: flex;
  gap: 10px;
}

.stat-label {
  color: var(--text-3);
  font-size: 13px;
}

.stat-value {
  color: var(--text-1);
  font-size: 20px;
  font-variant-numeric: tabular-nums;
}

.stat-error {
  color: var(--danger);
  font-size: 13px;
}

.piezo-controls {
  border-top: 1px solid var(--border);
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 16px;
  padding-top: 16px;
}

.setpoint-input {
  width: 160px;
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
</style>
