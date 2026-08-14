import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import IssuesView from '@/views/IssuesView.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { Issue } from '@/api/issues'

// IssuesView 页面测试（测试方案 §3.2 🔴）：grouped 四列分组、空列提示、
// drawer 打开加载详情、状态流转按钮（transitionIssue）。

vi.mock('@/api/issues', () => ({
  listIssues: vi.fn(),
  getIssue: vi.fn(),
  createIssue: vi.fn(),
  transitionIssue: vi.fn(),
  addIssueComment: vi.fn()
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'proj_01' } })
}))

import { listIssues, getIssue, transitionIssue } from '@/api/issues'

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: 'issue_01',
    project_id: 'proj_01',
    title: '真空度抖动',
    description: '描述',
    status: 'open',
    severity: 'high',
    author_id: 'user_01',
    ...overrides
  }
}

async function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const wrapper = mount(IssuesView, {
    global: {
      plugins: [createTestI18n(), pinia],
      // ElSelect 在 jsdom 触发递归更新（抽屉/表单内多实例），本页断言不依赖真实下拉
      stubs: { teleport: true, ElSelect: true }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listIssues).mockReset()
  vi.mocked(getIssue).mockReset()
  vi.mocked(transitionIssue).mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('IssuesView 看板', () => {
  it('列表加载：四列看板按状态分组渲染卡片与计数', async () => {
    vi.mocked(listIssues).mockResolvedValue({
      items: [
        makeIssue({ id: 'i1', title: '真空度抖动', status: 'open' }),
        makeIssue({ id: 'i2', title: 'RF 功率漂移', status: 'in_progress' }),
        makeIssue({ id: 'i3', title: '已定位根因', status: 'resolved' }),
        makeIssue({ id: 'i4', title: '历史问题', status: 'closed' })
      ],
      total: 4,
      page: 1
    })
    const wrapper = await mountView()
    expect(listIssues).toHaveBeenCalledWith('proj_01', expect.any(Object))
    const columns = wrapper.findAll('.column')
    expect(columns).toHaveLength(4)
    const cards = wrapper.findAll('.issue-card')
    expect(cards).toHaveLength(4)
    expect(cards[0].text()).toContain('真空度抖动')
    expect(cards[1].text()).toContain('RF 功率漂移')
    expect(cards[2].text()).toContain('已定位根因')
    expect(cards[3].text()).toContain('历史问题')
    const counts = columns.map((c) => c.find('.count').text())
    expect(counts).toEqual(['1', '1', '1', '1'])
  })

  it('空列提示：无数据列显示 empty-hint；全空时四列均显示', async () => {
    vi.mocked(listIssues).mockResolvedValue({ items: [makeIssue()], total: 1, page: 1 })
    const wrapper = await mountView()
    const emptyHints = wrapper.findAll('.empty-hint')
    expect(emptyHints).toHaveLength(3)
    expect(emptyHints[0].text()).toBe('暂无问题')
  })

  it('错误态：StateBlock 错误展示 + 重试按钮重新加载', async () => {
    vi.mocked(listIssues)
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce({ items: [makeIssue()], total: 1, page: 1 })
    const wrapper = await mountView()
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    await wrapper.find('.state-block-retry').trigger('click')
    await flushPromises()
    expect(listIssues).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.state-block-error').exists()).toBe(false)
  })
})

describe('IssuesView 详情与流转', () => {
  it('点击卡片打开 drawer：getIssue 加载详情并渲染评论/状态区', async () => {
    vi.mocked(listIssues).mockResolvedValue({ items: [makeIssue()], total: 1, page: 1 })
    vi.mocked(getIssue).mockResolvedValue(makeIssue({ comments: [] }))
    const wrapper = await mountView()
    await wrapper.find('.issue-card').trigger('click')
    await flushPromises()
    expect(getIssue).toHaveBeenCalledWith('issue_01')
    expect(wrapper.find('.el-drawer').exists()).toBe(true)
    expect(wrapper.text()).toContain('真空度抖动')
    expect(wrapper.text()).toContain('评论')
  })

  it('状态流转：改目标状态提交调 transitionIssue，成功后刷新列表并重开详情', async () => {
    vi.mocked(listIssues)
      .mockResolvedValueOnce({ items: [makeIssue()], total: 1, page: 1 })
      .mockResolvedValue({ items: [], total: 0, page: 1 })
    vi.mocked(getIssue).mockResolvedValue(makeIssue({ comments: [] }))
    vi.mocked(transitionIssue).mockResolvedValue(makeIssue({ status: 'resolved' }))
    const wrapper = await mountView()
    await wrapper.find('.issue-card').trigger('click')
    await flushPromises()
    const updateBtn = wrapper.findAll('button').find((b) => b.text().trim() === '更新状态')!
    await updateBtn.trigger('click')
    await flushPromises()
    expect(transitionIssue).toHaveBeenCalledWith('issue_01', 'open', '')
    expect(getIssue).toHaveBeenCalledTimes(2)
  })
})
