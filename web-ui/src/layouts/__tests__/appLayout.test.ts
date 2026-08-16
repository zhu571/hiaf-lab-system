import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { ElBadge } from 'element-plus'
import AppLayout from '@/layouts/AppLayout.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAskDialog } from '@/composables/useAskDialog'
import { useCommandPalette } from '@/composables/useCommandPalette'
import { useAgentPending } from '@/composables/useAgentPending'
import { useAuthStore } from '@/stores/auth'
import type { UserInfo } from '@/api/auth'

// AppLayout 组件测试（测试方案 §3.2 🟢 smoke）：
// 侧栏导航渲染、.nav-ask 按钮触发 openAskDialog、viewer 角色导航项过滤（filterNavByRole）。
// 结构改版 R1 补：桌面顶栏（折叠按钮/面包屑/用户菜单）、折叠双态 class 与持久化、面包屑规则表。

const routeState = vi.hoisted(() => ({
  current: { path: '/', meta: {} as Record<string, unknown>, params: {} as Record<string, string> }
}))

vi.mock('@/api/agent', () => ({
  listAgentCandidates: vi.fn()
}))

vi.mock('@/api/todos', () => ({
  listTodos: vi.fn().mockResolvedValue([])
}))

vi.mock('@/api/alerts', () => ({
  listAlerts: vi.fn().mockResolvedValue({ items: [], total: 0, limit: 6, offset: 0 })
}))

vi.mock('@/api/ask', () => ({
  askChat: vi.fn(),
  askHistory: vi.fn(),
  askHistoryDetail: vi.fn(),
  newIdempotencyKey: vi.fn(() => 'idem_1')
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState.current,
  useRouter: () => ({ push: vi.fn() })
}))

import { listAgentCandidates } from '@/api/agent'
import { listProjects } from '@/api/projects'

