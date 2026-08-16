<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('dailyReport.title') }}</h2>
      <el-button v-if="canSubmit" type="primary" :disabled="!report" @click="submit(false)">{{ t('dailyReport.submit') }}</el-button>
    </div>
    <section class="panel editor-panel">
      <div class="toolbar">
        <h3>{{ t('dailyReport.todayRecord') }}</h3>
        <div class="toolbar-actions">
          <el-upload :auto-upload="false" :show-file-list="false" :on-change="onFileSelect" accept="image/*,.pdf">
            <el-button>{{ t('dailyReport.addAttachment') }}</el-button>
          </el-upload>
          <el-button @click="saveRaw">{{ t('dailyReport.saveRaw') }}</el-button>
        </div>
      </div>
      <el-input v-model="rawText" type="textarea" :rows="8" :placeholder="t('dailyReport.editorPlaceholder')" />
      <div class="ai-row">
        <el-button type="primary" plain :disabled="!canAiOrganize" :loading="aiLoading" @click="organizeWithAI">
          {{ aiLoading ? t('dailyReport.aiOrganizing') : t('dailyReport.aiOrganize') }}
        </el-button>
      </div>
    </section>
    <section class="panel">
      <div class="toolbar">
        <h3>{{ t('dailyReport.structuredLogs') }}</h3>
        <el-button @click="openAddLog">{{ t('dailyReport.addLog') }}</el-button>
      </div>
      <ResponsiveTable :rows="(tableRows as any[])">
        <el-table-column :label="t('dailyReport.category')" width="150">
          <template #default="{ row }">
            <el-select v-if="row._draft" v-model="row.category" size="small">
              <el-option v-for="c in categories" :key="c" :label="c" :value="c" />
            </el-select>
            <template v-else>{{ row.category }}</template>
          </template>
        </el-table-column>
        <el-table-column :label="t('dailyReport.project')" width="160">
          <template #default="{ row }">
            <el-select v-if="row._draft" v-model="row.project_id" size="small">
              <el-option v-for="p in projects.projects" :key="p.id" :label="p.name" :value="p.id" />
            </el-select>
            <template v-else>{{ projectName(row.project_id) }}</template>
          </template>
        </el-table-column>
        <el-table-column :label="t('dailyReport.occurredAt')" width="220">
          <template #default="{ row }">
            <el-date-picker v-if="row._draft" v-model="row.occurred_at" type="datetime" size="small" value-format="YYYY-MM-DDTHH:mm:ssZ" style="width: 200px" />
            <template v-else>{{ row.occurred_at }}</template>
          </template>
        </el-table-column>
        <el-table-column :label="t('dailyReport.content')">
          <template #default="{ row }">
            <el-input v-if="row._draft" v-model="row.content" type="textarea" :rows="2" size="small" />
            <template v-else>{{ row.content }}</template>
          </template>
        </el-table-column>
        <el-table-column :label="t('dailyReport.status')" width="90">
          <template #default="{ row }">
            <el-tag v-if="row._draft" size="small" type="warning">{{ t('dailyReport.aiTag') }}</el-tag>
            <StatusBadge v-else :value="row.content_status" />
          </template>
        </el-table-column>
        <el-table-column :label="t('dailyReport.actions')" width="160">
          <template #default="{ row }">
            <template v-if="row._draft">
              <el-button link type="success" :loading="row.confirming" @click="confirmDraft(row)">{{ t('dailyReport.confirm') }}</el-button>
              <el-button link type="danger" @click="removeDraft(row)">{{ t('dailyReport.remove') }}</el-button>
            </template>
            <template v-else-if="row.content_status === 'draft'">
              <el-button link type="primary" @click="openEditLog(row)">{{ t('dailyReport.edit') }}</el-button>
              <el-button link type="success" @click="confirmLog(row.id)">{{ t('dailyReport.confirm') }}</el-button>
            </template>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="t('dailyReport.emptyLogs')" />
        </template>
        <template #card="{ row }">
          <div class="log-card">
            <div class="log-card-head">
              <span class="log-card-time">{{ row.occurred_at }}</span>
              <el-tag v-if="row._draft" size="small" type="warning">{{ t('dailyReport.aiTag') }}</el-tag>
              <StatusBadge v-else :value="row.content_status" />
            </div>
            <div class="log-card-meta">{{ row.category }}<template v-if="row.project_id"> · {{ projectName(row.project_id) }}</template></div>
            <p class="log-card-content">{{ row.content }}</p>
            <div class="log-card-actions">
              <template v-if="row._draft">
                <el-button size="small" type="success" :loading="row.confirming" @click="confirmDraft(row)">{{ t('dailyReport.confirm') }}</el-button>
                <el-button size="small" @click="removeDraft(row)">{{ t('dailyReport.remove') }}</el-button>
              </template>
              <template v-else-if="row.content_status === 'draft'">
                <el-button size="small" type="primary" @click="openEditLog(row)">{{ t('dailyReport.edit') }}</el-button>
                <el-button size="small" type="success" @click="confirmLog(row.id)">{{ t('dailyReport.confirm') }}</el-button>
              </template>
            </div>
          </div>
        </template>
      </ResponsiveTable>
    </section>
    <el-dialog v-model="logDialog" :title="editingLogId ? t('dailyReport.editLog') : t('dailyReport.addNewLog')" width="560">
      <el-form label-position="top">
        <el-form-item v-if="!editingLogId" :label="t('dailyReport.project')"><el-select v-model="logDraft.project_id"><el-option v-for="p in projects.projects" :key="p.id" :label="p.name" :value="p.id" /></el-select></el-form-item>
        <el-form-item :label="t('dailyReport.category')"><el-input v-model="logDraft.category" /></el-form-item>
        <el-form-item :label="t('dailyReport.content')"><el-input v-model="logDraft.content" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="logDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="saveLog">{{ t('dailyReport.save') }}</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="warningDialog" :title="t('dailyReport.confirmSubmit')" width="520">
      <div class="warning-list">
        <el-alert v-for="warning in warnings" :key="warning.code + warning.log_id" :title="warning.message" type="warning" show-icon :closable="false" />
      </div>
      <template #footer>
        <el-button @click="warningDialog = false">{{ t('dailyReport.backToEdit') }}</el-button>
        <el-button type="warning" @click="submit(true)">{{ t('dailyReport.ignoreSubmit') }}</el-button>
      </template>
    </el-dialog>

    <section v-if="pendingFiles.length" class="panel">
      <h3>{{ t('dailyReport.attachments', { n: pendingFiles.length }) }}</h3>
      <div class="file-list">
        <div v-for="f in pendingFiles" :key="f.name" class="file-item">
          <el-icon><Paperclip /></el-icon>
          <span>{{ f.name }}</span>
          <span class="muted">({{ formatSize(f.size) }})</span>
          <span v-if="f.uploaded" style="color:var(--ok)">✓</span>
          <el-button v-else size="small" @click="uploadPendingFile(f)">{{ t('dailyReport.upload') }}</el-button>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import { Paperclip } from '@element-plus/icons-vue'
