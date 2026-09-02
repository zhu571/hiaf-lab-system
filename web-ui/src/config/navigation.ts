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
// 3. ProjectLayout tabs 数组顺序 = router children 顺序（/projects/:id 下 7 个子路由），新增 tab 需双改。
// 4. section（nav-menu-redesign 方案 §3.1）：仅 system 组项使用，桌面侧栏与移动抽屉按
//    groupNavBySection 聚类出「设备监控 / 系统管理」小标题；main 组无标题不设 section。

export type NavSection = 'device' | 'manage'

// section 展示顺序（即侧栏/抽屉内小标题顺序）
export const NAV_SECTION_ORDER: NavSection[] = ['device', 'manage']

export interface NavEntry {
  path: string
  icon: Component
  titleKey: string
  shortTitleKey?: string
  group: 'main' | 'system'
  section?: NavSection
  minRole?: 'maintainer' | 'admin'
  badge?: 'agentPending'
  mobile?: boolean
}

export const NAV_ITEMS: NavEntry[] = [
  { path: '/', icon: HomeFilled, titleKey: 'nav.home', shortTitleKey: 'nav.short.home', group: 'main', mobile: true },
  { path: '/projects', icon: FolderOpened, titleKey: 'nav.projects', group: 'main', mobile: true },
	{ path: '/my-logs', icon: Document, titleKey: 'nav.myLogs', group: 'main' },
  { path: '/todos', icon: Tickets, titleKey: 'nav.todos', shortTitleKey: 'nav.short.todos', group: 'main', mobile: true },
  { path: '/daily-report', icon: Document, titleKey: 'nav.dailyReport', shortTitleKey: 'nav.short.dailyReport', group: 'main', mobile: true },
  { path: '/experiences', icon: Memo, titleKey: 'nav.experiences', group: 'main' },
  { path: '/attachments', icon: Paperclip, titleKey: 'nav.attachments', group: 'main' },
  { path: '/agent-candidates', icon: MagicStick, titleKey: 'nav.aiReview', group: 'main', minRole: 'maintainer', badge: 'agentPending' },
  { path: '/gas-control', icon: Monitor, titleKey: 'nav.gasControl', group: 'system', section: 'device' },
  { path: '/instrument-measure', icon: Odometer, titleKey: 'nav.instruments', group: 'system', section: 'device' },
  { path: '/sensors', icon: Connection, titleKey: 'nav.sensors', group: 'system', section: 'device' },
  { path: '/alerts', icon: Bell, titleKey: 'nav.alert', group: 'system', section: 'manage' },
  { path: '/audit', icon: DataBoard, titleKey: 'nav.audit', group: 'system', section: 'manage' },
  { path: '/admin/users', icon: User, titleKey: 'nav.adminUsers', group: 'system', section: 'manage', minRole: 'admin' },
  { path: '/manual', icon: Reading, titleKey: 'nav.manual', group: 'system', section: 'manage' },
  { path: '/settings', icon: Setting, titleKey: 'nav.settings', shortTitleKey: 'nav.short.mine', group: 'system', section: 'manage', mobile: true }
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

// section 聚类纯函数（nav-menu-redesign 方案 §3.1，对齐 filterNavByRole 泛型先例）：
// 输入已过滤的 system 组项，按 NAV_SECTION_ORDER 顺序聚类输出；
// 未标 section 的项兜底归 'manage'（防新增项漏标时整组消失），空组不输出。
export function groupNavBySection<T extends { section?: NavSection }>(items: T[]): { section: NavSection; items: T[] }[] {
  return NAV_SECTION_ORDER.map((section) => ({
    section,
    items: items.filter((i) => (i.section ?? 'manage') === section)
  })).filter((g) => g.items.length > 0)
}
