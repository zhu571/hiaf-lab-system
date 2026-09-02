import { describe, it, expect } from 'vitest'
import { NAV_ITEMS, filterNavByRole, groupNavBySection, type NavEntry } from '../navigation'

// 导航角色过滤矩阵（重构方案 §5 关键路径 2）：viewer/maintainer/admin × 各 NavEntry。
// 角色排序 viewer < maintainer < admin 写死在 filterNavByRole 内；
// minRole 缺省 = 全部登录角色可见，'maintainer' = maintainer+admin，'admin' = 仅 admin。

function entry(overrides: Partial<NavEntry>): NavEntry {
  return { path: '/x', icon: {} as NavEntry['icon'], titleKey: 't', group: 'main', ...overrides }
}

describe('filterNavByRole：角色矩阵', () => {
  const items: NavEntry[] = [
    entry({ path: '/open' }),
    entry({ path: '/maintainer-only', minRole: 'maintainer' }),
    entry({ path: '/admin-only', minRole: 'admin' })
  ]

  it('viewer：仅可见无 minRole 项', () => {
    expect(filterNavByRole(items, 'viewer').map((i) => i.path)).toEqual(['/open'])
  })

  it('maintainer：可见无 minRole 项 + maintainer 级项，不见 admin 级', () => {
    expect(filterNavByRole(items, 'maintainer').map((i) => i.path)).toEqual(['/open', '/maintainer-only'])
  })

  it('admin：全部可见', () => {
    expect(filterNavByRole(items, 'admin').map((i) => i.path)).toEqual(['/open', '/maintainer-only', '/admin-only'])
  })

  it('未知角色（空串/自定义）按最低级 viewer 处理', () => {
    expect(filterNavByRole(items, '').map((i) => i.path)).toEqual(['/open'])
    expect(filterNavByRole(items, 'superadmin').map((i) => i.path)).toEqual(['/open'])
  })

  it('过滤保持 NAV_ITEMS 原始顺序（顺序即展示顺序）', () => {
    const reversed = [...items].reverse()
    expect(filterNavByRole(reversed, 'admin').map((i) => i.path)).toEqual(['/admin-only', '/maintainer-only', '/open'])
  })

  it('空列表返回空列表', () => {
    expect(filterNavByRole([], 'admin')).toEqual([])
  })
})

