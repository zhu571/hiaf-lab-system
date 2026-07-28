<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('runList.title') }}</h2>
      <div class="controls">
        <el-select v-model="statusFilter" class="status-select" :placeholder="t('runList.status')" @change="search">
          <el-option :label="t('runList.all')" value="" />
          <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
        <el-input v-model="campaign" class="campaign-input" :placeholder="t('runList.searchCampaign')" clearable @change="search" @clear="search" />
        <el-button v-if="canEdit" type="primary" @click="createDialog = true">{{ t('runList.create') }}</el-button>
      </div>
    </div>
    <section v-loading="loading" class="panel list-panel">
      <div v-if="error" class="error-box">
        <el-alert :title="error" type="error" show-icon :closable="false" />
        <el-button @click="load">{{ t('runList.retry') }}</el-button>
      </div>
      <template v-else>
        <el-empty v-if="!runs.length && !loading" :description="t('runList.empty')" />
        <div v-else class="run-grid">
          <button v-for="run in runs" :key="run.id" class="run-card" @click="open(run)">
            <span class="card-head">
              <strong>{{ run.name }}</strong>
              <StatusBadge :value="run.status" />
            </span>
            <span class="meta">{{ run.campaign || t('runList.noCampaign') }}</span>
            <span class="tags">
              <el-tag size="small" effect="plain">{{ runTypeLabel(run.run_type) }}</el-tag>
              <el-tag size="small" type="info" effect="plain">{{ run.gas_type }}</el-tag>
              <el-tag v-for="d in run.devices || []" :key="d" size="small" type="warning" effect="plain">{{ d }}</el-tag>
            </span>
            <span class="time">{{ t('runList.createdAt') }}{{ fmtTime(run.created_at) }}</span>
          </button>
        </div>
        <el-pagination
          v-if="total > 0"
          v-model:current-page="page"
          class="pager"
          background
          layout="total, prev, pager, next"
          :page-size="perPage"
          :total="total"
          @current-change="load"
        />
      </template>
    </section>
    <el-dialog v-model="createDialog" :title="t('runList.create')" width="620">
      <el-form label-position="top">
        <el-form-item :label="t('runList.nameLabel')"><el-input v-model="draft.name" /></el-form-item>
        <el-form-item label="Campaign"><el-input v-model="draft.campaign" /></el-form-item>
        <div class="form-row">
          <el-form-item :label="t('runList.type')">
            <el-select v-model="draft.run_type">
              <el-option v-for="t in runTypes" :key="t.value" :label="t.label" :value="t.value" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('runList.gasType')">
            <el-select v-model="draft.gas_type">
              <el-option v-for="g in gasTypes" :key="g" :label="g" :value="g" />
            </el-select>
          </el-form-item>
        </div>
        <div class="form-row">
          <el-form-item :label="t('runList.targetTemp')"><el-input-number v-model="draft.target_temp" :controls="false" :placeholder="t('runList.optional')" /></el-form-item>
          <el-form-item :label="t('runList.minTemp')"><el-input-number v-model="draft.min_temp" :controls="false" :placeholder="t('runList.optional')" /></el-form-item>
        </div>
        <div class="form-row three">
          <el-form-item :label="t('runList.pressureMin')"><el-input-number v-model="draft.pressure_min" :controls="false" :placeholder="t('runList.optional')" /></el-form-item>
          <el-form-item :label="t('runList.pressureMax')"><el-input-number v-model="draft.pressure_max" :controls="false" :placeholder="t('runList.optional')" /></el-form-item>
          <el-form-item :label="t('runList.pressureUnit')"><el-input v-model="draft.pressure_unit" /></el-form-item>
        </div>
        <el-form-item :label="t('runList.hasBeam')"><el-switch v-model="draft.has_beam" /></el-form-item>
        <el-form-item :label="t('runList.devices')">
          <el-select v-model="draft.devices" multiple :placeholder="t('runList.devicesPlaceholder')">
            <el-option v-for="d in deviceOptions" :key="d" :label="d" :value="d" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('runList.description')"><el-input v-model="draft.description" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="creating" @click="create">{{ t('runList.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import StatusBadge from '../components/StatusBadge.vue'
