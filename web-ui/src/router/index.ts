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
const StepTemplatesView = () => import('../views/StepTemplatesView.vue')
const TestDataView = () => import('../views/TestDataView.vue')
const RFMatchingView = () => import('../views/RFMatchingView.vue')
const AssemblyView = () => import('../views/AssemblyView.vue')
const AttachmentView = () => import('../views/AttachmentView.vue')
const InstrumentMeasureView = () => import('../views/InstrumentMeasureView.vue')
const GasControlView = () => import('../views/GasControlView.vue')
const SensorsView = () => import('../views/SensorsView.vue')
const TodoView = () => import('../views/TodoView.vue')
const DailyReportDetailView = () => import('../views/DailyReportDetailView.vue')
const DailyReportShell = () => import('../components/DailyReportShell.vue')
const ProjectLayout = () => import('../components/ProjectLayout.vue')
const ProjectDashboard = () => import('../components/ProjectDashboard.vue')
const ManualView = () => import('../views/ManualView.vue')

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: () => import('../views/DashboardView.vue'), meta: { requiresAuth: true, titleKey: 'nav.home' } },
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/projects', component: ProjectsView, meta: { titleKey: 'nav.projects' } },
    {
      path: '/daily-report',
      component: DailyReportShell,
      meta: { titleKey: 'nav.dailyReport' },
      children: [
        { path: '', component: DailyReportView, meta: { titleKey: 'nav.dailyReport' } },
        { path: 'history', component: DailyHistoryView, meta: { titleKey: 'nav.dailyReport' } }
      ]
    },
    {
      path: '/projects/:id',
      component: ProjectLayout,
      meta: { titleKey: 'nav.projects' },
      children: [
        { path: '', component: ProjectDashboard, meta: { titleKey: 'nav.projects' } },
        { path: 'issues', component: IssuesView, meta: { titleKey: 'nav.projects' } },
        { path: 'experiment-runs', component: RunListView, meta: { titleKey: 'nav.projects' } },
        { path: 'test-data', component: TestDataView, meta: { titleKey: 'nav.projects' } },
        { path: 'rf-matching', component: RFMatchingView, meta: { titleKey: 'nav.projects' } },
        { path: 'assembly', component: AssemblyView, meta: { titleKey: 'nav.projects' } }
      ]
    },
    { path: '/experiment-runs/:id', component: RunDetailView, meta: { titleKey: 'mobile.title.runDetail' } },
    { path: '/step-templates', component: StepTemplatesView, meta: { requiresAuth: true, titleKey: 'mobile.title.stepTemplates' } },
    { path: '/attachments', component: AttachmentView, meta: { titleKey: 'nav.attachments' } },
    { path: '/instrument-measure', component: InstrumentMeasureView, meta: { titleKey: 'nav.instruments' } },
    { path: '/gas-control', component: GasControlView, meta: { titleKey: 'nav.gasControl' } },
    { path: '/sensors', component: SensorsView, meta: { titleKey: 'nav.sensors' } },
    { path: '/todos', component: TodoView, meta: { requiresAuth: true, titleKey: 'nav.todos' } },
    { path: '/experiences', component: ExperiencesView, meta: { titleKey: 'nav.experiences' } },
    { path: '/audit', component: AuditView, meta: { titleKey: 'nav.audit' } },
    { path: '/settings', component: SettingsView, meta: { titleKey: 'nav.settings' } },
    { path: '/manual', component: ManualView, meta: { requiresAuth: true, titleKey: 'nav.manual' } },
    { path: '/daily-reports/:id', component: DailyReportDetailView, meta: { titleKey: 'mobile.title.dailyReportDetail' } },
    { path: '/admin/users', component: AdminUsersView, meta: { admin: true, titleKey: 'nav.adminUsers' } },
    { path: '/agent-candidates', component: AgentCandidatesView, meta: { reviewer: true, titleKey: 'nav.aiReview' } },
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
