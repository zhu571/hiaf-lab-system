<template>
  <div class="page">
    <div class="toolbar">
      <h2>实验批次</h2>
      <div class="controls">
        <el-select v-model="statusFilter" class="status-select" placeholder="状态" @change="search">
          <el-option label="全部" value="" />
          <el-option v-for="s in statuses" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
        <el-input v-model="campaign" class="campaign-input" placeholder="搜索 campaign" clearable @change="search" @clear="search" />
        <el-button v-if="canEdit" type="primary" @click="createDialog = true">新建批次</el-button>
      </div>
    </div>
    <section v-loading="loading" class="panel list-panel">
      <div v-if="error" class="error-box">
        <el-alert :title="error" type="error" show-icon :closable="false" />
        <el-button @click="load">重试</el-button>
      </div>
      <template v-else>
        <el-empty v-if="!runs.length && !loading" description="暂无实验批次" />
        <div v-else class="run-grid">
          <button v-for="run in runs" :key="run.id" class="run-card" @click="open(run)">
            <span class="card-head">
              <strong>{{ run.name }}</strong>
              <StatusBadge :value="run.status" />
            </span>
            <span class="meta">{{ run.campaign || '未设置 campaign' }}</span>
            <span class="tags">
              <el-tag size="small" effect="plain">{{ runTypeLabel(run.run_type) }}</el-tag>
              <el-tag size="small" type="info" effect="plain">{{ run.gas_type }}</el-tag>
              <el-tag v-for="d in run.devices || []" :key="d" size="small" type="warning" effect="plain">{{ d }}</el-tag>
            </span>
            <span class="time">创建：{{ fmtTime(run.created_at) }}</span>
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
    <el-dialog v-model="createDialog" title="新建批次" width="620">
      <el-form label-position="top">
        <el-form-item label="名称（必填）"><el-input v-model="draft.name" /></el-form-item>
        <el-form-item label="Campaign"><el-input v-model="draft.campaign" /></el-form-item>
        <div class="form-row">
          <el-form-item label="类型">
            <el-select v-model="draft.run_type">
              <el-option v-for="t in runTypes" :key="t.value" :label="t.label" :value="t.value" />
            </el-select>
          </el-form-item>
          <el-form-item label="气体">
            <el-select v-model="draft.gas_type">
              <el-option v-for="g in gasTypes" :key="g" :label="g" :value="g" />
            </el-select>
          </el-form-item>
        </div>
        <div class="form-row">
          <el-form-item label="目标温度"><el-input-number v-model="draft.target_temp" :controls="false" placeholder="可选" /></el-form-item>
          <el-form-item label="最低温度"><el-input-number v-model="draft.min_temp" :controls="false" placeholder="可选" /></el-form-item>
        </div>
        <div class="form-row three">
          <el-form-item label="压力下限"><el-input-number v-model="draft.pressure_min" :controls="false" placeholder="可选" /></el-form-item>
          <el-form-item label="压力上限"><el-input-number v-model="draft.pressure_max" :controls="false" placeholder="可选" /></el-form-item>
          <el-form-item label="压力单位"><el-input v-model="draft.pressure_unit" /></el-form-item>
        </div>
        <el-form-item label="有束流"><el-switch v-model="draft.has_beam" /></el-form-item>
        <el-form-item label="设备">
          <el-select v-model="draft.devices" multiple placeholder="选择设备">
            <el-option v-for="d in deviceOptions" :key="d" :label="d" :value="d" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述"><el-input v-model="draft.description" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import StatusBadge from '../components/StatusBadge.vue'
import { createRun, listRuns, type ExperimentRun, type RunPayload } from '../api/runs'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

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

const statuses = [
  { value: 'planned', label: '计划中' },
  { value: 'active', label: '进行中' },
  { value: 'paused', label: '已暂停' },
  { value: 'completed', label: '已完成' },
  { value: 'aborted', label: '已中止' }
]
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
    runs.value = data.items
    total.value = data.total
  } catch (err) {
    error.value = err instanceof Error ? err.message : '批次加载失败'
    showApiError(err, '批次加载失败')
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
  return runTypes.find((t) => t.value === value)?.label || value
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
    ElMessage.warning('请填写批次名称')
    return
  }
  creating.value = true
  try {
    await createRun(projectId.value, toPayload(draft))
    ElMessage.success('批次已创建')
    createDialog.value = false
    Object.assign(draft, emptyDraft())
    await load()
  } catch (err) {
    showApiError(err, '批次创建失败')
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