function makeUser(role: string): UserInfo {
  return {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
}

async function mountLayout(role: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().user = makeUser(role)
  const wrapper = mount(AppLayout, {
    global: {
      plugins: [createTestI18n(), pinia],
      // ElDropdown 真件在 jsdom 触发 "Maximum recursive updates"（popper 内部循环）；
      // 模板桩只渲染 default 槽（触发按钮）不渲染 #dropdown 菜单，本用例不断言下拉菜单；
      // RouterLink 渲染为纯插槽容器（保留导航项文本），RouterView 空占位
      stubs: {
        RouterView: true,
        RouterLink: { template: '<a><slot /></a>' },
        teleport: true,
        ElDropdown: { template: '<div><slot /></div>' }
      }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listAgentCandidates).mockReset()
  vi.mocked(listProjects).mockResolvedValue([])
  routeState.current = { path: '/', meta: {}, params: {} }
  // 折叠状态走 useLocalStorage 持久化，用例间互不影响须清空
  localStorage.clear()
})

afterEach(() => {
  useAskDialog().askOpen.value = false
  useCommandPalette().closePalette()
  // R2 单例计数跨用例共享，后置清零防串扰
  useAgentPending().agentPending.value = 0
  vi.restoreAllMocks()
})

describe('AppLayout 导航渲染', () => {
  it('admin：主组/系统组导航项渲染，.nav-ask 存在，待审核徽章展示候选总数', async () => {
    vi.mocked(listAgentCandidates).mockResolvedValue({
      items: [],
      total: 3,
      page: 1,
      per_page: 1
    })
    const wrapper = await mountLayout('admin')
    expect(wrapper.find('.nav').exists()).toBe(true)
    expect(wrapper.text()).toContain('项目')
    expect(wrapper.text()).toContain('AI审核')
    expect(wrapper.find('.nav-ask').exists()).toBe(true)
    expect(wrapper.text()).toContain('AI 问答')
    expect(listAgentCandidates).toHaveBeenCalledWith(
      expect.objectContaining({ status: 'pending_review' })
    )
    await flushPromises()
    expect(wrapper.find('.nav-badge .el-badge__content').text()).toBe('3')
    wrapper.unmount()
  })

  it('viewer：maintainer/admin 专属导航项（AI审核/用户管理）被过滤，徽章不拉取', async () => {
    const wrapper = await mountLayout('viewer')
    expect(wrapper.text()).toContain('项目')
    expect(wrapper.text()).not.toContain('AI审核')
    expect(wrapper.text()).not.toContain('用户管理')
    expect(listAgentCandidates).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('点击 .nav-ask 触发 openAskDialog（useAskDialog 单例 askOpen 置真）', async () => {
    const wrapper = await mountLayout('maintainer')
    expect(useAskDialog().askOpen.value).toBe(false)
    await wrapper.find('.nav-ask').trigger('click')
    expect(useAskDialog().askOpen.value).toBe(true)
    wrapper.unmount()
  })
})

describe('AppLayout 桌面顶栏（R1）', () => {
  it('顶栏渲染：折叠按钮 + 面包屑容器 + 用户菜单（迁入顶栏右侧），侧栏不再有用户卡片', async () => {
    const wrapper = await mountLayout('admin')
    expect(wrapper.find('.topbar').exists()).toBe(true)
    expect(wrapper.find('.collapse-btn').exists()).toBe(true)
    expect(wrapper.find('.topbar .user-card-btn').exists()).toBe(true)
    expect(wrapper.find('.topbar').text()).toContain('Test User')
    expect(wrapper.find('.nav .user-card-btn').exists()).toBe(false)
    // 首页路由（meta 无 titleKey 的测试态）面包屑为空不渲染
    expect(wrapper.find('.app-breadcrumb').exists()).toBe(false)
    wrapper.unmount()
  })

  it('折叠切换：nav-collapsed 双态 class、aria-label 与 localStorage 持久化；折叠态 badge 退化 is-dot', async () => {
    vi.mocked(listAgentCandidates).mockResolvedValue({ items: [], total: 2, page: 1, per_page: 1 })
    const wrapper = await mountLayout('admin')
    await flushPromises()
    const btn = wrapper.find('.collapse-btn')
    expect(btn.attributes('aria-label')).toBe('收起侧边栏')
    expect(wrapper.find('.layout').classes()).not.toContain('nav-collapsed')

    await btn.trigger('click')
    expect(wrapper.find('.layout').classes()).toContain('nav-collapsed')
    expect(btn.attributes('aria-label')).toBe('展开侧边栏')
    expect(localStorage.getItem('lab-nav-collapsed')).toBe('true')
    expect(wrapper.findComponent(ElBadge).props('isDot')).toBe(true)

    await btn.trigger('click')
    expect(wrapper.find('.layout').classes()).not.toContain('nav-collapsed')
    expect(localStorage.getItem('lab-nav-collapsed')).toBe('false')
    expect(wrapper.findComponent(ElBadge).props('isDot')).toBe(false)
    wrapper.unmount()
  })
})

describe('AppLayout 顶栏 R2 入口（命令面板 + 通知中心）', () => {
  it('顶栏渲染搜索触发框与铃铛；点击触发框打开全局挂载的命令面板', async () => {
    const wrapper = await mountLayout('admin')
    expect(wrapper.find('.palette-trigger').exists()).toBe(true)
    expect(wrapper.find('.notify-trigger').exists()).toBe(true)
    expect(useCommandPalette().paletteOpen.value).toBe(false)

    await wrapper.find('.palette-trigger').trigger('click')
    await flushPromises()
    expect(useCommandPalette().paletteOpen.value).toBe(true)
    expect(wrapper.find('.palette-input').exists()).toBe(true)
    wrapper.unmount()
  })

  it('Ctrl+K 全局快捷键唤起命令面板（AppLayout 全局挂载生效）', async () => {
    const wrapper = await mountLayout('viewer')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }))
    await flushPromises()
    expect(useCommandPalette().paletteOpen.value).toBe(true)
    wrapper.unmount()
  })
})

describe('AppLayout 面包屑规则表（R1 §2.3）', () => {
  it('一级页（/todos）：单段纯标题，无链接无分隔符', async () => {
    routeState.current = { path: '/todos', meta: { titleKey: 'nav.todos' }, params: {} }
    const wrapper = await mountLayout('viewer')
    const crumbs = wrapper.findAll('.crumb-item')
    expect(crumbs).toHaveLength(1)
    expect(wrapper.find('.crumb-text').text()).toBe('待办')
    expect(wrapper.find('.crumb-link').exists()).toBe(false)
    wrapper.unmount()
  })

  it('/daily-report：单段「日报」', async () => {
    routeState.current = { path: '/daily-report', meta: { titleKey: 'nav.dailyReport' }, params: {} }
    const wrapper = await mountLayout('viewer')
    expect(wrapper.findAll('.crumb-item')).toHaveLength(1)
    expect(wrapper.find('.crumb-text').text()).toBe('日报')
    wrapper.unmount()
  })

  it('/daily-report/history：[日报（可点） / 日报历史]', async () => {
    routeState.current = { path: '/daily-report/history', meta: { titleKey: 'nav.dailyReport' }, params: {} }
    const wrapper = await mountLayout('viewer')
    expect(wrapper.findAll('.crumb-item')).toHaveLength(2)
    expect(wrapper.find('.crumb-link').text()).toBe('日报')
    expect(wrapper.find('.crumb-text').text()).toBe('日报历史')
    wrapper.unmount()
  })

  it('/daily-reports/:id：[日报（指向历史） / 日报详情]', async () => {
    routeState.current = { path: '/daily-reports/r1', meta: { titleKey: 'mobile.title.dailyReportDetail' }, params: { id: 'r1' } }
    const wrapper = await mountLayout('viewer')
    expect(wrapper.findAll('.crumb-item')).toHaveLength(2)
    expect(wrapper.find('.crumb-link').text()).toBe('日报')
    expect(wrapper.find('.crumb-text').text()).toBe('日报详情')
    wrapper.unmount()
  })

  it('/projects：单段「项目」', async () => {
    routeState.current = { path: '/projects', meta: { titleKey: 'nav.projects' }, params: {} }
    const wrapper = await mountLayout('viewer')
    expect(wrapper.findAll('.crumb-item')).toHaveLength(1)
    expect(wrapper.find('.crumb-text').text()).toBe('项目')
    wrapper.unmount()
  })

  it('/projects/:id/tab：[项目（可点） / 项目名] 两段即止（项目名取自 project store 只读缓存）', async () => {
    vi.mocked(listProjects).mockResolvedValue([
      { id: 'p1', code: 'HIAF', name: 'HIAF气靶', short_name: '', description: '', status: 'active', visibility: 'private' }
    ])
    routeState.current = { path: '/projects/p1/issues', meta: { titleKey: 'nav.projects' }, params: { id: 'p1' } }
    const wrapper = await mountLayout('viewer')
    await flushPromises()
    expect(wrapper.findAll('.crumb-item')).toHaveLength(2)
    expect(wrapper.find('.crumb-link').text()).toBe('项目')
    expect(wrapper.find('.crumb-text').text()).toBe('HIAF气靶')
    wrapper.unmount()
  })

  it('/projects/:id 项目名未加载时降级为单段「项目」', async () => {
    routeState.current = { path: '/projects/p404', meta: { titleKey: 'nav.projects' }, params: { id: 'p404' } }
    const wrapper = await mountLayout('viewer')
    expect(wrapper.findAll('.crumb-item')).toHaveLength(1)
    expect(wrapper.find('.crumb-text').text()).toBe('项目')
    wrapper.unmount()
  })

  it('/experiment-runs/:id：单段「运行详情」', async () => {
    routeState.current = { path: '/experiment-runs/run1', meta: { titleKey: 'mobile.title.runDetail' }, params: { id: 'run1' } }
    const wrapper = await mountLayout('viewer')
    expect(wrapper.findAll('.crumb-item')).toHaveLength(1)
    expect(wrapper.find('.crumb-text').text()).toBe('运行详情')
    wrapper.unmount()
  })

  it('/step-templates：单段「步骤模板」', async () => {
    routeState.current = { path: '/step-templates', meta: { titleKey: 'mobile.title.stepTemplates' }, params: {} }
    const wrapper = await mountLayout('viewer')
    expect(wrapper.findAll('.crumb-item')).toHaveLength(1)
    expect(wrapper.find('.crumb-text').text()).toBe('步骤模板')
    wrapper.unmount()
  })
})