import StatusBadge from '@/components/base/StatusBadge.vue'
import ResponsiveTable from '@/components/base/ResponsiveTable.vue'
import { aiParseReport, createLog, submitReport, todayReport, updateLog, updateReportRawText, type DailyReport, type LogItem } from '../api/logs'
import { useProjectStore } from '../stores/project'
import { useAuthStore } from '../stores/auth'
import { uploadAttachment } from '../api/attachments'

const { t } = useI18n()
const projectStore = useProjectStore()
const auth = useAuthStore()
const canSubmit = computed(() => auth.user?.role !== 'viewer')
const projects = projectStore

// Attachments
type PendingFile = { file: File; name: string; size: number; uploaded: boolean }
const pendingFiles = ref<PendingFile[]>([])

function formatSize(bytes: number) {
  if (bytes < 1024) return bytes + 'B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + 'KB'
  return (bytes / 1048576).toFixed(1) + 'MB'
}

async function onFileSelect(uploadFile: any) {
  const file = uploadFile.raw as File
  const entry: PendingFile = { file, name: file.name, size: file.size, uploaded: false }
  pendingFiles.value.push(entry)

  // Upload immediately if today's report already exists
  if (report.value?.id) {
    await uploadPendingFile(entry)
  } else {
    ElMessage.info(t('dailyReport.autoUploadHint', { name: file.name }))
  }
}

async function uploadPendingFile(pf: PendingFile) {
  if (!report.value?.id) return
  try {
    await uploadAttachment(pf.file, 'daily_report', report.value.id)
    pf.uploaded = true
  } catch {
    ElMessage.warning(t('dailyReport.uploadFailed', { name: pf.name }))
  }
}

async function uploadAllPending() {
  if (!report.value?.id) return
  for (const pf of pendingFiles.value) {
    if (!pf.uploaded) await uploadPendingFile(pf)
  }
}
const report = ref<DailyReport | null>(null)
const rawText = ref('')
const logDialog = ref(false)
const editingLogId = ref('')
const warningDialog = ref(false)
const warnings = ref<Array<{ code: string; message: string; log_id?: string }>>([])
const logDraft = reactive({ project_id: '', category: 'general', content: '' })

// AI 整理草稿：仅前端内存态，刷新丢失（方案明示的取舍）。
type AiDraftRow = {
  _draft: true
  key: number
  category: string
  project_id: string
  content: string
  occurred_at: string
  confirming: boolean
}
const aiDrafts = ref<AiDraftRow[]>([])
const aiLoading = ref(false)
let aiDraftSeq = 0
const categories = ['general', 'assembly', 'test', 'cryo', 'rf', 'vacuum', 'beam', 'data_analysis']
const tableRows = computed(() => [...(report.value?.logs || []), ...aiDrafts.value])
const canAiOrganize = computed(
  () => !!report.value && report.value.content_status === 'draft' && rawText.value.trim() !== '' && !aiLoading.value
)

function projectName(id: string) {
  return projects.projects.find((p) => p.id === id)?.name || id
}

