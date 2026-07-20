import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import LoginView from '../views/LoginView.vue'
import ProjectsView from '../views/ProjectsView.vue'
import DailyReportView from '../views/DailyReportView.vue'
import IssuesView from '../views/IssuesView.vue'
import ExperiencesView from '../views/ExperiencesView.vue'
import AuditView from '../views/AuditView.vue'
import SettingsView from '../views/SettingsView.vue'
import DailyHistoryView from '../views/DailyHistoryView.vue'
import AdminUsersView from '../views/AdminUsersView.vue'
import AgentCandidatesView from '../views/AgentCandidatesView.vue'
import RunListView from '../views/RunListView.vue'
import RunDetailView from '../views/RunDetailView.vue'
import TestDataView from '../views/TestDataView.vue'
import RFMatchingView from '../views/RFMatchingView.vue'
import AssemblyView from '../views/AssemblyView.vue'
import AttachmentView from '../views/AttachmentView.vue'
import InstrumentsView from '../views/InstrumentsView.vue'
import SensorsView from '../views/SensorsView.vue'
import DailyReportDetailView from '../views/DailyReportDetailView.vue'
import DailyReportShell from '../components/DailyReportShell.vue'
import ProjectLayout from '../components/ProjectLayout.vue'
import ProjectDashboard from '../components/ProjectDashboard.vue'

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
    { path: '/instruments', component: InstrumentsView },
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
    { path: '/projects/:id/runs', redirect: '/projects/:id/experiment-runs' }
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
