<template>
  <!-- 列表骨架统一 base/ListPage（结构改版 R3）：筛选/搜索在 actions 槽左段；列表错误态收口 StateBlock -->
  <ListPage
    :title="t('stepTemplates.title')"
    :error="loadError ? { message: loadError } : null"
    @retry="load"
  >
    <template #actions>
      <el-select v-model="kindFilter" class="kind-select" :placeholder="t('stepTemplates.allTypes')" clearable @change="onFilter">
        <el-option :label="t('stepTemplates.assembly')" value="assembly" />
        <el-option :label="t('stepTemplates.experiment')" value="experiment" />
      </el-select>
      <el-input v-model="keyword" class="search-input" :placeholder="t('stepTemplates.searchPlaceholder')" clearable @change="onFilter" />
      <el-button v-if="canCreate" type="primary" @click="openCreate">{{ t('stepTemplates.newTemplate') }}</el-button>
    </template>
    <ResponsiveTable :rows="items" :loading="loading">
      <el-table-column :label="t('stepTemplates.name')" min-width="180">
        <template #default="{ row }">{{ row.name }}</template>
      </el-table-column>
      <el-table-column :label="t('stepTemplates.type')" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="row.kind === 'assembly' ? 'primary' : 'success'" effect="light">{{ kindLabel(row.kind) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('stepTemplates.stepCount')" width="80" align="center">
        <template #default="{ row }">{{ row._item_count ?? '—' }}</template>
      </el-table-column>
      <el-table-column :label="t('stepTemplates.source')" width="80">
        <template #default="{ row }">
          <el-tag v-if="row.ai_generated" size="small" type="warning" effect="light">AI</el-tag>
          <span v-else>{{ t('stepTemplates.manual') }}</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('stepTemplates.createdBy')" width="120">
        <template #default="{ row }">{{ row.created_by || '—' }}</template>
      </el-table-column>
      <el-table-column :label="t('stepTemplates.createdAt')" width="170">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('stepTemplates.actions')" width="300">
        <template #default="{ row }">
          <el-button size="small" @click="openDetail(row)">{{ t('stepTemplates.detail') }}</el-button>
          <el-button v-if="canManage(row)" size="small" @click="openEdit(row)">{{ t('stepTemplates.edit') }}</el-button>
          <el-button size="small" type="primary" plain @click="openApply(row)">{{ t('stepTemplates.applyToProject') }}</el-button>
          <el-button v-if="canManage(row)" size="small" type="danger" plain @click="remove(row)">{{ t('stepTemplates.delete') }}</el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty :description="t('stepTemplates.empty')" />
      </template>
      <template #card="{ row }">
        <div class="tmpl-card">
          <div class="tmpl-card-title">
            <span class="card-title">{{ row.name }}</span>
            <el-tag size="small" :type="row.kind === 'assembly' ? 'primary' : 'success'" effect="light">{{ kindLabel(row.kind) }}</el-tag>
          </div>
          <div class="card-fields">
            <span>{{ t('stepTemplates.stepCount') }}：{{ row._item_count ?? '—' }}</span>
            <span>{{ formatDateTime(row.created_at) }}</span>
          </div>
          <div class="card-actions">
            <el-button size="small" @click="openDetail(row)">{{ t('stepTemplates.detail') }}</el-button>
            <el-button v-if="canManage(row)" size="small" @click="openEdit(row)">{{ t('stepTemplates.edit') }}</el-button>
            <el-button size="small" type="primary" plain @click="openApply(row)">{{ t('stepTemplates.applyToProject') }}</el-button>
            <el-button v-if="canManage(row)" size="small" type="danger" plain @click="remove(row)">{{ t('stepTemplates.delete') }}</el-button>
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

    <el-dialog v-model="detailDialog" :title="t('stepTemplates.templateDetail')" width="640">
      <div v-loading="detailLoading" class="grid">
        <template v-if="detail">
          <el-descriptions border :column="1" size="small">
            <el-descriptions-item :label="t('stepTemplates.name')">{{ detail.name }}</el-descriptions-item>
            <el-descriptions-item :label="t('stepTemplates.type')">{{ kindLabel(detail.kind) }}</el-descriptions-item>
            <el-descriptions-item :label="t('stepTemplates.description')">{{ detail.description || '—' }}</el-descriptions-item>
            <el-descriptions-item v-if="detail.source_prompt" :label="t('stepTemplates.sourcePrompt')">{{ detail.source_prompt }}</el-descriptions-item>
            <el-descriptions-item :label="t('stepTemplates.source')">{{ detail.ai_generated ? t('stepTemplates.aiGenerated') : t('stepTemplates.manuallyCreated') }}</el-descriptions-item>
            <el-descriptions-item :label="t('stepTemplates.createdAt')">{{ formatDateTime(detail.created_at) }}</el-descriptions-item>
          </el-descriptions>
          <el-table :data="sortedItems(detail)" size="small">
            <el-table-column label="#" width="50" align="center">
              <template #default="{ row }">{{ row.step_order }}</template>
            </el-table-column>
            <el-table-column :label="t('stepTemplates.name')" min-width="150">
              <template #default="{ row }">{{ row.name }}</template>
            </el-table-column>
            <el-table-column :label="t('stepTemplates.description')" min-width="180">
              <template #default="{ row }">{{ row.description || '—' }}</template>
            </el-table-column>
            <el-table-column :label="t('stepTemplates.dependsOn')" width="80" align="center">
              <template #default="{ row }">{{ row.depends_on_order ?? '—' }}</template>
            </el-table-column>
          </el-table>
        </template>
      </div>
    </el-dialog>

    <FormDialog v-model="editDialog" :title="t('stepTemplates.editTemplate')" width="760" :loading="saving" @submit="saveEdit">
      <el-form-item :label="t('stepTemplates.name')" required><el-input v-model="editForm.name" maxlength="256" /></el-form-item>
      <el-form-item :label="t('stepTemplates.description')"><el-input v-model="editForm.description" type="textarea" :rows="2" maxlength="2000" /></el-form-item>
      <el-form-item :label="t('stepTemplates.steps')">
        <StepItemsEditor :key="editKey" v-model="editItems" />
      </el-form-item>
    </FormDialog>

    <FormDialog v-model="createDialog" :title="t('stepTemplates.createTemplate')" width="760" :loading="saving" @submit="saveCreate">
      <el-form-item :label="t('stepTemplates.name')" required><el-input v-model="createForm.name" maxlength="256" /></el-form-item>
      <el-form-item :label="t('stepTemplates.type')" required>
        <el-select v-model="createForm.kind">
          <el-option :label="t('stepTemplates.assembly')" value="assembly" />
          <el-option :label="t('stepTemplates.experiment')" value="experiment" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('stepTemplates.description')"><el-input v-model="createForm.description" type="textarea" :rows="2" maxlength="2000" /></el-form-item>
      <el-form-item :label="t('stepTemplates.steps')" required>
        <StepItemsEditor :key="createKey" v-model="createItems" />
      </el-form-item>
    </FormDialog>

    <FormDialog v-model="applyDialog" :title="t('stepTemplates.applyToProjectTitle')" width="480">
      <div class="grid">
        <p class="apply-tip">{{ applyTip }}</p>
        <el-select
          v-model="applyProjectId"
          v-loading="projectsLoading"
          :placeholder="t('stepTemplates.selectProject')"
          class="apply-select"
          @change="onApplyProjectChange"
        >
          <el-option v-for="p in projectOptions" :key="p.id" :label="p.short_name || p.name" :value="p.id" />
        </el-select>
        <el-select
          v-if="applyTarget?.kind === 'experiment'"
          v-model="applyRunId"
          v-loading="runsLoading"
          :placeholder="t('stepTemplates.selectRun')"
          class="apply-select"
          :disabled="!applyProjectId"
        >
          <el-option v-for="r in runOptions" :key="r.id" :label="r.name" :value="r.id" />
        </el-select>
      </div>
      <template #footer>
        <el-button @click="applyDialog = false">{{ t('stepTemplates.cancel') }}</el-button>
        <el-button
          type="primary"
          :loading="applying"
          :disabled="!applyProjectId || (applyTarget?.kind === 'experiment' && !applyRunId)"
          @click="confirmApply"
        >
          {{ t('stepTemplates.confirmApply') }}
        </el-button>
      </template>
    </FormDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import StepItemsEditor from '@/components/business/StepItemsEditor.vue'
import ListPage from '@/components/base/ListPage.vue'
import FormDialog from '@/components/base/FormDialog.vue'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import { formatDateTime } from '@/utils/datetime'
import {
  createTemplate,
  deleteTemplate,
  getTemplate,
  listTemplates,
  replaceTemplateItems,
  updateTemplate,
  type StepTemplate,
  type StepTemplateItem
} from '../api/stepTemplates'
import { applyAssemblyTemplate } from '../api/assembly'
import { applyRunTemplate, listRuns, type ExperimentRun } from '../api/runs'
import { listMembers, listProjects, type Project } from '../api/projects'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

const { t } = useI18n()
const auth = useAuthStore()
const items = ref<StepTemplate[]>([])
const loading = ref(false)
const loadError = ref('')
const keyword = ref('')
const kindFilter = ref('')
const page = ref(1)
const perPage = 20
const total = ref(0)

const detailDialog = ref(false)
const detailLoading = ref(false)
const detail = ref<StepTemplate | null>(null)

const editDialog = ref(false)
const editKey = ref(0)
const editTarget = ref<StepTemplate | null>(null)
const editForm = reactive({ name: '', description: '' })
const editItems = ref<StepTemplateItem[]>([])

const createDialog = ref(false)
const createKey = ref(0)
const createForm = reactive({ name: '', kind: 'assembly', description: '' })
const createItems = ref<StepTemplateItem[]>([])

const applyDialog = ref(false)
const applyTarget = ref<StepTemplate | null>(null)
const applyProjectId = ref('')
const projectOptions = ref<Project[]>([])
const projectsLoading = ref(false)
const applyRunId = ref('')
const runOptions = ref<ExperimentRun[]>([])
const runsLoading = ref(false)
const applying = ref(false)
const saving = ref(false)

// 与后端 requireWriteAccess 对齐：创建模板需 admin/maintainer；更新/删除允许创建人本人
const canCreate = computed(() => ['admin', 'maintainer'].includes(auth.user?.role || ''))

// experiment 模板应用到批次步骤，assembly 模板应用到项目装配步骤
const applyTip = computed(() => {
  if (!applyTarget.value) return ''
  const count = applyTarget.value._item_count ?? ''
  return applyTarget.value.kind === 'experiment'
    ? t('stepTemplates.applyTipExperiment', { name: applyTarget.value.name, count })
    : t('stepTemplates.applyTipAssembly', { name: applyTarget.value.name, count })
})

onMounted(load)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const res = await listTemplates({ kind: kindFilter.value || undefined, q: keyword.value.trim() || undefined, page: page.value, per_page: perPage })
    items.value = res.items ?? []
    total.value = res.total ?? 0
    fillItemCounts()
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : t('stepTemplates.loadFailed')
  } finally {
    loading.value = false
  }
}

