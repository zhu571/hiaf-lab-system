<template>
  <section class="panel dashboard">
    <div v-if="store.current" class="grid">
      <div class="toolbar">
        <div class="dash-title">
          <h2>{{ store.current.name }}</h2>
          <p class="muted">{{ store.current.description || '暂无说明' }}</p>
        </div>
      </div>
      <div class="stage-panel">
        <div class="stage-flow">
          <template v-for="(stage, index) in STAGES" :key="stage.key">
            <div class="stage-node" :data-state="nodeState(index)">
              <span class="stage-badge" aria-hidden="true">{{ nodeState(index) === 'done' ? '✓' : stage.icon }}</span>
              <span class="stage-name">{{ stage.label }}</span>
              <el-button
                v-if="index === currentIndex && backAction"
                class="stage-back"
                :type="backAction.type"
                size="small"
                round
                plain
                @click="openBackConfirm"
              >
                <span class="back-arrow" aria-hidden="true">←</span>
                {{ backAction.label }}
              </el-button>
              <el-button
                v-if="index === currentIndex && nextAction"
                class="stage-next"
                type="primary"
                size="small"
                round
                @click="openConfirm"
              >
                {{ nextAction.label }}
                <span class="next-arrow" aria-hidden="true">→</span>
              </el-button>
            </div>
            <span v-if="index < STAGES.length - 1" class="stage-arrow" :data-done="index < currentIndex" aria-hidden="true">→</span>
          </template>
        </div>
        <div class="stage-desc">
          <strong class="stage-desc-title">{{ currentStage.icon }} 当前阶段：{{ currentStage.label }}</strong>
          <p v-for="item in stageDesc.can" :key="'can-' + item" class="desc-line">✅ {{ item }}</p>
          <p v-for="item in stageDesc.cannot" :key="'no-' + item" class="desc-line">❌ {{ item }}</p>
        </div>
      </div>
      <div class="metric-grid">
        <div class="metric"><strong>{{ members.length }}</strong><span>成员</span></div>
        <div class="metric"><strong>{{ issueTotal }}</strong><span>问题</span></div>
        <div class="metric"><strong>{{ store.current.log_count || logs.length }}</strong><span>日志</span></div>
      </div>
      <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" />
      <div v-loading="loading" class="overview-grid">
        <section class="overview-card">
          <div class="toolbar overview-head">
            <h3>项目成员</h3>
            <el-button v-if="auth.isAdmin" link type="primary" @click="go('/admin/users')">用户管理</el-button>
          </div>
          <div v-if="members.length" class="member-list">
            <div v-for="member in members" :key="member.user_id" class="member-row">
              <span>{{ member.username || member.user_id }}</span>
              <el-tag size="small" effect="plain">{{ roleLabel(member.role) }}</el-tag>
            </div>
          </div>
          <el-empty v-else :image-size="52" description="暂无成员" />
        </section>
        <section class="overview-card timeline-card">
          <div class="toolbar overview-head">
            <h3>最近日志</h3>
            <el-button link type="primary" @click="go('/daily-report')">新建日志</el-button>
          </div>
          <div v-if="logs.length" class="timeline-list">
            <article v-for="log in logs" :key="log.id" class="timeline-item">
              <div class="timeline-meta">
                <el-tag size="small" effect="plain">{{ log.category }}</el-tag>
                <time>{{ formatTime(log.occurred_at) }}</time>
              </div>
              <p>{{ log.content }}</p>
            </article>
          </div>
          <el-empty v-else :image-size="52" description="暂无日志" />
        </section>
        <section class="overview-card">
          <div class="toolbar overview-head">
            <h3>最近问题</h3>
            <el-button link type="primary" @click="go('/projects/' + store.current.id + '/issues')">新建问题</el-button>
          </div>
          <div v-if="issues.length" class="issue-list">
            <div v-for="issue in issues" :key="issue.id" class="issue-row">
              <span>{{ issue.title }}</span>
              <el-tag size="small" effect="dark" class="severity-tag" :data-severity="issue.severity">{{ severityLabel(issue.severity) }}</el-tag>
            </div>
          </div>
          <el-empty v-else :image-size="52" description="暂无问题" />
        </section>
      </div>
    </div>
    <el-empty v-else description="暂无项目" />
    <el-dialog v-model="confirmVisible" :title="confirmTitle" width="min(440px, 92vw)">
      <p v-if="pendingNext" class="confirm-text">切换后项目将进入「{{ pendingNext.target.label }}」阶段。</p>
      <el-alert
        v-if="pendingNext?.action === 'complete' && (unresolvedIssues ?? 0) > 0"
        class="confirm-alert"
        type="warning"
        show-icon
        :closable="false"
        :title="`项目仍有 ${unresolvedIssues} 个未解决 Issue`"
        description="完成后仍可继续关闭 Issue、发布经验和生成最终报告。"
      />
      <template #footer>
        <el-button @click="confirmVisible = false">取消</el-button>
        <el-button type="primary" :loading="transitioning" @click="confirmTransition">确认切换</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { showApiError } from '../composables/useNotify'
