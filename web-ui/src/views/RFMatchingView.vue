<template>
  <!-- 列表骨架统一 base/ListPage（结构改版 R3）：筛选控件在 actions 槽左段；列表错误态收口 StateBlock -->
  <ListPage
    :title="t('rfMatching.title')"
    :error="error ? { message: error } : null"
    @retry="load"
  >
    <template #actions>
      <el-select v-model="device" class="filter-select" :placeholder="t('rfMatching.device')" @change="onFilter">
        <el-option :label="t('rfMatching.allDevices')" value="" />
        <el-option v-for="d in devices" :key="d" :label="d" :value="d" />
      </el-select>
      <el-select v-model="status" class="filter-select" :placeholder="t('rfMatching.status')" @change="onFilter">
        <el-option :label="t('rfMatching.allStatuses')" value="" />
        <el-option v-for="s in statuses" :key="s" :label="s" :value="s" />
      </el-select>
      <el-button v-if="!isViewer" type="primary" @click="openDialog">{{ t('rfMatching.create') }}</el-button>
    </template>
    <ResponsiveTable :rows="items" :loading="loading" :row-class-name="rowClass">
      <el-table-column prop="device" :label="t('rfMatching.device')" width="120" />
      <el-table-column prop="frequency_mhz" :label="t('rfMatching.frequency')" width="110" />
      <el-table-column :label="t('rfMatching.s11')" width="90">
        <template #default="{ row }">{{ row.s11 == null ? '—' : row.s11 }}</template>
      </el-table-column>
      <el-table-column :label="t('rfMatching.capacitance')" min-width="120" show-overflow-tooltip>
        <template #default="{ row }">{{ row.capacitance_text || '—' }}</template>
      </el-table-column>
      <el-table-column :label="t('rfMatching.status')" width="150">
        <template #default="{ row }">
          <el-tag v-if="row.status" :type="statusTag(row.status)" size="small" effect="light">{{ row.status }}</el-tag>
          <span v-else>—</span>
          <el-tooltip v-if="row.is_void" :content="row.void_reason ? t('rfMatching.voidReason', { reason: row.void_reason }) : t('rfMatching.voidedRecord')" placement="top">
            <el-tag class="void-tag" type="info" size="small" effect="plain">{{ t('rfMatching.voided') }}</el-tag>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column :label="t('rfMatching.measuredAt')" width="170">
        <template #default="{ row }">{{ formatDateTime(row.measured_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('rfMatching.measuredBy')" width="110">
        <template #default="{ row }">{{ row.measured_by || '—' }}</template>
      </el-table-column>
      <el-table-column v-if="!isViewer" :label="t('rfMatching.actions')" width="100">
        <template #default="{ row }">
          <el-button size="small" type="danger" plain :disabled="row.is_void" @click="voidRecord(row)">{{ t('rfMatching.void') }}</el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('rfMatching.empty')" />
      </template>
      <template #card="{ row }">
        <div class="rf-card" :class="{ void: row.is_void }">
          <span class="card-title">{{ row.device }} · {{ row.frequency_mhz }} MHz</span>
          <div class="card-fields">
            <span>{{ formatDateTime(row.measured_at) }}</span>
            <span>s11: {{ row.s11 == null ? '—' : row.s11 }}</span>
            <span>{{ row.capacitance_text || '—' }}</span>
            <el-tag v-if="row.status" :type="statusTag(row.status)" size="small" effect="light">{{ row.status }}</el-tag>
            <el-tag v-if="row.is_void" type="info" size="small" effect="plain">{{ t('rfMatching.voided') }}</el-tag>
          </div>
          <div class="card-actions">
            <el-button v-if="!isViewer" size="small" type="danger" plain :disabled="row.is_void" @click="voidRecord(row)">{{ t('rfMatching.void') }}</el-button>
          </div>
        </div>
      </template>
    </ResponsiveTable>
    <template #pagination>
      <el-pagination
        v-model:current-page="page"
        layout="total, prev, pager, next"
        :page-size="perPage"
        :total="total"
        @current-change="load"
      />
    </template>
  </ListPage>

  <FormDialog v-model="dialog" :title="t('rfMatching.dialogTitle')" width="640" :loading="submitting" @submit="create">
      <div class="form-grid">
        <el-form-item :label="t('rfMatching.device')" required>
          <el-select v-model="draft.device" :placeholder="t('rfMatching.selectDevice')">
            <el-option v-for="d in devices" :key="d" :label="d" :value="d" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('rfMatching.frequency')" required>
          <el-input-number v-model="draft.frequency_mhz" :controls="false" :min="0" :placeholder="t('rfMatching.mustBePositive')" />
        </el-form-item>
        <el-form-item :label="t('rfMatching.status')" required>
          <el-select v-model="draft.status" :placeholder="t('rfMatching.selectStatus')">
            <el-option v-for="s in statuses" :key="s" :label="s" :value="s" />
          </el-select>
        </el-form-item>
      </div>
      <el-collapse v-model="activeMore">
        <el-collapse-item :title="t('rfMatching.moreParams')" name="more">
          <div class="form-grid more-grid">
            <el-form-item :label="t('rfMatching.s11')">
              <el-input-number v-model="draft.s11" :controls="false" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.inputFreq')">
              <el-input-number v-model="draft.input_freq" :controls="false" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.inputVoltage')">
              <el-input-number v-model="draft.input_voltage" :controls="false" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.inputPower')">
              <el-input-number v-model="draft.input_power" :controls="false" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.inputDesc')">
              <el-input v-model="draft.input_desc" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.outputFreq')">
              <el-input-number v-model="draft.output_freq" :controls="false" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.outputVoltage')">
              <el-input-number v-model="draft.output_voltage" :controls="false" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.outputPower')">
              <el-input-number v-model="draft.output_power" :controls="false" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.outputDesc')">
              <el-input v-model="draft.output_desc" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.transformerTurns')">
              <el-input v-model="draft.transformer_turns" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.capacitance')">
              <el-input v-model="draft.capacitance_text" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.transformerMaterial')">
              <el-input v-model="draft.transformer_material" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.shuntInductance')">
              <el-input v-model="draft.shunt_inductance" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.seriesCapacitor')">
              <el-input v-model="draft.series_capacitor" />
            </el-form-item>
            <el-form-item :label="t('rfMatching.measuredAt')">
              <el-date-picker v-model="draft.measured_at" type="datetime" :placeholder="t('rfMatching.selectTime')" />
            </el-form-item>
          </div>
          <el-form-item :label="t('rfMatching.notes')">
            <el-input v-model="draft.notes" type="textarea" :rows="3" />
          </el-form-item>
        </el-collapse-item>
      </el-collapse>
  </FormDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { createRFMatching, deleteRFMatching, listRFMatching, type RFMatchingPayload, type RFMatchingRecord } from '../api/rfmatch'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'
