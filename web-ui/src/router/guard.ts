import type { RouteLocationNormalized } from 'vue-router'
import type { UserInfo } from '@/api/auth'

// 路由守卫纯函数（重构方案 §3.6，S0）：四规则逐条保留自 router/index.ts 原 beforeEach，
// 纯函数化后可直接单测。index.ts 的 beforeEach 只做「loadMe + 调纯函数 + 应用结果」。
// meta 语义：非 public 即需登录（requiresAuth 冗余装饰已删除）。

export interface GuardContext {
  ready: boolean
  user: UserInfo | null
}

// 返回重定向 path（string）或 undefined（放行）。ready 表明 loadMe 已完成
// （与「会话态丢失」用例保持同一语义：ready=true 但 user=null 仍按未登录处理）。
export function resolveRouteGuard(to: RouteLocationNormalized, ctx: GuardContext): string | undefined {
  // 规则 1：非 public 无 user → /login
  if (!to.meta.public && !ctx.user) return '/login'
  // 规则 2：meta.admin 非 admin 越权 → /projects
  if (to.meta.admin && ctx.user?.role !== 'admin') return '/projects'
  // 规则 3：meta.reviewer 非 maintainer/admin 越权 → /projects（对应 auth store canReviewAgent，auth.ts:19）
  if (to.meta.reviewer && !['admin', 'maintainer'].includes(ctx.user?.role || '')) return '/projects'
  // 规则 4：must_change_password 强制 /settings（/settings 本身放行）
  if (to.path !== '/settings' && ctx.user?.must_change_password) return '/settings'
  return undefined
}