async function organizeWithAI() {
  if (!report.value) return
  aiLoading.value = true
  try {
    // 后端从已保存的 raw_text 取数：先落盘当前编辑内容，避免整理到旧文本或空文本
    if (rawText.value !== report.value.raw_text) {
      report.value = await updateReportRawText(report.value.id, rawText.value)
    }
    const { data } = await aiParseReport(report.value.id)
    if (data.status === 'ok') {
      for (const log of data.logs) {
        aiDrafts.value.push({ _draft: true, key: ++aiDraftSeq, confirming: false, ...log })
      }
    } else if (data.status === 'clarify') {
      await ElMessageBox.alert(data.question || '', t('dailyReport.aiClarifyTitle'))
    } else {
      await ElMessageBox.alert(data.reason || '', t('dailyReport.aiRejectedTitle'))
    }
  } catch (err) {
    const e = err as (Error & { requestId?: string; status?: number }) | undefined
    const key =
      e?.status === 502 ? 'aiUpstreamDown'
      : e?.status === 429 ? 'aiRateLimited'
      : e?.status === 409 ? 'aiDuplicate'
      : 'aiFailed'
    const message = t(`dailyReport.${key}`)
    ElMessage.error(e?.requestId ? `${message}（request_id: ${e.requestId}）` : message)
  } finally {
    aiLoading.value = false
  }
}

async function confirmDraft(row: AiDraftRow) {
  if (!report.value) return
  row.confirming = true
  try {
    await createLog(row.project_id, {
      daily_report_id: report.value.id,
      category: row.category,
      content: row.content,
      occurred_at: row.occurred_at,
      source: 'agent'
    })
    // 部分成功语义：单条失败只提示该条，其余草稿保留；成功才刷新并移除该行。
    report.value = await todayReport()
    aiDrafts.value = aiDrafts.value.filter((item) => item.key !== row.key)
    ElMessage.success(t('dailyReport.logConfirmed'))
  } catch (err) {
    showApiError(err, t('dailyReport.aiConfirmFailed'))
  } finally {
    row.confirming = false
  }
}

function removeDraft(row: AiDraftRow) {
  aiDrafts.value = aiDrafts.value.filter((item) => item.key !== row.key)
}

onMounted(async () => {
  await projects.load()
  report.value = await todayReport()
  rawText.value = report.value.raw_text
  logDraft.project_id = projects.current?.id || ''
})

async function saveRaw() {
  if (!report.value) return
  report.value = await updateReportRawText(report.value.id, rawText.value)
  ElMessage.success(t('dailyReport.saved'))
}

function openAddLog() {
  editingLogId.value = ''
  logDraft.category = 'general'
  logDraft.content = ''
  logDialog.value = true
}

function openEditLog(log: LogItem) {
  editingLogId.value = log.id
  logDraft.category = log.category
  logDraft.content = log.content
  logDialog.value = true
}

async function saveLog() {
  if (!report.value) return
  try {
    if (editingLogId.value) {
      await updateLog(editingLogId.value, { category: logDraft.category, content: logDraft.content })
    } else {
      await createLog(logDraft.project_id, { daily_report_id: report.value.id, category: logDraft.category, content: logDraft.content })
    }
    report.value = await todayReport()
    logDialog.value = false
  } catch (err) {
    showApiError(err, t('dailyReport.saveLogFailed'))
  }
}

async function confirmLog(id: string) {
  try {
    await updateLog(id, { content_status: 'confirmed' })
    report.value = await todayReport()
    ElMessage.success(t('dailyReport.logConfirmed'))
  } catch (err) {
    showApiError(err, t('dailyReport.confirmLogFailed'))
  }
}

async function submit(force: boolean) {
  if (!report.value) return
  const result = await submitReport(report.value.id, force)
  report.value = result.report
  if (result.warnings.length > 0 && result.blocked) {
    warnings.value = result.warnings as typeof warnings.value
    warningDialog.value = true
    return
  }
  warningDialog.value = false
  ElMessage.success(t('dailyReport.submitted'))
}
</script>

<style scoped>
.panel {
  align-content: start;
  display: grid;
  gap: 14px;
}

.warning-list {
  display: grid;
  gap: 10px;
}

.toolbar-actions {
  display: flex;
  gap: 10px;
}

.ai-row {
  display: flex;
  justify-content: flex-end;
}

.file-list {
  display: grid;
  gap: 8px;
}

.file-item {
  align-items: center;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  display: flex;
  gap: 8px;
  padding: 8px 12px;
}

.log-card-list {
  display: grid;
  gap: var(--space-3);
}

.log-card {
  background: var(--surface-2);
  border-radius: var(--radius-sm);
  display: grid;
  gap: 8px;
  padding: 12px 14px;
}

.log-card-head {
  align-items: center;
  display: flex;
  gap: 8px;
}

.log-card-time {
  color: var(--text-3);
  font-size: 12px;
  margin-right: auto;
}

.log-card-meta {
  color: var(--brand-600);
  font-size: 13px;
  font-weight: 600;
}

.log-card-content {
  color: var(--text-2);
  font-size: 14px;
  line-height: 1.6;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.log-card-actions {
  display: flex;
  gap: 10px;
  margin-top: 2px;
}

.muted {
  color: var(--text-3);
}
</style>
