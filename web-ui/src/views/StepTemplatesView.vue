<template>
  <div class="page">
    <div class="toolbar">
      <h2>步骤模板</h2>
      <el-select v-model="kindFilter" class="kind-select" placeholder="全部类型" clearable @change="onFilter">
        <el-option label="装配" value="assembly" />
        <el-option label="实验" value="experiment" />
      </el-select>
      <el-input v-model="keyword" class="search-input" placeholder="搜索名称 / 描述" clearable @change="onFilter" />
      <el-button v-if="canCreate" type="primary" @click="openCreate">新建模板</el-button>
    </div>
    <section class="panel">
      <el-alert v-if="loadError" class="load-error" type="error" :title="loadError" show-icon :closable="false">
        <el-button size="small" @click="load">重试</el-button>
      </el-alert>
      <el-table v-loading="loading" :data="items">
        <el-table-column label="名称" min-width="180">
          <template #default="{ row }">{{ row.name }}</template>
        </el-table-column>
        <el-table-column label="类型" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="row.kind === 'assembly' ? 'primary' : 'success'" effect="light">{{ kindLabel(row.kind) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="步骤数" width="80" align="center">
          <template #default="{ row }">{{ row._item_count ?? '—' }}</template>
        </el-table-column>
        <el-table-column label="来源" width="80">
          <template #default="{ row }">
            <el-tag v-if="row.ai_generated" size="small" type="warning" effect="light">AI</el-tag>
            <span v-else>手动</span>
          </template>
        </el-table-column>
        <el-table-column label="创建人" width="120">
          <template #default="{ row }">{{ row.created_by || '—' }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="300" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openDetail(row)">详情</el-button>
            <el-button v-if="canManage(row)" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="primary" plain @click="openApply(row)">应用到项目</el-button>
            <el-button v-if="canManage(row)" size="small" type="danger" plain @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无步骤模板" />
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
    </section>

    <el-dialog v-model="detailDialog" title="模板详情" width="640">
      <div v-loading="detailLoading" class="grid">
        <template v-if="detail">
          <el-descriptions border :column="1" size="small">
            <el-descriptions-item label="名称">{{ detail.name }}</el-descriptions-item>
            <el-descriptions-item label="类型">{{ kindLabel(detail.kind) }}</el-descriptions-item>
            <el-descriptions-item label="描述">{{ detail.description || '—' }}</el-descriptions-item>
            <el-descriptions-item v-if="detail.source_prompt" label="来源提示词">{{ detail.source_prompt }}</el-descriptions-item>
            <el-descriptions-item label="来源">{{ detail.ai_generated ? 'AI 生成' : '手动创建' }}</el-descriptions-item>
            <el-descriptions-item label="创建时间">{{ fmtTime(detail.created_at) }}</el-descriptions-item>
          </el-descriptions>
          <el-table :data="sortedItems(detail)" size="small">
            <el-table-column label="#" width="50" align="center">
              <template #default="{ row }">{{ row.step_order }}</template>
            </el-table-column>
            <el-table-column label="名称" min-width="150">
              <template #default="{ row }">{{ row.name }}</template>
            </el-table-column>
            <el-table-column label="描述" min-width="180">
              <template #default="{ row }">{{ row.description || '—' }}</template>
            </el-table-column>
            <el-table-column label="依赖" width="80" align="center">
              <template #default="{ row }">{{ row.depends_on_order ?? '—' }}</template>
            </el-table-column>
          </el-table>
        </template>
      </div>
    </el-dialog>

    <el-dialog v-model="editDialog" title="编辑模板" width="760">
      <el-form label-position="top">
        <el-form-item label="名称" required><el-input v-model="editForm.name" maxlength="256" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="editForm.description" type="textarea" :rows="2" maxlength="2000" /></el-form-item>
        <el-form-item label="步骤">
          <StepItemsEditor :key="editKey" v-model="editItems" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveEdit">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="createDialog" title="新建模板" width="760">
      <el-form label-position="top">
        <el-form-item label="名称" required><el-input v-model="createForm.name" maxlength="256" /></el-form-item>
        <el-form-item label="类型" required>
          <el-select v-model="createForm.kind">
            <el-option label="装配" value="assembly" />
            <el-option label="实验" value="experiment" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述"><el-input v-model="createForm.description" type="textarea" :rows="2" maxlength="2000" /></el-form-item>
        <el-form-item label="步骤" required>
          <StepItemsEditor :key="createKey" v-model="createItems" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveCreate">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="applyDialog" title="应用到项目" width="480">
      <div class="grid">
        <p class="apply-tip">将模板「{{ applyTarget?.name }}」的 {{ applyTarget?._item_count ?? '' }} 个步骤追加到目标项目的装配步骤。</p>
        <el-select v-model="applyProjectId" v-loading="projectsLoading" placeholder="选择项目（需 member 及以上角色）" class="apply-select">
          <el-option v-for="p in projectOptions" :key="p.id" :label="p.short_name || p.name" :value="p.id" />
        </el-select>
      </div>
      <template #footer>
        <el-button @click="applyDialog = false">取消</el-button>
        <el-button type="primary" :loading="applying" :disabled="!applyProjectId" @click="confirmApply">确认应用</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import StepItemsEditor from '../components/StepItemsEditor.vue'
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
import { listMembers, listProjects, type Project } from '../api/projects'
import { useAuthStore } from '../stores/auth'
import { showApiError } from '../composables/useNotify'

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
const applying = ref(false)
const saving = ref(false)

// 与后端 requireWriteAccess 对齐：创建模板需 admin/maintainer；更新/删除允许创建人本人
const canCreate = computed(() => ['admin', 'maintainer'].includes(auth.user?.role || ''))

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
    loadError.value = err instanceof Error ? err.message : '模板列表加载失败'
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
  return kind === 'assembly' ? '装配' : kind === 'experiment' ? '实验' : kind
}

function fmtTime(t?: string) {
  return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '—'
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
    ElMessage.warning('步骤数需在 1-30 之间')
    return false
  }
  if (items.some((s) => !s.name.trim())) {
    ElMessage.warning('请填写所有步骤名称')
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
    showApiError(err, '模板详情加载失败')
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
    showApiError(err, '模板详情加载失败')
  }
}

