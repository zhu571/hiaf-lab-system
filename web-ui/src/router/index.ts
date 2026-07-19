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

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/projects' },
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/projects', component: ProjectsView },
    { path: '/daily-report', component: DailyReportView },
    { path: '/projects/:id/issues', component: IssuesView },
    { path: '/projects/:id/runs', component: RunListView },
    { path: '/runs/:id', component: () => import('../views/RunDetailView.vue') },
    { path: '/projects/:id/test-data', component: () => import('../views/TestDataView.vue') },
    { path: '/projects/:id/rf-matching', component: () => import('../views/RFMatchingView.vue') },
    { path: '/projects/:id/assembly', component: () => import('../views/AssemblyView.vue') },
    { path: '/attachments', component: () => import('../views/AttachmentView.vue') },
    { path: '/issues', component: () => import('../views/IssuesFallback.vue') },
    { path: '/experiences', component: ExperiencesView },
    { path: '/audit', component: AuditView },
    { path: '/settings', component: SettingsView },
    { path: '/daily-reports', component: DailyHistoryView },
    { path: '/daily-reports/:id', component: () => import('../views/DailyReportDetailView.vue') },
    { path: '/admin/users', component: AdminUsersView, meta: { admin: true } },
    { path: '/agent-candidates', component: AgentCandidatesView, meta: { reviewer: true } }
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