// 列表接口不返回步骤数，逐条取详情补全；单行失败不影响整体
async function fillItemCounts() {
  await Promise.all(
    items.value.map(async (t) => {
      try {
        const d = await getTemplate(t.id)
        t._item_count = d.items?.length ?? 0
      } catch {
        /* 忽略单行失败 */
      }
    })
  )
}

function onFilter() {
  page.value = 1
  load()
}

function kindLabel(kind: string) {
  return kind === 'assembly' ? t('stepTemplates.assembly') : kind === 'experiment' ? t('stepTemplates.experiment') : kind
}


function sortedItems(t: StepTemplate) {
  return (t.items ?? []).slice().sort((a, b) => a.step_order - b.step_order)
}

function canManage(row: StepTemplate) {
  if (['admin', 'maintainer'].includes(auth.user?.role || '')) return true
  return !!row.created_by && row.created_by === auth.user?.id
}

function validItems(items: StepTemplateItem[]) {
  if (items.length < 1 || items.length > 30) {
    ElMessage.warning(t('stepTemplates.itemsCountRange'))
    return false
  }
  if (items.some((s) => !s.name.trim())) {
    ElMessage.warning(t('stepTemplates.allNamesRequired'))
    return false
  }
  return true
}

async function openDetail(row: StepTemplate) {
  detail.value = row
  detailDialog.value = true
  detailLoading.value = true
  try {
    detail.value = await getTemplate(row.id)
  } catch (err) {
    showApiError(err, t('stepTemplates.detailLoadFailed'))
  } finally {
    detailLoading.value = false
  }
}