import { useProjectStore } from '../stores/project'
import { useAuthStore } from '../stores/auth'
import { getMembers, transitionProject, type ProjectMember } from '../api/projects'
import { listProjectLogs, type LogItem } from '../api/logs'
import { listProjectIssues, type Issue } from '../api/issues'

const router = useRouter()
const store = useProjectStore()
const auth = useAuthStore()

const members = ref<ProjectMember[]>([])
const logs = ref<LogItem[]>([])
const issues = ref<Issue[]>([])
const issueTotal = ref(0)
const loading = ref(false)
const loadError = ref('')
const projectId = computed(() => store.current?.id || '')

watch(projectId, async (id) => {
  members.value = []
  logs.value = []
  issues.value = []
  issueTotal.value = 0
  loadError.value = ''
  if (!id) return
  loading.value = true
  try {
    // 三个区块独立加载：任一接口失败只显示错误提示，不影响其他区块渲染
    const [memberRes, logRes, issueRes] = await Promise.allSettled([
      getMembers(id),
      listProjectLogs(id, { per_page: 5 }),
      listProjectIssues(id, { per_page: 5, sort: 'created', order: 'desc' })
    ])
    if (id !== projectId.value) return
    if (memberRes.status === 'fulfilled') {
      const data = memberRes.value
      // members API 返回裸数组，兜底兼容分页对象格式
      members.value = Array.isArray(data) ? data : (data as { items?: ProjectMember[] }).items || []
    }
    // 空列表时后端返回 items: null，统一兜底为空数组
    if (logRes.status === 'fulfilled') logs.value = logRes.value.items || []
    if (issueRes.status === 'fulfilled') {
      issues.value = issueRes.value.items || []
      issueTotal.value = issueRes.value.total ?? 0
    }
    const failed = [memberRes, logRes, issueRes].find((r) => r.status === 'rejected')
    if (failed && failed.status === 'rejected') {
      loadError.value = failed.reason instanceof Error ? failed.reason.message : '项目概览加载失败'
    }
  } finally {
    if (id === projectId.value) loading.value = false
  }
}, { immediate: true })

type Stage = { key: string; label: string; icon: string }

const STAGES: Stage[] = [
  { key: 'draft', label: '筹备', icon: '📝' },
  { key: 'active', label: '进行中', icon: '🔬' },
  { key: 'completed', label: '已完成', icon: '✅' },
  { key: 'archived', label: '归档', icon: '📦' }
]

// 与 go-server projects.targetStatus 的合法流转一一对应
const NEXT_ACTIONS: Record<string, { action: string; label: string; target: string }> = {
  draft: { action: 'activate', label: '开始实验', target: 'active' },
  active: { action: 'complete', label: '标记完成', target: 'completed' },
  completed: { action: 'archive', label: '归档项目', target: 'archived' }
}

// 进度倒退操作，仅 admin 可见/可调（后端二次校验）
const BACK_ACTIONS: Record<string, { action: string; label: string; target: string; type: 'info' | 'warning' }> = {
  active: { action: 'deactivate', label: '退回筹备', target: 'draft', type: 'info' },
  completed: { action: 'reopen', label: '重新打开', target: 'active', type: 'warning' }
}
const BACKWARD_ACTIONS = new Set(['deactivate', 'reopen'])

