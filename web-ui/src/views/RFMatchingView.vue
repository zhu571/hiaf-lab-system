<template>
  <div class="page">
    <div class="toolbar">
      <h2>RF 匹配</h2>
      <el-select v-model="device" class="filter-select" placeholder="设备" @change="onFilter">
        <el-option label="全部设备" value="" />
        <el-option v-for="d in devices" :key="d" :label="d" :value="d" />
      </el-select>
      <el-select v-model="status" class="filter-select" placeholder="状态" @change="onFilter">
        <el-option label="全部状态" value="" />
        <el-option v-for="s in statuses" :key="s" :label="s" :value="s" />
      </el-select>
      <el-button v-if="!isViewer" type="primary" @click="openDialog">新增记录</el-button>
    </div>

    <section class="panel">
      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false">
        <el-button size="small" @click="load">重试</el-button>
      </el-alert>
      <template v-else>
        <el-table v-loading="loading" :data="items" :row-class-name="rowClass">
          <el-table-column prop="device" label="设备" width="120" />
          <el-table-column prop="frequency_mhz" label="频率 (MHz)" width="110" />
          <el-table-column label="S11" width="90">
            <template #default="{ row }">{{ row.s11 == null ? '—' : row.s11 }}</template>
          </el-table-column>
          <el-table-column label="电容" min-width="120" show-overflow-tooltip>
            <template #default="{ row }">{{ row.capacitance_text || '—' }}</template>
          </el-table-column>
          <el-table-column label="状态" width="150">
            <template #default="{ row }">
              <el-tag v-if="row.status" :type="statusTag(row.status)" size="small" effect="light">{{ row.status }}</el-tag>
              <span v-else>—</span>
              <el-tooltip v-if="row.is_void" :content="row.void_reason ? `作废原因：${row.void_reason}` : '该记录已作废'" placement="top">
                <el-tag class="void-tag" type="info" size="small" effect="plain">已作废</el-tag>
              </el-tooltip>
            </template>
          </el-table-column>
          <el-table-column label="测量时间" width="170">
            <template #default="{ row }">{{ formatTime(row.measured_at) }}</template>
          </el-table-column>
          <el-table-column label="测量人" width="110">
            <template #default="{ row }">{{ row.measured_by || '—' }}</template>
          </el-table-column>
          <el-table-column v-if="!isViewer" label="操作" width="100">
            <template #default="{ row }">
              <el-button size="small" type="danger" plain :disabled="row.is_void" @click="voidRecord(row)">作废</el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="暂无记录" />
          </template>
        </el-table>
        <el-pagination
          v-model:current-page="page"
          class="pager"
          layout="total, prev, pager, next"
          :page-size="perPage"
          :total="total"
          @current-change="load"
        />
      </template>
    </section>

    <el-dialog v-model="dialog" title="新增 RF 匹配记录" width="640">
      <el-form label-position="top" @submit.prevent>
        <div class="form-grid">
          <el-form-item label="设备" required>
            <el-select v-model="draft.device" placeholder="选择设备">
              <el-option v-for="d in devices" :key="d" :label="d" :value="d" />
            </el-select>
          </el-form-item>
          <el-form-item label="频率 (MHz)" required>
            <el-input-number v-model="draft.frequency_mhz" :controls="false" :min="0" placeholder="必须大于 0" />
          </el-form-item>
          <el-form-item label="状态" required>
            <el-select v-model="draft.status" placeholder="选择状态">
              <el-option v-for="s in statuses" :key="s" :label="s" :value="s" />
            </el-select>
          </el-form-item>
        </div>
        <el-collapse v-model="activeMore">
          <el-collapse-item title="更多参数（可选）" name="more">
            <div class="form-grid more-grid">
              <el-form-item label="S11">
                <el-input-number v-model="draft.s11" :controls="false" />
              </el-form-item>
              <el-form-item label="输入频率">
                <el-input-number v-model="draft.input_freq" :controls="false" />
              </el-form-item>
              <el-form-item label="输入电压">
                <el-input-number v-model="draft.input_voltage" :controls="false" />
              </el-form-item>
              <el-form-item label="输入功率">
                <el-input-number v-model="draft.input_power" :controls="false" />
              </el-form-item>
              <el-form-item label="输入描述">
                <el-input v-model="draft.input_desc" />
              </el-form-item>
              <el-form-item label="输出频率">
                <el-input-number v-model="draft.output_freq" :controls="false" />
              </el-form-item>
              <el-form-item label="输出电压">
                <el-input-number v-model="draft.output_voltage" :controls="false" />
              </el-form-item>
              <el-form-item label="输出功率">
                <el-input-number v-model="draft.output_power" :controls="false" />
              </el-form-item>
              <el-form-item label="输出描述">
                <el-input v-model="draft.output_desc" />
              </el-form-item>
              <el-form-item label="变压器匝数">
                <el-input v-model="draft.transformer_turns" />
              </el-form-item>
              <el-form-item label="电容">
                <el-input v-model="draft.capacitance_text" />
              </el-form-item>
              <el-form-item label="变压器材料">
                <el-input v-model="draft.transformer_material" />
              </el-form-item>
              <el-form-item label="并联电感">
                <el-input v-model="draft.shunt_inductance" />
              </el-form-item>
              <el-form-item label="串联电容">
                <el-input v-model="draft.series_capacitor" />
              </el-form-item>
              <el-form-item label="测量时间">
                <el-date-picker v-model="draft.measured_at" type="datetime" placeholder="选择时间（可选）" />
              </el-form-item>
            </div>
            <el-form-item label="备注">
              <el-input v-model="draft.notes" type="textarea" :rows="3" />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="create">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createRFMatching, deleteRFMatching, listRFMatching, type RFMatchingPayload, type RFMatchingRecord } from '../api/rfmatch'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

