<template>
  <section class="panel dashboard">
    <div v-if="store.current" class="grid">
      <div class="toolbar">
        <div class="dash-title">
          <h2>{{ store.current.name }}</h2>
          <p class="muted">{{ store.current.description || t('projectDashboard.noDescription') }}</p>
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
          <strong class="stage-desc-title">{{ t('projectDashboard.currentStage', { label: currentStage.label }) }}</strong>
          <p v-for="item in stageDesc.can" :key="'can-' + item" class="desc-line">✅ {{ item }}</p>
          <p v-for="item in stageDesc.cannot" :key="'no-' + item" class="desc-line">❌ {{ item }}</p>
        </div>
      </div>
      <div class="metric-grid">
        <div class="metric"><strong>{{ members.length }}</strong><span>{{ t('projectDashboard.metricMembers') }}</span></div>
        <div class="metric"><strong>{{ issueTotal }}</strong><span>{{ t('projectDashboard.metricIssues') }}</span></div>
        <div class="metric"><strong>{{ store.current.log_count || logs.length }}</strong><span>{{ t('projectDashboard.metricLogs') }}</span></div>
      </div>
      <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" />
      <div v-loading="loading" class="overview-grid">
        <section class="overview-card">
          <div class="toolbar overview-head">
            <h3>{{ t('projectDashboard.projectMembers') }}</h3>
            <el-button v-if="auth.isAdmin" link type="primary" @click="go('/admin/users')">{{ t('projectDashboard.userManagement') }}</el-button>
          </div>
          <div v-if="members.length" class="member-list">
            <div v-for="member in members" :key="member.user_id" class="member-row">
              <span>{{ member.username || member.user_id }}</span>
              <el-tag size="small" effect="plain">{{ roleLabel(member.role) }}</el-tag>
            </div>
          </div>
          <el-empty v-else :image-size="52" :description="t('projectDashboard.noMembers')" />
        </section>
        <section class="overview-card timeline-card">
          <div class="toolbar overview-head">
            <h3>{{ t('projectDashboard.recentLogs') }}</h3>
            <el-button link type="primary" @click="go('/daily-report')">{{ t('projectDashboard.newLog') }}</el-button>
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
          <el-empty v-else :image-size="52" :description="t('projectDashboard.noLogs')" />
        </section>
        <section class="overview-card">
          <div class="toolbar overview-head">
            <h3>{{ t('projectDashboard.recentIssues') }}</h3>
            <el-button link type="primary" @click="go('/projects/' + store.current.id + '/issues')">{{ t('projectDashboard.newIssue') }}</el-button>
          </div>
          <div v-if="issues.length" class="issue-list">
            <div v-for="issue in issues" :key="issue.id" class="issue-row">
              <span>{{ issue.title }}</span>
              <el-tag size="small" effect="dark" class="severity-tag" :data-severity="issue.severity">{{ severityLabel(issue.severity) }}</el-tag>
            </div>
          </div>
          <el-empty v-else :image-size="52" :description="t('projectDashboard.noIssues')" />
        </section>
      </div>
    </div>
    <el-empty v-else :image-size="52" :description="t('projectDashboard.noProject')" />
    <el-dialog v-model="confirmVisible" :title="confirmTitle" width="min(440px, 92vw)">
      <p v-if="pendingNext" class="confirm-text">{{ t('projectDashboard.confirmSwitchDesc', { label: pendingNext.target.label }) }}</p>
      <el-alert
        v-if="pendingNext?.action === 'complete' && (unresolvedIssues ?? 0) > 0"
        class="confirm-alert"
        type="warning"
        show-icon
        :closable="false"
        :title="t('projectDashboard.unresolvedWarning', { count: unresolvedIssues })"
        :description="t('projectDashboard.unresolvedDesc')"
      />
      <template #footer>
        <el-button @click="confirmVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="transitioning" @click="confirmTransition">{{ t('projectDashboard.confirmSwitch') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { showApiError } from '@/composables/useNotify'
import { useProjectStore } from '@/stores/project'
import { useAuthStore } from '@/stores/auth'
import { getMembers, transitionProject, type ProjectMember } from '@/api/projects'
import { listProjectLogs, type LogItem } from '@/api/logs'
import { listProjectIssues, type Issue } from '@/api/issues'

const router = useRouter()
const store = useProjectStore()
const auth = useAuthStore()
const { t, locale } = useI18n()

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
    // Three sections load independently: a single API failure shows an error banner without blocking other sections
    const [memberRes, logRes, issueRes] = await Promise.allSettled([
      getMembers(id),
      listProjectLogs(id, { per_page: 5 }),
      listProjectIssues(id, { per_page: 5, sort: 'created', order: 'desc' })
    ])
    if (id !== projectId.value) return
    if (memberRes.status === 'fulfilled') {
      const data = memberRes.value
      // members API returns a bare array; also handle paginated object format as fallback
      members.value = Array.isArray(data) ? data : (data as { items?: ProjectMember[] }).items || []
    }
    // Backend returns items: null for empty lists; normalize to empty array
    if (logRes.status === 'fulfilled') logs.value = logRes.value.items || []
    if (issueRes.status === 'fulfilled') {
      issues.value = issueRes.value.items || []
      issueTotal.value = issueRes.value.total ?? 0
    }
    const failed = [memberRes, logRes, issueRes].find((r) => r.status === 'rejected')
    if (failed && failed.status === 'rejected') {
      loadError.value = failed.reason instanceof Error ? failed.reason.message : t('projectDashboard.loadFailed')
    }
  } finally {
    if (id === projectId.value) loading.value = false
  }
}, { immediate: true })

type StageItem = { key: string; label: string; icon: string }