// 阶段权限说明，与 docs/project-design.md 第 3 节保持一致
const STAGE_DESC: Record<string, { can: string[]; cannot: string[] }> = {
  draft: {
    can: ['完善项目信息、成员和配置', '创建实验计划', 'owner 可记录少量筹备日志'],
    cannot: ['新增正式日志、Issue 和测试数据']
  },
  active: {
    can: ['新增日志、Issue 和测试数据', '提交经验候选、推进实验计划', '有导出权限时可导出报告'],
    cannot: []
  },
  completed: {
    can: ['补录分析、总结、文档类整理日志', '关闭 Issue、发布经验、生成最终报告'],
    cannot: ['新增普通日志和测试数据']
  },
  archived: {
    can: ['只读查询历史数据', 'owner/admin 可导出归档资料'],
    cannot: ['新增或修改日志、Issue、测试数据']
  }
}

const currentIndex = computed(() => {
  const index = STAGES.findIndex((stage) => stage.key === store.current?.status)
  return index >= 0 ? index : 0
})
const currentStage = computed(() => STAGES[currentIndex.value])
const stageDesc = computed(() => STAGE_DESC[currentStage.value.key])
const nextAction = computed(() => (store.current ? NEXT_ACTIONS[store.current.status] : undefined))
const backAction = computed(() => (store.current && auth.isAdmin ? BACK_ACTIONS[store.current.status] : undefined))
const nodeState = (index: number) => (index < currentIndex.value ? 'done' : index === currentIndex.value ? 'current' : 'future')

const confirmVisible = ref(false)
const transitioning = ref(false)
const unresolvedIssues = ref<number | null>(null)
const pendingNext = ref<{ action: string; target: Stage; from: Stage } | null>(null)
const confirmTitle = computed(() => {
  const pending = pendingNext.value
  if (!pending) return '确认切换项目状态'
  const verb = BACKWARD_ACTIONS.has(pending.action) ? '退回到' : '切换到'
  return `确认将项目从「${pending.from.label}」${verb}「${pending.target.label}」？`
})

function openConfirm() {
  openTransition(nextAction.value)
}

function openBackConfirm() {
  openTransition(backAction.value)
}

function openTransition(action?: { action: string; target: string }) {
  if (!store.current || !action) return
  const target = STAGES.find((stage) => stage.key === action.target)
  if (!target) return
  pendingNext.value = { action: action.action, target, from: currentStage.value }
  unresolvedIssues.value = null
  confirmVisible.value = true
  // 标记完成前拉取未解决 Issue 数（open + in_progress），用于对话框警告
  if (action.action === 'complete') void loadUnresolvedIssues()
}

async function loadUnresolvedIssues() {
  const id = store.current?.id
  if (!id) return
  try {
    const [openRes, progressRes] = await Promise.all([
      listProjectIssues(id, { status: 'open', per_page: 1 }),
      listProjectIssues(id, { status: 'in_progress', per_page: 1 })
    ])
    if (id !== projectId.value || !confirmVisible.value) return
    unresolvedIssues.value = (openRes.total || 0) + (progressRes.total || 0)
  } catch {
    // 拉取失败则不显示数量警告，流转结果仍由后端二次校验
  }
}

async function confirmTransition() {
  const pending = pendingNext.value
  if (!store.current || !pending) return
  transitioning.value = true
  try {
    const payload: { action: string; ignore_warnings?: boolean } = { action: pending.action }
    // 对话框已展示未解决 Issue 警告并由用户确认，跳转时跳过后端重复告警
    if (pending.action === 'complete') payload.ignore_warnings = true
    await transitionProject(store.current.id, payload)
    ElMessage.success(`项目已切换到「${pending.target.label}」`)
    confirmVisible.value = false
    await store.load()
  } catch (err) {
    showApiError(err, '项目状态切换失败')
  } finally {
    transitioning.value = false
  }
}

const roleLabel = (role: string) => ({ owner: '负责人', maintainer: '维护者', member: '成员', viewer: '访客' })[role] || role
const severityLabel = (severity: string) => ({ low: '低', medium: '中', high: '高', critical: '严重' })[severity] || severity
const formatTime = (value: string) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
const go = (path: string) => router.push(path)
</script>

<style scoped>
.dashboard {
  align-content: start;
  display: grid;
  min-height: 320px;
}

.dash-title {
  display: grid;
  gap: 6px;
}

.stage-panel {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  display: grid;
  gap: 14px;
  padding: 16px 18px;
}

.stage-flow {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px 6px;
}

.stage-node {
  align-items: center;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: 999px;
  color: var(--text-3);
  display: flex;
  gap: 8px;
  min-height: 38px;
  padding: 6px 14px;
}