const route = useRoute()
const auth = useAuthStore()

const items = ref<RFMatchingRecord[]>([])
const loading = ref(false)
const submitting = ref(false)
const error = ref('')
const page = ref(1)
const perPage = 20
const total = ref(0)
const device = ref('')
const status = ref('')
const dialog = ref(false)
const activeMore = ref<string[]>([])

const devices = ['rf_carpet', 'rfq', 'qpig']
const statuses = ['pass', 'adjust', 'fail']

const draft = reactive({
  device: '',
  frequency_mhz: undefined as number | undefined,
  status: '',
  s11: undefined as number | undefined,
  input_freq: undefined as number | undefined,
  input_voltage: undefined as number | undefined,
  input_power: undefined as number | undefined,
  input_desc: '',
  output_freq: undefined as number | undefined,
  output_voltage: undefined as number | undefined,
  output_power: undefined as number | undefined,
  output_desc: '',
  transformer_turns: '',
  capacitance_text: '',
  transformer_material: '',
  shunt_inductance: '',
  series_capacitor: '',
  measured_at: null as Date | null,
  notes: ''
})

const numericKeys = ['s11', 'input_freq', 'input_voltage', 'input_power', 'output_freq', 'output_voltage', 'output_power'] as const
const textKeys = ['input_desc', 'output_desc', 'transformer_turns', 'capacitance_text', 'transformer_material', 'shunt_inductance', 'series_capacitor'] as const

const isViewer = computed(() => auth.user?.role === 'viewer')
// projectId 的唯一事实来源是路由参数（由 ProjectLayout 保证存在）
const projectId = computed(() => String(route.params.id || ''))

onMounted(load)
watch(projectId, load)

async function load() {
  if (!projectId.value) return
  loading.value = true
  error.value = ''
  try {
    const params: Record<string, string | number> = { page: page.value, per_page: perPage }
    if (device.value) params.device = device.value
    if (status.value) params.status = status.value
    const data = await listRFMatching(projectId.value, params)
    items.value = data.items
    total.value = data.total
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'RF 匹配记录加载失败'
    showApiError(err, 'RF 匹配记录加载失败')
  } finally {
    loading.value = false
  }
}

function onFilter() {
  page.value = 1
  load()
}

function num(v: number | undefined) {
  return typeof v === 'number' && !Number.isNaN(v) ? v : undefined
}

function openDialog() {
  resetDraft()
  dialog.value = true
}

function resetDraft() {
  draft.device = ''
  draft.frequency_mhz = undefined
  draft.status = ''
  for (const key of numericKeys) draft[key] = undefined
  for (const key of textKeys) draft[key] = ''
  draft.measured_at = null
  draft.notes = ''
  activeMore.value = []
}

async function create() {
  if (!draft.device) {
    ElMessage.warning('请选择设备')
    return
  }
  const freq = num(draft.frequency_mhz)
  if (freq === undefined || freq <= 0) {
    ElMessage.warning('频率必须大于 0')
    return
  }
  if (!draft.status) {
    ElMessage.warning('请选择状态')
    return
  }
  // 后端开启 DisallowUnknownFields，只提交白名单内字段；空值剔除
  const payload: RFMatchingPayload = { device: draft.device, frequency_mhz: freq, status: draft.status }
  for (const key of numericKeys) {
    const v = num(draft[key])
    if (v !== undefined) payload[key] = v
  }
  for (const key of textKeys) {
    const v = draft[key].trim()
    if (v) payload[key] = v
  }
  if (draft.measured_at) payload.measured_at = new Date(draft.measured_at).toISOString()
  const notes = draft.notes.trim()
  if (notes) payload.notes = notes
  submitting.value = true
  try {
    await createRFMatching(projectId.value, payload)
    ElMessage.success('记录已保存')
    dialog.value = false
    await load()
  } catch (err) {
    showApiError(err, '保存失败')
  } finally {
    submitting.value = false
  }
}

async function voidRecord(row: RFMatchingRecord) {
  let reason = ''
  try {
    const { value } = await ElMessageBox.prompt('请输入作废原因（可留空）', '作废记录', {
      confirmButtonText: '确认作废',
      cancelButtonText: '取消',
      inputPlaceholder: '作废原因（可留空）'
    })
    reason = (value || '').trim()
  } catch {
    return
  }
  try {
    await deleteRFMatching(row.id, reason)
    ElMessage.success('记录已作废')
    await load()
  } catch (err) {
    showApiError(err, '作废失败')
  }
}

function rowClass({ row }: { row: RFMatchingRecord }) {
  return row.is_void ? 'void-row' : ''
}

function statusTag(v: string): 'success' | 'warning' | 'danger' | 'info' {
  if (v === 'pass') return 'success'
  if (v === 'adjust') return 'warning'
  if (v === 'fail') return 'danger'
  return 'info'
}

function formatTime(v?: string) {
  return v ? new Date(v).toLocaleString('zh-CN', { hour12: false }) : '—'
}
</script>

<style scoped>
.filter-select {
  width: 150px;
}

.pager {
  justify-content: flex-end;
  margin-top: 14px;
}

.void-tag {
  margin-left: 6px;
}

:deep(.void-row) {
  color: var(--text-3);
  opacity: 0.55;
}

.form-grid {
  display: grid;
  gap: 0 14px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.form-grid .el-select,
.form-grid .el-input-number,
.form-grid .el-date-editor {
  width: 100%;
}

.more-grid {
  padding-top: 8px;
}

@media (max-width: 768px) {
  .filter-select {
    width: 100%;
  }

  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