import { createRun, listRuns, type ExperimentRun, type RunPayload } from '../api/runs'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const runs = ref<ExperimentRun[]>([])
const loading = ref(false)
const error = ref('')
const page = ref(1)
const perPage = 12
const total = ref(0)
const statusFilter = ref('')
const campaign = ref('')
const createDialog = ref(false)
const creating = ref(false)

const statuses = computed(() => [
  { value: 'planned', label: t('runList.runStatus.planned') },
  { value: 'active', label: t('runList.runStatus.active') },
  { value: 'paused', label: t('runList.runStatus.paused') },
  { value: 'completed', label: t('runList.runStatus.completed') },
  { value: 'aborted', label: t('runList.runStatus.aborted') }
])
const runTypes = computed(() => [
  { value: 'cooldown', label: t('runList.runType.cooldown') },
  { value: 'warmup', label: t('runList.runType.warmup') },
  { value: 'steady_state', label: t('runList.runType.steady_state') },
  { value: 'test', label: t('runList.runType.test') }
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

const emptyDraft = (): RunForm => ({
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
const draft = reactive<RunForm>(emptyDraft())

// viewer 只读，隐藏所有写操作入口（后端仍强校验）
const canEdit = computed(() => !!auth.user && auth.user.role !== 'viewer')
// projectId 的唯一事实来源是路由参数（由 ProjectLayout 保证存在）
const projectId = computed(() => String(route.params.id || ''))

onMounted(load)
watch(projectId, () => {
  page.value = 1
  load()
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    if (!projectId.value) {
      runs.value = []
      total.value = 0
      return
    }
    const params: Record<string, string | number> = { page: page.value, per_page: perPage }
    if (statusFilter.value) params.status = statusFilter.value
    if (campaign.value.trim()) params.campaign = campaign.value.trim()
    const data = await listRuns(projectId.value, params)
    runs.value = data.items ?? []
    total.value = data.total
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('runList.loadFailed')
    showApiError(err, t('runList.loadFailed'))
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 1
  load()
}

function open(run: ExperimentRun) {
  router.push(`/experiment-runs/${run.id}`)
}

function runTypeLabel(value: string) {
  return runTypes.value.find((t) => t.value === value)?.label || value
}

function fmtTime(x?: string) {
  if (!x) return '—'
  return new Date(x).toLocaleString('zh-CN', { hour12: false })
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

async function create() {
  if (!draft.name.trim()) {
    ElMessage.warning(t('runList.nameRequired'))
    return
  }
  creating.value = true
  try {
    await createRun(projectId.value, toPayload(draft))
    ElMessage.success(t('runList.created'))
    createDialog.value = false
    Object.assign(draft, emptyDraft())
    await load()
  } catch (err) {
    showApiError(err, t('runList.createFailed'))
  } finally {
    creating.value = false
  }
}
</script>

<style scoped>
.controls {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.status-select {
  width: 140px;
}

.campaign-input {
  max-width: 220px;
}

.list-panel {
  min-height: 240px;
}

.error-box {
  display: grid;
  gap: 12px;
  justify-items: center;
  padding: 32px 0;
}

.run-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.run-card {
  background: #fff;
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow-sm);
  cursor: pointer;
  display: grid;
  gap: 8px;
  padding: 14px 16px;
  text-align: left;
  transition:
    border-color 0.15s ease,
    box-shadow 0.15s ease,
    transform 0.15s ease;
}

.run-card:hover {
  border-color: var(--brand-100);
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
}

.card-head {
  align-items: center;
  display: flex;
  gap: 8px;
  justify-content: space-between;
}

.card-head strong {
  color: var(--text-1);
  font-size: 14px;
  line-height: 1.4;
}

.meta,
.time {
  color: var(--text-3);
  font-size: 12px;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.pager {
  justify-content: flex-end;
  margin-top: 16px;
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
  .run-grid {
    grid-template-columns: 1fr;
  }

  .controls .el-select,
  .campaign-input {
    max-width: none;
    width: 100%;
  }
}
</style>
