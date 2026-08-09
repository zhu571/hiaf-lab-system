<template>
  <div class="page">
    <div class="toolbar">
      <h2>{{ t('issues.title') }}</h2>
      <el-button type="primary" @click="createDialog = true">{{ t('issues.create') }}</el-button>
    </div>
    <div class="board">
      <section v-for="status in statuses" :key="status" class="panel column" :data-status="status">
        <div class="column-head">
          <h3><span class="dot" />{{ status }}</h3>
          <span class="count">{{ grouped[status].length }}</span>
        </div>
        <button
          v-for="issue in grouped[status]"
          :key="issue.id"
          class="issue-card"
          :data-severity="issue.severity"
          @click="open(issue.id)"
        >
          <strong>{{ issue.title }}</strong>
          <span class="severity"><i class="sev-dot" />{{ issue.severity }}</span>
          <el-tag v-if="issue.ai_generated" class="ai-tag" size="small" type="warning" effect="light">AI</el-tag>
        </button>
        <p v-if="grouped[status].length === 0" class="empty-hint">{{ t('issues.empty') }}</p>
      </section>
    </div>
    <el-pagination
      v-model:current-page="page"
      v-model:page-size="perPage"
      class="pager"
      layout="total, sizes, prev, pager, next"
      :page-sizes="[20, 50, 100]"
      :total="total"
      @current-change="load"
      @size-change="onSizeChange"
    />
    <el-drawer v-model="drawer" size="420" :title="t('issues.detail')">
      <div v-if="selected" class="grid">
        <StatusBadge :value="selected.status" />
        <h3>{{ selected.title }}</h3>
        <MarkdownView :source="selected.description" />
        <el-select v-model="targetStatus">
          <el-option v-for="s in statuses" :key="s" :label="s" :value="s" />
        </el-select>
        <el-input v-model="reason" :placeholder="t('issues.reasonPlaceholder')" />
        <el-button @click="transition">{{ t('issues.updateStatus') }}</el-button>
        <CommentSection :comments="selected.comments || []" @submit="comment" />
      </div>
    </el-drawer>
    <el-dialog v-model="createDialog" :title="t('issues.create')" width="560">
      <el-form label-position="top">
        <el-form-item :label="t('issues.fieldTitle')"><el-input v-model="draft.title" /></el-form-item>
        <el-form-item :label="t('issues.severity')"><el-select v-model="draft.severity"><el-option v-for="s in severities" :key="s" :label="s" :value="s" /></el-select></el-form-item>
        <el-form-item :label="t('issues.description')"><el-input v-model="draft.description" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="create">{{ t('issues.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import StatusBadge from '../components/StatusBadge.vue'
import CommentSection from '../components/CommentSection.vue'
import MarkdownView from '../components/MarkdownView.vue'
import { addIssueComment, createIssue, getIssue, listIssues, transitionIssue, type Issue } from '../api/issues'
import { useProjectStore } from '../stores/project'

const route = useRoute()
const router = useRouter()
const projects = useProjectStore()
const { t } = useI18n()
const issues = ref<Issue[]>([])
const selected = ref<Issue | null>(null)
const drawer = ref(false)
const createDialog = ref(false)
const targetStatus = ref('open')
const reason = ref('')
const page = ref(1)
const perPage = ref(20)
const total = ref(0)
const statuses = ['open', 'in_progress', 'resolved', 'closed']
const severities = ['low', 'medium', 'high', 'critical']
const draft = reactive({ title: '', severity: 'medium', description: '' })

const projectId = computed(() => String(route.params.id || projects.current?.id || ''))
const grouped = computed(() => Object.fromEntries(statuses.map((s) => [s, issues.value.filter((item) => item.status === s)])) as Record<string, Issue[]>)

onMounted(load)
watch(projectId, () => {
  page.value = 1
  load()
})

function onSizeChange() {
  page.value = 1
  load()
}

async function load() {
  await projects.load()
  if (!projectId.value) return
  const data = await listIssues(projectId.value, { page: page.value, per_page: perPage.value })
  issues.value = data.items ?? []
  total.value = data.total ?? 0
}

function switchProject(id: string) {
  if (!id || id === projectId.value) return
  projects.select(id)
  router.replace({ path: `/projects/${id}/issues` })
}

async function open(id: string) {
  selected.value = await getIssue(id)
  targetStatus.value = selected.value.status
  drawer.value = true
}

async function transition() {
  if (!selected.value) return
  const id = selected.value.id
  try {
    await transitionIssue(id, targetStatus.value, reason.value)
    reason.value = ''
    ElMessage.success(t('issues.statusUpdated'))
    await load()
    await open(id)
  } catch (err) {
    showApiError(err, t('issues.statusUpdateFailed'))
  }
}

async function comment(content: string) {
  if (!selected.value) return
  await addIssueComment(selected.value.id, content)
  await open(selected.value.id)
}

async function create() {
  await createIssue(projectId.value, draft)
  createDialog.value = false
  await load()
}
</script>

<style scoped>
.board {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.pager {
  justify-content: flex-end;
  margin-top: 14px;
}

.column {
  align-content: start;
  background: var(--surface-2);
  display: grid;
  gap: 12px;
}

.column-head {
  align-items: center;
  display: flex;
  justify-content: space-between;
}

.column-head h3 {
  align-items: center;
  display: flex;
  font-size: 14px;
  gap: 8px;
  letter-spacing: 0.01em;
}

.dot {
  background: var(--text-3);
  border-radius: 50%;
  height: 8px;
  width: 8px;
}

[data-status='open'] .dot {
  background: var(--warn);
}

[data-status='in_progress'] .dot {
  background: var(--brand-500);
}

[data-status='resolved'] .dot {
  background: var(--ok);
}

[data-status='closed'] .dot {
  background: #9099a5;
}

.count {
  background: #fff;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--text-3);
  font-size: 12px;
  font-weight: 600;
  min-width: 26px;
  padding: 0 8px;
  text-align: center;
}

.issue-card {
  background: #fff;
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow-sm);
  cursor: pointer;
  display: grid;
  gap: 8px;
  padding: 12px 14px;
  text-align: left;
  transition:
    border-color 0.15s ease,
    box-shadow 0.15s ease,
    transform 0.15s ease;
}

.issue-card:hover {
  border-color: var(--brand-100);
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
}

.issue-card strong {
  color: var(--text-1);
  font-size: 14px;
  line-height: 1.4;
}

.severity {
  align-items: center;
  color: var(--text-3);
  display: inline-flex;
  font-size: 12px;
  gap: 6px;
}

.ai-tag {
  justify-self: start;
}

.sev-dot {
  border-radius: 50%;
  display: inline-block;
  height: 6px;
  width: 6px;
}

[data-severity='low'] .sev-dot {
  background: #8ba3b8;
}

[data-severity='medium'] .sev-dot {
  background: var(--brand-500);
}

[data-severity='high'] .sev-dot {
  background: var(--warn);
}

[data-severity='critical'] .sev-dot {
  background: var(--danger);
}

.empty-hint {
  border: 1px dashed var(--border-strong);
  border-radius: 10px;
  color: var(--text-3);
  font-size: 12px;
  padding: 16px 0;
  text-align: center;
}

@media (max-width: 768px) {
  .board {
    grid-template-columns: 1fr;
  }
}
</style>