async function openEdit(row: StepTemplate) {
  try {
    const d = await getTemplate(row.id)
    editTarget.value = d
    editForm.name = d.name
    editForm.description = d.description ?? ''
    editItems.value = sortedItems(d)
    editKey.value += 1
    editDialog.value = true
  } catch (err) {
    showApiError(err, t('stepTemplates.detailLoadFailed'))
  }
}

async function saveEdit() {
  if (!editTarget.value) return
  if (!editForm.name.trim()) {
    ElMessage.warning(t('stepTemplates.nameRequired'))
    return
  }
  if (!validItems(editItems.value)) return
  saving.value = true
  try {
    await updateTemplate(editTarget.value.id, { name: editForm.name.trim(), description: editForm.description.trim() })
    await replaceTemplateItems(editTarget.value.id, editItems.value)
    editDialog.value = false
    ElMessage.success(t('stepTemplates.saved'))
    await load()
  } catch (err) {
    showApiError(err, t('stepTemplates.saveFailed'))
  } finally {
    saving.value = false
  }
}

function openCreate() {
  createForm.name = ''
  createForm.kind = 'assembly'
  createForm.description = ''
  createItems.value = [{ name: '', step_order: 1, depends_on_order: null }]
  createKey.value += 1
  createDialog.value = true
}

