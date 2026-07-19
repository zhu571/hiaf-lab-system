<template>
  <div class="page">
    <div class="toolbar">
      <h2>日报录入</h2>
      <el-button type="primary" :disabled="!report" @click="submit(false)">提交日报</el-button>
    </div>
    <section class="panel editor-panel">
      <div class="toolbar">
        <h3>今日记录</h3>
        <el-button @click="saveRaw">保存原文</el-button>
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
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import StatusBadge from '../components/StatusBadge.vue'
import { createLog, submitReport, todayReport, updateLog, updateReportRawText, type DailyReport, type LogItem } from '../api/logs'
import { useProjectStore } from '../stores/project'

const projects = useProjectStore()
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
</style>