.stage-badge {
  font-size: 15px;
  line-height: 1;
}

.stage-name {
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
}

.stage-node[data-state='done'] {
  background: #eef6f0;
  border-color: var(--ok);
  color: var(--ok);
}

.stage-node[data-state='done'] .stage-badge {
  align-items: center;
  background: var(--ok);
  border-radius: 50%;
  color: #fff;
  display: inline-flex;
  font-size: 11px;
  height: 18px;
  justify-content: center;
  width: 18px;
}

.stage-node[data-state='current'] {
  background: var(--brand-050);
  border-color: var(--brand-600);
  box-shadow: 0 0 0 3px var(--brand-100);
  color: var(--brand-700);
}

.stage-back {
  margin-left: 4px;
}

.back-arrow {
  margin-right: 2px;
}

.stage-next {
  margin-left: 4px;
}

.next-arrow {
  margin-left: 2px;
}

.stage-arrow {
  color: var(--border-strong);
  font-size: 16px;
}

.stage-arrow[data-done='true'] {
  color: var(--ok);
}

.stage-desc {
  border-top: 1px dashed var(--border);
  display: grid;
  gap: 4px;
  padding-top: 12px;
}

.stage-desc-title {
  color: var(--text-1);
  font-size: 13px;
}

.desc-line {
  color: var(--text-2);
  font-size: 13px;
}

.confirm-text {
  color: var(--text-2);
}

.confirm-alert {
  margin-top: 12px;
}

@media (max-width: 768px) {
  .stage-node {
    padding: 5px 10px;
  }
}

.metric-grid {
  display: grid;
  gap: 14px;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
}

.metric {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  display: grid;
  gap: 2px;
  overflow: hidden;
  padding: 18px 18px 18px 21px;
  position: relative;
  transition:
    box-shadow 0.15s ease,
    transform 0.15s ease;
}

.metric::before {
  background: linear-gradient(180deg, var(--brand-500), var(--brand-700));
  border-radius: 3px;
  bottom: 14px;
  content: '';
  left: 0;
  position: absolute;
  top: 14px;
  width: 3px;
}

.metric:hover {
  box-shadow: var(--shadow-md);
  transform: translateY(-2px);
}

.metric strong {
  color: var(--brand-700);
  font-size: 30px;
  font-weight: 700;
  line-height: 1.1;
}

.metric span {
  color: var(--text-3);
  font-size: 13px;
}

.overview-grid {
  display: grid;
  gap: 14px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  min-height: 180px;
}

.overview-card {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  display: grid;
  gap: 12px;
  padding: 16px;
}

.timeline-card {
  grid-row: span 2;
}

.overview-head h3 {
  font-size: 15px;
}

.member-list,
.issue-list,
.timeline-list {
  align-content: start;
  display: grid;
}

.member-row,
.issue-row {
  align-items: center;
  border-top: 1px solid var(--border);
  display: flex;
  gap: 12px;
  justify-content: space-between;
  min-width: 0;
  padding: 10px 0;
}

.member-row:first-child,
.issue-row:first-child {
  border-top: 0;
}

.member-row > span,
.issue-row > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.timeline-item {
  border-left: 2px solid var(--brand-100);
  display: grid;
  gap: 6px;
  padding: 0 0 16px 14px;
}

.timeline-item:last-child {
  padding-bottom: 0;
}

.timeline-meta {
  align-items: center;
  display: flex;
  gap: 10px;
  justify-content: space-between;
}

.timeline-meta time {
  color: var(--text-3);
  font-size: 12px;
}

.timeline-item p {
  color: var(--text-2);
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.severity-tag[data-severity='low'] { --el-tag-bg-color: #8ba3b8; --el-tag-border-color: #8ba3b8; }
.severity-tag[data-severity='medium'] { --el-tag-bg-color: var(--warn); --el-tag-border-color: var(--warn); }
.severity-tag[data-severity='high'] { --el-tag-bg-color: #df7344; --el-tag-border-color: #df7344; }
.severity-tag[data-severity='critical'] { --el-tag-bg-color: var(--danger); --el-tag-border-color: var(--danger); }

@media (max-width: 768px) {
  .overview-grid {
    grid-template-columns: 1fr;
  }

  .timeline-card {
    grid-row: auto;
  }
}
</style>