import ListPage from '@/components/base/ListPage.vue'
import FormDialog from '@/components/base/FormDialog.vue'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import { formatDateTime } from '@/utils/datetime'

const route = useRoute()
const auth = useAuthStore()
const { t } = useI18n()

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
// single source of truth for projectId is the route param (guaranteed by ProjectLayout)
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
    items.value = data.items ?? []
    total.value = data.total
  } catch (err) {
    // 列表级错误收口 StateBlock（R3）：error ref 交 ListPage 展示并重试，不再 toast
    error.value = err instanceof Error ? err.message : t('rfMatching.loadFailed')
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
    ElMessage.warning(t('rfMatching.selectDeviceWarning'))
    return
  }
  const freq = num(draft.frequency_mhz)
  if (freq === undefined || freq <= 0) {
    ElMessage.warning(t('rfMatching.frequencyPositive'))
    return
  }
  if (!draft.status) {
    ElMessage.warning(t('rfMatching.selectStatusWarning'))
    return
  }
  // the backend has DisallowUnknownFields enabled; only submit whitelisted fields; drop empty values
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
    ElMessage.success(t('rfMatching.saved'))
    dialog.value = false
    await load()
  } catch (err) {
    showApiError(err, t('rfMatching.saveFailed'))
  } finally {
    submitting.value = false
  }
}

async function voidRecord(row: RFMatchingRecord) {
  let reason = ''
  try {
    const { value } = await ElMessageBox.prompt(t('rfMatching.voidReasonPrompt'), t('rfMatching.voidRecordTitle'), {
      confirmButtonText: t('rfMatching.confirmVoid'),
      cancelButtonText: t('common.cancel'),
      inputPlaceholder: t('rfMatching.voidReasonPlaceholder')
    })
    reason = (value || '').trim()
  } catch {
    return
  }
  try {
    await deleteRFMatching(row.id, reason)
    ElMessage.success(t('rfMatching.voidedSuccess'))
    await load()
  } catch (err) {
    showApiError(err, t('rfMatching.voidFailed'))
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

</script>

<style scoped>
.filter-select {
  width: 150px;
}

.void-tag {
  margin-left: 6px;
}

:deep(.void-row) {
  color: var(--text-3);
  opacity: 0.55;
}

.rf-card.void {
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
