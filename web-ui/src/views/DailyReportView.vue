<template>
  <div class="page">
    <div class="toolbar">
      <h2>日报录入</h2>
      <el-button v-if="canSubmit" type="primary" :disabled="!report" @click="submit(false)">提交日报</el-button>
    </div>
    <section class="panel editor-panel">
      <div class="toolbar">
        <h3>今日记录</h3>
        <div class="toolbar-actions">
          <el-upload :auto-upload="false" :show-file-list="false" :on-change="onFileSelect" accept="image/*,.pdf">
            <el-button>📎 添加附件</el-button>
          </el-upload>
          <el-button @click="saveRaw">保存原文</el-button>
        </div>
      </div>
      <el-input v-model="rawText" type="textarea" :rows="8" placeholder="记录今天的实验、装配、测试、问题和结论" />
    </section>
    <section class="panel">
      <div class="toolbar">
        <h3>项目化日志</h3>
        <el-button @click="openAddLog">添加日志</el-button>
      </div>
      <el-table :data="report?.logs || []">
        <el-table-column prop="category" label="分类" width="140" />
        <el-table-column prop="content" label="内容" />
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <StatusBadge :value="row.content_status" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150">
          <template #default="{ row }">
            <template v-if="row.content_status === 'draft'">
              <el-button link type="primary" @click="openEditLog(row)">编辑</el-button>
              <el-button link type="success" @click="confirmLog(row.id)">确认</el-button>
            </template>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无日志，点击右上角添加" />
        </template>
      </el-table>
    </section>
    <el-dialog v-model="logDialog" :title="editingLogId ? '编辑日志' : '添加日志'" width="560">
      <el-form label-position="top">
        <el-form-item v-if="!editingLogId" label="项目"><el-select v-model="logDraft.project_id"><el-option v-for="p in projects.projects" :key="p.id" :label="p.name" :value="p.id" /></el-select></el-form-item>
        <el-form-item label="分类"><el-input v-model="logDraft.category" /></el-form-item>
        <el-form-item label="内容"><el-input v-model="logDraft.content" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="logDialog = false">取消</el-button>
        <el-button type="primary" @click="saveLog">保存</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="warningDialog" title="提交前请确认" width="520">
      <div class="warning-list">
        <el-alert v-for="warning in warnings" :key="warning.code + warning.log_id" :title="warning.message" type="warning" show-icon :closable="false" />
      </div>
      <template #footer>
        <el-button @click="warningDialog = false">返回修改</el-button>
        <el-button type="warning" @click="submit(true)">忽略并提交</el-button>
      </template>
    </el-dialog>

    <section v-if="pendingFiles.length" class="panel">
      <h3>附件 ({{ pendingFiles.length }})</h3>
      <div class="file-list">
        <div v-for="f in pendingFiles" :key="f.name" class="file-item">
          <el-icon><Paperclip /></el-icon>
          <span>{{ f.name }}</span>
          <span class="muted">({{ formatSize(f.size) }})</span>
          <span v-if="f.uploaded" style="color:var(--success,#67c23a)">✓</span>
          <el-button v-else size="small" @click="uploadPendingFile(f)">上传</el-button>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Paperclip } from '@element-plus/icons-vue'
import StatusBadge from '../components/StatusBadge.vue'
import { createLog, submitReport, todayReport, updateLog, updateReportRawText, type DailyReport, type LogItem } from '../api/logs'
import { useProjectStore } from '../stores/project'
import { useAuthStore } from '../stores/auth'
import { uploadAttachment } from '../api/attachments'

const projectStore = useProjectStore()
const auth = useAuthStore()
const canSubmit = computed(() => auth.user?.role !== 'viewer')
const projects = projectStore

// 附件
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

  // 如果日报已存在（已创建今日日报），立即上传
  if (report.value?.id) {
    await uploadPendingFile(entry)
  } else {
    ElMessage.info(`${file.name} 将在日报创建后自动上传`)
  }
}

async function uploadPendingFile(pf: PendingFile) {
  if (!report.value?.id) return
  try {
    await uploadAttachment(pf.file, 'daily_report', report.value.id)
    pf.uploaded = true
  } catch {
    ElMessage.warning(`${pf.name} 上传失败`)
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

onMounted(async () => {
  await projects.load()
  report.value = await todayReport()
  rawText.value = report.value.raw_text
  logDraft.project_id = projects.current?.id || ''
})

async function saveRaw() {
  if (!report.value) return
  report.value = await updateReportRawText(report.value.id, rawText.value)
  ElMessage.success('已保存')
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
    ElMessage.error(err instanceof Error ? err.message : '保存日志失败')
  }
}

async function confirmLog(id: string) {
  try {
    await updateLog(id, { content_status: 'confirmed' })
    report.value = await todayReport()
    ElMessage.success('日志已确认')
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '确认日志失败')
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
  ElMessage.success('日报已提交')
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

.file-list {
  display: grid;
  gap: 8px;
}

.file-item {
  align-items: center;
  border: 1px solid var(--border-light, #e5e7eb);
  border-radius: 6px;
  display: flex;
  gap: 8px;
  padding: 8px 12px;
}

.muted {
  color: var(--text-secondary, #9ca3af);
}
</style>