describe('NAV_ITEMS 实际配置（对齐 AppLayout 三组派生）', () => {
  it('配置完整性：16 项、path 唯一、titleKey 非空', () => {
    expect(NAV_ITEMS).toHaveLength(16)
    const paths = NAV_ITEMS.map((i) => i.path)
    expect(new Set(paths).size).toBe(paths.length)
    for (const i of NAV_ITEMS) {
      expect(i.titleKey.length).toBeGreaterThan(0)
      expect(['main', 'system']).toContain(i.group)
    }
  })

  it('桌面主组（viewer）：7 项，aiReview 经 minRole 过滤隐藏', () => {
    const main = filterNavByRole(NAV_ITEMS.filter((i) => i.group === 'main'), 'viewer')
    expect(main.map((i) => i.path)).toEqual(['/', '/projects', '/my-logs', '/todos', '/daily-report', '/experiences', '/attachments'])
    expect(main.find((i) => i.path === '/agent-candidates')).toBeUndefined()
  })

  it('桌面主组（maintainer）：8 项，agent-candidates 带 agentPending 徽章', () => {
    const main = filterNavByRole(NAV_ITEMS.filter((i) => i.group === 'main'), 'maintainer')
    expect(main).toHaveLength(8)
    const aiReview = main.find((i) => i.path === '/agent-candidates')
    expect(aiReview?.badge).toBe('agentPending')
    expect(aiReview?.minRole).toBe('maintainer')
  })

  it('桌面系统组（admin）：7 项且不含仅移动端 settings；admin/users 仅 admin 可见；顺序按 section 聚类（device 在前）', () => {
    const systemAdmin = filterNavByRole(NAV_ITEMS.filter((i) => i.group === 'system' && !i.mobile), 'admin')
    expect(systemAdmin.map((i) => i.path)).toEqual([
      '/gas-control',
      '/instrument-measure',
      '/sensors',
      '/alerts',
      '/audit',
      '/admin/users',
      '/manual'
    ])
    const systemViewer = filterNavByRole(NAV_ITEMS.filter((i) => i.group === 'system' && !i.mobile), 'viewer')
    expect(systemViewer.find((i) => i.path === '/admin/users')).toBeUndefined()
  })

  it('系统组项均带 section 标注（device/manage），main 组项不设 section', () => {
    for (const i of NAV_ITEMS.filter((i) => i.group === 'system')) {
      expect(['device', 'manage']).toContain(i.section)
    }
    for (const i of NAV_ITEMS.filter((i) => i.group === 'main')) {
      expect(i.section).toBeUndefined()
    }
  })

  it('移动底栏：mobile:true 恰好 5 项（home/projects/todos/daily-report/settings）', () => {
    const mobile = filterNavByRole(NAV_ITEMS.filter((i) => i.mobile), 'viewer')
    expect(mobile.map((i) => i.path)).toEqual(['/', '/projects', '/todos', '/daily-report', '/settings'])
    // settings 是仅移动端项：桌面系统组过滤（group=system 且 !mobile）不得包含它
    const desktopSystem = filterNavByRole(NAV_ITEMS.filter((i) => i.group === 'system' && !i.mobile), 'admin')
    expect(desktopSystem.find((i) => i.path === '/settings')).toBeUndefined()
  })

  it('mobile 项带 shortTitleKey（projects 无短标签回退 titleKey）', () => {
    const mobile = NAV_ITEMS.filter((i) => i.mobile)
    for (const i of mobile) {
      if (i.path === '/projects') {
        expect(i.shortTitleKey).toBeUndefined()
        expect(i.titleKey).toBe('nav.projects')
      } else {
        expect(i.shortTitleKey).toBeTruthy()
      }
    }
  })
})

describe('groupNavBySection：系统组 section 聚类（nav-menu-redesign 方案 §3.1）', () => {
  it('按 NAV_SECTION_ORDER 顺序聚类（device 在前 manage 在后），组内保持输入顺序', () => {
    const items: NavEntry[] = [
      entry({ path: '/m1', group: 'system', section: 'manage' }),
      entry({ path: '/d1', group: 'system', section: 'device' }),
      entry({ path: '/d2', group: 'system', section: 'device' }),
      entry({ path: '/m2', group: 'system', section: 'manage' })
    ]
    const groups = groupNavBySection(items)
    expect(groups.map((g) => g.section)).toEqual(['device', 'manage'])
    expect(groups[0].items.map((i) => i.path)).toEqual(['/d1', '/d2'])
    expect(groups[1].items.map((i) => i.path)).toEqual(['/m1', '/m2'])
  })

  it('未标 section 的项兜底归 manage（防新增项漏标时整组消失）', () => {
    const groups = groupNavBySection([entry({ path: '/x', group: 'system' })])
    expect(groups).toHaveLength(1)
    expect(groups[0].section).toBe('manage')
    expect(groups[0].items.map((i) => i.path)).toEqual(['/x'])
  })

  it('空组不输出；空列表返回空列表', () => {
    const onlyDevice = groupNavBySection([entry({ path: '/d', group: 'system', section: 'device' })])
    expect(onlyDevice.map((g) => g.section)).toEqual(['device'])
    expect(groupNavBySection([])).toEqual([])
  })

  it('实际配置：桌面系统组（viewer）聚类为 device 3 项 + manage 3 项', () => {
    const systemViewer = filterNavByRole(NAV_ITEMS.filter((i) => i.group === 'system' && !i.mobile), 'viewer')
    const groups = groupNavBySection(systemViewer)
    expect(groups.map((g) => [g.section, g.items.length])).toEqual([
      ['device', 3],
      ['manage', 3]
    ])
  })
})
