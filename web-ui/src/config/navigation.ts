import type { Component } from 'vue'
import {
  Bell,
  Connection,
  DataBoard,
  Document,
  FolderOpened,
  HomeFilled,
  MagicStick,
  Memo,
  Monitor,
  Odometer,
  Paperclip,
  Reading,
  Setting,
  Tickets,
  User
} from '@element-plus/icons-vue'

// 导航单一数据源（重构方案 §3.3，S0）：
// AppLayout 的桌面主组/系统组与移动底栏全部由 NAV_ITEMS 过滤派生，新增页面只改这里。
//
// 可见性只有一个维度 minRole：
//   缺省        = 全部登录角色可见
//   'maintainer' = maintainer + admin 可见（对应 auth store canReviewAgent，auth.ts:19）
//   'admin'     = 仅 admin 可见（对应 auth store isAdmin，auth.ts:18）
// 不引入 roles 数组（单一维度已覆盖全部现状判断，避免双键歧义）。
//
// 角色排序 viewer < maintainer < admin 写死在 filterNavByRole 内。
//
// 约束说明：
// 1. 前端角色过滤只是 UX，不替代后端鉴权——路由 meta（admin/reviewer）与后端接口强校验仍是安全边界。
// 2. mobile: true = 出现在移动底栏；settings 为仅移动端项（group 'system' 但桌面系统组过滤时排除 mobile 项）。
// 3. ProjectLayout tabs 数组顺序 = router children 顺序（/projects/:id 下 6 个子路由），新增 tab 需双改。

export interface NavEntry {
  path: string
  icon: Component
  titleKey: string
  shortTitleKey?: string
  group: 'main' | 'system'
  minRole?: 'maintainer' | 'admin'
  badge?: 'agentPending'
  mobile?: boolean
}

export const NAV_ITEMS: NavEntry[] = [
  { path: '/', icon: HomeFilled, titleKey: 'nav.home', shortTitleKey: 'nav.short.home', group: 'main', mobile: true },
  { path: '/projects', icon: FolderOpened, titleKey: 'nav.projects', group: 'main', mobile: true },
  { path: '/todos', icon: Tickets, titleKey: 'nav.todos', shortTitleKey: 'nav.short.todos', group: 'main', mobile: true },
  { path: '/daily-report', icon: Document, titleKey: 'nav.dailyReport', shortTitleKey: 'nav.short.dailyReport', group: 'main', mobile: true },
  { path: '/experiences', icon: Memo, titleKey: 'nav.experiences', group: 'main' },
  { path: '/attachments', icon: Paperclip, titleKey: 'nav.attachments', group: 'main' },
  { path: '/agent-candidates', icon: MagicStick, titleKey: 'nav.aiReview', group: 'main', minRole: 'maintainer', badge: 'agentPending' },
  { path: '/gas-control', icon: Monitor, titleKey: 'nav.gasControl', group: 'system' },
  { path: '/instrument-measure', icon: Odometer, titleKey: 'nav.instruments', group: 'system' },
  { path: '/sensors', icon: Connection, titleKey: 'nav.sensors', group: 'system' },
  { path: '/admin/users', icon: User, titleKey: 'nav.adminUsers', group: 'system', minRole: 'admin' },
  { path: '/alerts', icon: Bell, titleKey: 'nav.alert', group: 'system' },
  { path: '/audit', icon: DataBoard, titleKey: 'nav.audit', group: 'system' },
  { path: '/manual', icon: Reading, titleKey: 'nav.manual', group: 'system' },
  { path: '/settings', icon: Setting, titleKey: 'nav.settings', shortTitleKey: 'nav.short.mine', group: 'system', mobile: true }
]

const ROLE_ORDER: Record<string, number> = { viewer: 0, maintainer: 1, admin: 2 }

// 角色过滤纯函数（可单测）：minRole 缺省放行全部角色；未知角色按 viewer 最低级处理。
// 泛型放宽（结构改版 R2 §3.1）：命令面板动作组等非 NavEntry 项复用同一过滤机制，
// 仅约束 minRole 字段，运行时行为不变。
export function filterNavByRole<T extends { minRole?: 'maintainer' | 'admin' }>(items: T[], role: string): T[] {
  const level = ROLE_ORDER[role] ?? 0
  return items.filter((item) => {
    if (!item.minRole) return true
    return level >= (ROLE_ORDER[item.minRole] ?? 0)
  })
}