async function saveEdit() {
  if (!editTarget.value) return
  if (!editForm.name.trim()) {
    ElMessage.warning('请填写模板名称')
    return
  }
  if (!validItems(editItems.value)) return
  saving.value = true
  try {
    await updateTemplate(editTarget.value.id, { name: editForm.name.trim(), description: editForm.description.trim() })
    await replaceTemplateItems(editTarget.value.id, editItems.value)
    editDialog.value = false
    ElMessage.success('模板已保存')
    await load()
  } catch (err) {
    showApiError(err, '模板保存失败')
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
    ElMessage.warning('请填写模板名称')
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
    ElMessage.success('模板已创建')
    await load()
  } catch (err) {
    showApiError(err, '模板创建失败')
  } finally {
    saving.value = false
  }
}

async function openApply(row: StepTemplate) {
  applyTarget.value = row
  applyProjectId.value = ''
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
    showApiError(err, '项目列表加载失败')
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

async function confirmApply() {
  if (!applyTarget.value || !applyProjectId.value) return
  applying.value = true
  try {
    await applyAssemblyTemplate(applyProjectId.value, { template_id: applyTarget.value.id })
    applyDialog.value = false
    ElMessage.success('模板已应用到项目装配步骤')
  } catch (err) {
    showApiError(err, '应用模板失败')
  } finally {
    applying.value = false
  }
}

async function remove(row: StepTemplate) {
  try {
    await ElMessageBox.confirm(`确认删除模板「${row.name}」？`, '删除模板', { type: 'warning' })
  } catch {
    return
  }
  try {
    await deleteTemplate(row.id)
    ElMessage.success('模板已删除')
    await load()
  } catch (err) {
    showApiError(err, '模板删除失败')
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

.load-error {
  margin-bottom: 16px;
}

.pager {
  justify-content: flex-end;
  margin-top: 14px;
}

.apply-tip {
  color: var(--text-2);
  font-size: 13px;
}

.apply-select {
  width: 100%;
}
</style>
