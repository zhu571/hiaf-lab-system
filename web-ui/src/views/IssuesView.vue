<template>
  <div class="page">
    <div class="toolbar">
      <h2>问题看板</h2>
      <el-select v-model="selectedProjectId" class="project-select" placeholder="选择项目">
        <el-option v-for="p in projects.projects" :key="p.id" :label="p.short_name || p.name" :value="p.id" />
      </el-select>
      <el-button v-if="canCreate" type="primary" @click="createDialog = true">新建问题</el-button>
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
        <p v-if="grouped[status].length === 0" class="empty-hint">暂无问题</p>
      </section>
    </div>
    <el-drawer v-model="drawer" size="420" title="问题详情">
      <div v-if="selected" class="grid">
        <StatusBadge :value="selected.status" />
        <h3>{{ selected.title }}</h3>
        <p class="issue-desc">{{ selected.description }}</p>
        <el-select v-model="targetStatus">
          <el-option v-for="s in statuses" :key="s" :label="s" :value="s" />
        </el-select>
        <el-input v-model="reason" placeholder="状态变更理由" />
        <el-button @click="transition">更新状态</el-button>
        <CommentSection :comments="selected.comments || []" @submit="comment" />
      </div>
    </el-drawer>
    <el-dialog v-model="createDialog" title="新建问题" width="560">
      <el-form label-position="top">
        <el-form-item label="标题"><el-input v-model="draft.title" /></el-form-item>
        <el-form-item label="严重程度"><el-select v-model="draft.severity"><el-option v-for="s in severities" :key="s" :label="s" :value="s" /></el-select></el-form-item>
        <el-form-item label="描述"><el-input v-model="draft.description" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDialog = false">取消</el-button>
        <el-button type="primary" @click="create">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import StatusBadge from '../components/StatusBadge.vue'
import CommentSection from '../components/CommentSection.vue'
import { addIssueComment, createIssue, getIssue, listIssues, transitionIssue, type Issue } from '../api/issues'
import { useAuthStore } from '../stores/auth'
import { useProjectStore } from '../stores/projects'

const route = useRoute()
const auth = useAuthStore()
const router = useRouter()
const projects = useProjectStore()
const canCreate = computed(() => auth.user?.role !== 'viewer')
const issues = ref<Issue[]>([])
const selected = ref<Issue | null>(null)
const drawer = ref(false)
const createDialog = ref(false)
const targetStatus = ref('open')
const reason = ref('')
const statuses = ['open', 'in_progress', 'resolved', 'closed']
const severities = ['low', 'medium', 'high', 'critical']
const draft = reactive({ title: '', severity: 'medium', description: '' })

// projectId 的唯一事实来源是路由参数（由 ProjectLayout 保证存在）
const projectId = computed(() => String(route.params.id || projects.current?.id || ''))
const selectedProjectId = computed({
  get: () => projectId.value,
  set: (id: string) => switchProject(id)
})
const grouped = computed(() => Object.fromEntries(statuses.map((s) => [s, issues.value.filter((item) => item.status === s)])) as Record<string, Issue[]>)

onMounted(load)
watch(projectId, load)

async function load() {
  if (!projectId.value) return
  if (projectId.value !== projects.currentId) projects.select(projectId.value)
  try {
    const data = await listIssues(projectId.value, { per_page: 100 })
    // 空列表时后端返回 items: null，直接赋值会让 grouped 计算属性 filter 崩溃
    issues.value = data.items ?? []
  } catch (err) {
    issues.value = []
    ElMessage.error(err instanceof Error ? err.message : '问题加载失败')
  }
}
}

function switchProject(id: string) {
  if (!id || id === projectId.value) return
  projects.select(id)
  router.replace({ path: `/projects/${id}/issues` })
}

function switchProject(id: string) {
  if (!id || id === projectId.value) return
  projects.select(id)
  router.replace({ path: `/projects/${id}/issues` })
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
    ElMessage.success('状态已更新')
    await load()
    await open(id)
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '状态更新失败')
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
.project-select {
  max-width: 240px;
}

.board {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
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

.issue-desc {
  color: var(--text-2);
  white-space: pre-wrap;
}

@media (max-width: 768px) {
  .board {
    grid-template-columns: 1fr;
  }
}
</style>
