// 状态语义注册表（M4 定稿：重构方案 §3.7 = 美术方案 §3.8，两方案同一文件、同一事实源）。
// 本批（重构 S3）随 StatusBadge 泛化创建十域注册表；美术 S4 在此基础上增量补全全部域映射与三级色取值。
//
// 结构定稿：
//   StatusMeta = { tone, labelKey, graphic?, text?, soft? }
//   tone      —— Element tag type（StatusBadge 等 EP 场景取此字段）
//   labelKey  —— i18n key，一律显式字面量，禁止模板字符串拼装动态 key（keys.test.ts 静态可扫，无盲区）
//   graphic   —— 图形级 CSS 变量名（圆点/进度/图例），如 '--ok'
//   text      —— 文本级 CSS 变量名，如 '--ok-text'
//   soft      —— 浅底级 CSS 变量名，如 '--ok-soft'
// 语义→族映射总表（美术 §3.8）：success=--ok 族 / warning=--warn 族 / danger=--danger 族 /
// info=--info 族 / primary=--brand-500/-600/-050。
//
// 枚举值集唯一事实源 = 后端 Go model（逐域对照登记，2026-08-14 实测）：
//   runStatus            go-server/runs/model.go:6-10        planned/active/paused/completed/aborted
//   stepStatus           go-server/runs/model.go:98-103      planned/in_progress/paused/completed/skipped/cancelled
//                        （装配步骤同枚举 go-server/assembly/model.go:6-11）
//   issueStatus          go-server/issues/model.go:6-9       open/in_progress/resolved/closed
//   issueSeverity        go-server/issues/model.go:11-14     low/medium/high/critical
//   alertLevel           go-server/alert/model.go:19-22      info/warning/error/critical
//   instrumentState      go-server/instruments/model.go:205-208  running/rate_limited/needs_reconnect/error
//   testQuality          go-server/testdata/model.go:16-19   normal/outlier/suspect/invalid
//   todoPriority         go-server/todos/model.go:11-13      high/medium/low
//   userRole             go-server/auth/model.go:7-11        admin/maintainer/member/viewer/agent
//   projectStage         go-server/projects/model.go:6-9     draft/active/completed/archived
// 本批补登（StatusBadge 行为兼容核对所必需，美术 §3.8「现映射全部保留」）：
//   experienceStatus     go-server/experiences/model.go:6-8  candidate/published/archived
//   reportStatus         go-server/logs/model.go:6-9         draft/submitted/confirmed/locked
//   agentCandidateStatus go-server/agent/model.go:15-19      pending_review/approved/rejected/executed/execution_failed
// 日志查看优化批补登（log-view-optimization，2026-09-01）：
//   logStatus            go-server/logs/model.go:16-19       draft/confirmed/locked/voided
//   reportQuality        go-server/logs/model.go:12-14       unchecked/passed/warnings
// 美术 S4 补登（前端派生态，非后端枚举——DashboardView isOnline/gasOnline 由设备 state/snapshot 计算）：
//   onlineStatus         web-ui/src/views/DashboardView.vue  online/offline
//
// 未命中行为（M4 定稿）：label 降级显示原文（现 value.replace(/_/g,' ') 行为）+ console.warn，
// tone 落 info（R9 登记的有意视觉变更，原默认 primary）。后续后端新增枚举值时同 PR 补映射与 labelKey。

export type StatusTone = 'success' | 'warning' | 'info' | 'primary' | 'danger'

export interface StatusMeta {
  tone: StatusTone
  labelKey: string
  graphic?: string
  text?: string
  soft?: string
}

export type StatusDomain =
  | 'runStatus'
  | 'stepStatus'
  | 'issueStatus'
  | 'issueSeverity'
  | 'alertLevel'
  | 'instrumentState'
  | 'testQuality'
  | 'todoPriority'
  | 'userRole'
  | 'projectStage'
  | 'experienceStatus'
  | 'reportStatus'
  | 'agentCandidateStatus'
  | 'onlineStatus'
  | 'invitationCode'
  | 'translation'
  | 'logStatus'
  | 'reportQuality'

const FAMILY: Record<StatusTone, { graphic: string; text: string; soft: string }> = {
  success: { graphic: '--ok', text: '--ok-text', soft: '--ok-soft' },
  warning: { graphic: '--warn', text: '--warn-text', soft: '--warn-soft' },
  danger: { graphic: '--danger', text: '--danger-text', soft: '--danger-soft' },
  info: { graphic: '--info', text: '--info-text', soft: '--info-soft' },
  primary: { graphic: '--brand-500', text: '--brand-600', soft: '--brand-050' }
}

function meta(tone: StatusTone, labelKey: string): StatusMeta {
  return { tone, labelKey, ...FAMILY[tone] }
}