const STAGES = computed<StageItem[]>(() => [
  { key: 'draft', label: t('project.stages.draft'), icon: '📝' },
  { key: 'active', label: t('project.stages.active'), icon: '🔬' },
  { key: 'completed', label: t('project.stages.completed'), icon: '✅' },
  { key: 'archived', label: t('project.stages.archived'), icon: '📦' }
])

// Maps to valid transitions in go-server projects.targetStatus
const NEXT_ACTIONS = computed<Record<string, { action: string; label: string; target: string }>>(() => ({
  draft: { action: 'activate', label: t('projectDashboard.actionActivate'), target: 'active' },
  active: { action: 'complete', label: t('projectDashboard.actionComplete'), target: 'completed' },
  completed: { action: 'archive', label: t('projectDashboard.actionArchive'), target: 'archived' }
}))

// Backward transitions, admin-only visibility/invocation (backend re-validates)
const BACK_ACTIONS = computed<Record<string, { action: string; label: string; target: string; type: 'info' | 'warning' }>>(() => ({
  active: { action: 'deactivate', label: t('projectDashboard.actionDeactivate'), target: 'draft', type: 'info' as const },
  completed: { action: 'reopen', label: t('projectDashboard.actionReopen'), target: 'active', type: 'warning' as const }
}))
const BACKWARD_ACTIONS = new Set(['deactivate', 'reopen'])

// Stage permission descriptions, consistent with docs/project-design.md §3
const STAGE_DESC = computed<Record<string, { can: string[]; cannot: string[] }>>(() => ({
  draft: {
    can: [t('projectDashboard.descDraftCan1'), t('projectDashboard.descDraftCan2'), t('projectDashboard.descDraftCan3')],
    cannot: [t('projectDashboard.descDraftCannot1')]
  },
  active: {
    can: [t('projectDashboard.descActiveCan1'), t('projectDashboard.descActiveCan2'), t('projectDashboard.descActiveCan3')],
    cannot: []
  },
  completed: {
    can: [t('projectDashboard.descCompletedCan1'), t('projectDashboard.descCompletedCan2')],
    cannot: [t('projectDashboard.descCompletedCannot1')]
  },
  archived: {
    can: [t('projectDashboard.descArchivedCan1'), t('projectDashboard.descArchivedCan2')],
    cannot: [t('projectDashboard.descArchivedCannot1')]
  }
}))

const currentIndex = computed(() => {
  const index = STAGES.value.findIndex((stage) => stage.key === store.current?.status)
  return index >= 0 ? index : 0
})
const currentStage = computed(() => STAGES.value[currentIndex.value])
const stageDesc = computed(() => STAGE_DESC.value[currentStage.value.key])
const nextAction = computed(() => (store.current ? NEXT_ACTIONS.value[store.current.status] : undefined))
const backAction = computed(() => (store.current && auth.isAdmin ? BACK_ACTIONS.value[store.current.status] : undefined))
const nodeState = (index: number) => (index < currentIndex.value ? 'done' : index === currentIndex.value ? 'current' : 'future')

const confirmVisible = ref(false)
const transitioning = ref(false)
const unresolvedIssues = ref<number | null>(null)
const pendingNext = ref<{ action: string; target: StageItem; from: StageItem } | null>(null)
const confirmTitle = computed(() => {
  const pending = pendingNext.value
  if (!pending) return t('projectDashboard.confirmTitleDefault')
  if (BACKWARD_ACTIONS.has(pending.action)) {
    return t('projectDashboard.confirmBackTemplate', { from: pending.from.label, to: pending.target.label })
  }
  return t('projectDashboard.confirmForwardTemplate', { from: pending.from.label, to: pending.target.label })
})

function openConfirm() {
  openTransition(nextAction.value)
}

function openBackConfirm() {
  openTransition(backAction.value)
}

function openTransition(action?: { action: string; target: string }) {
  if (!store.current || !action) return
  const target = STAGES.value.find((stage) => stage.key === action.target)
  if (!target) return
  pendingNext.value = { action: action.action, target, from: currentStage.value }
  unresolvedIssues.value = null
  confirmVisible.value = true
  // Fetch unresolved issue count before marking complete, for dialog warning
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
    // If fetch fails, skip count warning; backend still validates the transition
  }
}

async function confirmTransition() {
  const pending = pendingNext.value
  if (!store.current || !pending) return
  transitioning.value = true
  try {
    const payload: { action: string; ignore_warnings?: boolean } = { action: pending.action }
    // Dialog already showed the unresolved issue warning and user confirmed; skip duplicate backend alert
    if (pending.action === 'complete') payload.ignore_warnings = true
    await transitionProject(store.current.id, payload)
    ElMessage.success(t('projectDashboard.switchedTo', { label: pending.target.label }))
    confirmVisible.value = false
    await store.load()
  } catch (err) {
    showApiError(err, t('projectDashboard.transitionFailed'))
  } finally {
    transitioning.value = false
  }
}

const roleLabel = (role: string) => {
  const map: Record<string, string> = {
    owner: t('projectDashboard.roleOwner'),
    maintainer: t('projectDashboard.roleMaintainer'),
    member: t('projectDashboard.roleMember'),
    viewer: t('projectDashboard.roleViewer')
  }
  return map[role] || role
}
const severityLabel = (severity: string) => {
  const map: Record<string, string> = {
    low: t('projectDashboard.severityLow'),
    medium: t('projectDashboard.severityMedium'),
    high: t('projectDashboard.severityHigh'),
    critical: t('projectDashboard.severityCritical')
  }
  return map[severity] || severity
}
const formatTime = (value: string) => {
  const l = locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return new Intl.DateTimeFormat(l, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
}
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