async function saveCreate() {
  if (!createForm.name.trim()) {
    ElMessage.warning(t('stepTemplates.nameRequired'))
    return
  }
  if (!validItems(createItems.value)) return
  saving.value = true
  try {
    await createTemplate({
      name: createForm.name.trim(),
      kind: createForm.kind,
      description: createForm.description.trim() || undefined,
      items: createItems.value
    })
    createDialog.value = false
    ElMessage.success(t('stepTemplates.created'))
    await load()
  } catch (err) {
    showApiError(err, t('stepTemplates.createFailed'))
  } finally {
    saving.value = false
  }
}

async function openApply(row: StepTemplate) {
  applyTarget.value = row
  applyProjectId.value = ''
  applyRunId.value = ''
  runOptions.value = []
  projectOptions.value = []
  applyDialog.value = true
  if (row._item_count === undefined) {
    getTemplate(row.id)
      .then((d) => {
        if (applyTarget.value?.id === row.id) applyTarget.value._item_count = d.items?.length ?? 0
      })
      .catch(() => {})
  }
  projectsLoading.value = true
  try {
    const all = await listProjects()
    projectOptions.value = await filterMemberPlus(all ?? [])
  } catch (err) {
    showApiError(err, t('stepTemplates.projectsLoadFailed'))
  } finally {
    projectsLoading.value = false
  }
}

// 非 admin 的项目列表只含已加入的项目，再按成员角色过滤出 member 及以上
async function filterMemberPlus(all: Project[]) {
  if (auth.isAdmin) return all
  const uid = auth.user?.id
  if (!uid) return []
  const checks = await Promise.all(
    all.map(async (p) => {
      try {
        const members = await listMembers(p.id)
        const me = (members ?? []).find((m) => m.user_id === uid && m.status === 'active')
        return me && ['member', 'maintainer', 'owner'].includes(me.role) ? p : null
      } catch {
        return null
      }
    })
  )
  return checks.filter((p): p is Project => p !== null)
}

// experiment 模板需要再选目标批次：项目变更时加载该项目下的实验批次
async function onApplyProjectChange() {
  applyRunId.value = ''
  runOptions.value = []
  if (applyTarget.value?.kind !== 'experiment' || !applyProjectId.value) return
  runsLoading.value = true
  try {
    const res = await listRuns(applyProjectId.value, { per_page: 100 })
    runOptions.value = res.items ?? []
  } catch (err) {
    showApiError(err, t('stepTemplates.runsLoadFailed'))
  } finally {
    runsLoading.value = false
  }
}

async function confirmApply() {
  if (!applyTarget.value || !applyProjectId.value) return
  applying.value = true
  try {
    if (applyTarget.value.kind === 'experiment') {
      if (!applyRunId.value) return
      await applyRunTemplate(applyRunId.value, { template_id: applyTarget.value.id })
      ElMessage.success(t('stepTemplates.appliedToRun'))
    } else {
      await applyAssemblyTemplate(applyProjectId.value, { template_id: applyTarget.value.id })
      ElMessage.success(t('stepTemplates.appliedToAssembly'))
    }
    applyDialog.value = false
  } catch (err) {
    showApiError(err, t('stepTemplates.applyFailed'))
  } finally {
    applying.value = false
  }
}

async function remove(row: StepTemplate) {
  try {
    await ElMessageBox.confirm(t('stepTemplates.confirmDelete', { name: row.name }), t('stepTemplates.deleteTitle'), { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteTemplate(row.id)
    ElMessage.success(t('stepTemplates.deleted'))
    await load()
  } catch (err) {
    showApiError(err, t('stepTemplates.deleteFailed'))
  }
}
</script>

<style scoped>
.kind-select {
  max-width: 140px;
}

.search-input {
  max-width: 240px;
}

.apply-tip {
  color: var(--text-2);
  font-size: 13px;
}

.apply-select {
  width: 100%;
}

.tmpl-card-title {
  align-items: center;
  display: flex;
  gap: 8px;
  min-width: 0;
}
</style>