export const STATUS_META: Record<StatusDomain, Record<string, StatusMeta>> = {
  runStatus: {
    planned: meta('info', 'runList.runStatus.planned'),
    active: meta('success', 'runList.runStatus.active'),
    paused: meta('warning', 'runList.runStatus.paused'),
    completed: meta('success', 'runList.runStatus.completed'),
    aborted: meta('danger', 'runList.runStatus.aborted')
  },
  stepStatus: {
    planned: meta('info', 'assembly.status.planned'),
    in_progress: meta('primary', 'assembly.status.in_progress'),
    paused: meta('warning', 'assembly.status.paused'),
    completed: meta('success', 'assembly.status.completed'),
    skipped: meta('info', 'assembly.status.skipped'),
    cancelled: meta('info', 'assembly.status.cancelled')
  },
  issueStatus: {
    open: meta('warning', 'issues.status.open'),
    in_progress: meta('primary', 'issues.status.in_progress'),
    resolved: meta('success', 'issues.status.resolved'),
    closed: meta('info', 'issues.status.closed')
  },
  issueSeverity: {
    low: meta('info', 'projectDashboard.severityLow'),
    medium: meta('warning', 'projectDashboard.severityMedium'),
    high: meta('danger', 'projectDashboard.severityHigh'),
    critical: meta('danger', 'projectDashboard.severityCritical')
  },
  alertLevel: {
    info: meta('info', 'alert.levelInfo'),
    warning: meta('warning', 'alert.levelWarning'),
    error: meta('danger', 'alert.levelError'),
    critical: meta('danger', 'alert.levelCritical')
  },
  instrumentState: {
    running: meta('primary', 'instrument.stateRunning'),
    rate_limited: meta('warning', 'instrument.stateRateLimited'),
    needs_reconnect: meta('warning', 'instrument.stateNeedsReconnect'),
    error: meta('danger', 'instrument.stateError')
  },
  testQuality: {
    normal: meta('success', 'testData.qualityNormal'),
    outlier: meta('warning', 'testData.qualityOutlier'),
    suspect: meta('danger', 'testData.qualitySuspect'),
    invalid: meta('danger', 'testData.qualityInvalid')
  },
  todoPriority: {
    high: meta('danger', 'todos.priorityHigh'),
    medium: meta('warning', 'todos.priorityMedium'),
    low: meta('info', 'todos.priorityLow')
  },
  userRole: {
    admin: meta('primary', 'adminUsers.roleAdmin'),
    maintainer: meta('primary', 'adminUsers.roleMaintainer'),
    member: meta('info', 'adminUsers.roleMember'),
    viewer: meta('info', 'adminUsers.roleViewer'),
    agent: meta('warning', 'adminUsers.roleAgent')
  },
  projectStage: {
    draft: meta('warning', 'project.stages.draft'),
    active: meta('success', 'project.stages.active'),
    completed: meta('success', 'project.stages.completed'),
    archived: meta('info', 'project.stages.archived')
  },
  experienceStatus: {
    candidate: meta('warning', 'experiences.columnCandidate'),
    published: meta('success', 'experiences.published'),
    archived: meta('info', 'experiences.archived')
  },
  reportStatus: {
    draft: meta('warning', 'dailyHistory.draft'),
    submitted: meta('warning', 'dailyHistory.submitted'),
    confirmed: meta('success', 'dailyHistory.confirmed'),
    locked: meta('info', 'dailyHistory.locked')
  },
  agentCandidateStatus: {
    pending_review: meta('warning', 'agentCandidates.statusPending'),
    approved: meta('success', 'agentCandidates.statusApproved'),
    rejected: meta('danger', 'agentCandidates.statusRejected'),
    executed: meta('success', 'agentCandidates.statusExecuted'),
    execution_failed: meta('danger', 'agentCandidates.statusExecutionFailed')
  },
  onlineStatus: {
    online: meta('success', 'common.online'),
    offline: meta('info', 'common.offline')
  },
  invitationCode: { active: meta('success', 'adminUsers.invitationCodes.statusActive'), used: meta('info', 'adminUsers.invitationCodes.statusUsed'), expired: meta('warning', 'adminUsers.invitationCodes.statusExpired'), revoked: meta('danger', 'adminUsers.invitationCodes.statusRevoked') }
  ,translation: { pending: meta('warning', 'translation.pending'), failed: meta('danger', 'translation.failed'), stale: meta('warning', 'translation.stale'), missing: meta('info', 'translation.missing'), ready: meta('success', 'translation.original') },
  // 日志查看优化批补登（log-view-optimization）：logStatus/reportQuality 追加在末尾，
  // 保持 findStatusMeta 跨域扫描对 draft/confirmed/locked 等共享值的既有命中顺序不变
  logStatus: {
    draft: meta('warning', 'logStatus.draft'),
    confirmed: meta('success', 'logStatus.confirmed'),
    locked: meta('info', 'logStatus.locked'),
    voided: meta('danger', 'logStatus.voided')
  },
  reportQuality: {
    unchecked: meta('info', 'reportQuality.unchecked'),
    passed: meta('success', 'reportQuality.passed'),
    warnings: meta('warning', 'reportQuality.warnings')
  }
}

/** 按 domain 查注册表；未命中返回 undefined（调用方走未命中降级） */
export function statusMetaFor(domain: StatusDomain, value: string): StatusMeta | undefined {
  return STATUS_META[domain]?.[value]
}

/**
 * 跨域值优先查找：StatusBadge 未显式传 domain（9 个既有视图仅传 :value）时使用。
 * 各域同名值的语义族经美术 §3.8 语义→族总表对齐，跨域扫描结果一致，无歧义。
 */
export function findStatusMeta(value: string): StatusMeta | undefined {
  for (const domain of Object.values(STATUS_META)) {
    const m = domain[value]
    if (m) return m
  }
  return undefined
}
