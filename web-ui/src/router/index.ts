import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

// 路由级代码分割：所有页面组件懒加载，首屏只下载当前路由需要的 chunk
const LoginView = () => import('../views/LoginView.vue')
const ProjectsView = () => import('../views/ProjectsView.vue')
const DailyReportView = () => import('../views/DailyReportView.vue')
const IssuesView = () => import('../views/IssuesView.vue')
const ExperiencesView = () => import('../views/ExperiencesView.vue')
const AuditView = () => import('../views/AuditView.vue')
const SettingsView = () => import('../views/SettingsView.vue')
const DailyHistoryView = () => import('../views/DailyHistoryView.vue')
const AdminUsersView = () => import('../views/AdminUsersView.vue')
const AgentCandidatesView = () => import('../views/AgentCandidatesView.vue')
const RunListView = () => import('../views/RunListView.vue')
const RunDetailView = () => import('../views/RunDetailView.vue')
const TestDataView = () => import('../views/TestDataView.vue')
const RFMatchingView = () => import('../views/RFMatchingView.vue')
const AssemblyView = () => import('../views/AssemblyView.vue')
const AttachmentView = () => import('../views/AttachmentView.vue')
const InstrumentMeasureView = () => import('../views/InstrumentMeasureView.vue')
const GasControlView = () => import('../views/GasControlView.vue')
const SensorsView = () => import('../views/SensorsView.vue')
const DailyReportDetailView = () => import('../views/DailyReportDetailView.vue')
const DailyReportShell = () => import('../components/DailyReportShell.vue')
const ProjectLayout = () => import('../components/ProjectLayout.vue')
const ProjectDashboard = () => import('../components/ProjectDashboard.vue')

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/projects' },
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/projects', component: ProjectsView },
    {
      path: '/daily-report',
      component: DailyReportShell,
      children: [
        { path: '', component: DailyReportView },
        { path: 'history', component: DailyHistoryView }
      ]
    },
    {
      path: '/projects/:id',
      component: ProjectLayout,
      children: [
        { path: '', component: ProjectDashboard },
        { path: 'issues', component: IssuesView },
        { path: 'experiment-runs', component: RunListView },
        { path: 'test-data', component: TestDataView },
        { path: 'rf-matching', component: RFMatchingView },
        { path: 'assembly', component: AssemblyView }
      ]
    },
    { path: '/experiment-runs/:id', component: RunDetailView },
    { path: '/attachments', component: AttachmentView },
    { path: '/instrument-measure', component: InstrumentMeasureView },
    { path: '/gas-control', component: GasControlView },
    { path: '/sensors', component: SensorsView },
    { path: '/experiences', component: ExperiencesView },
    { path: '/audit', component: AuditView },
    { path: '/settings', component: SettingsView },
    { path: '/daily-reports/:id', component: DailyReportDetailView },
    { path: '/admin/users', component: AdminUsersView, meta: { admin: true } },
    { path: '/agent-candidates', component: AgentCandidatesView, meta: { reviewer: true } },
    // 兼容重定向：保留旧链接不 404
    { path: '/issues', redirect: '/projects' },
    { path: '/daily-reports', redirect: '/daily-report/history' },
    { path: '/runs/:id', redirect: '/experiment-runs/:id' },
    { path: '/projects/:id/runs', redirect: '/projects/:id/experiment-runs' },
    { path: '/instruments', redirect: '/instrument-measure' }
  ]
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!to.meta.public && !auth.ready) {
    try {
      await auth.loadMe()
    } catch {
      return '/login'
    }
  }
  if (!to.meta.public && !auth.user) return '/login'
  if (to.meta.admin && !auth.isAdmin) return '/projects'
  if (to.meta.reviewer && !auth.canReviewAgent) return '/projects'
  if (to.path !== '/settings' && auth.user?.must_change_password) return '/settings'
})

export default router
