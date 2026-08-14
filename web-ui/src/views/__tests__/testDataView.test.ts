import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import TestDataView from '@/views/TestDataView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { TestData } from '@/api/testdata'

// TestDataView 页面测试（测试方案 §3.2 🔴）：列表三态、isViewer 无录入 tab、
// chartGroups 分组渲染、invalidate 确认流。

vi.mock('@/api/testdata', () => ({
  listTestData: vi.fn(),
  deleteTestData: vi.fn(),
  createTestDataBatch: vi.fn()
}))

vi.mock('@/api/runs', () => ({
  listRuns: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'proj_01' } })
}))

import { listTestData, deleteTestData } from '@/api/testdata'
import { listRuns } from '@/api/runs'

function makeRow(overrides: Partial<TestData> = {}): TestData {
  return {
    id: 'td_1',
    project_id: 'proj_01',
    data_type: 'pressure',
    measurement: '入口压强',
    value: 101325,
    unit: 'Pa',
    quality: 'normal',
    source: 'manual',
    measured_at: '2026-01-03T09:00:00+08:00',
    notes: '',
    created_at: '2026-01-03T09:00:00+08:00',
    updated_at: '2026-01-03T09:00:00+08:00',
    ...overrides
  }
}

async function mountView(role = 'member') {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().user = {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
  const wrapper = mount(TestDataView, {
    global: {
      plugins: [createTestI18n(), pinia],
      stubs: { teleport: true, ElSelect: true, ElInputNumber: true, ElDatePicker: true }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listTestData).mockReset()
  vi.mocked(deleteTestData).mockReset()
  vi.mocked(listRuns).mockReset().mockResolvedValue({ items: [], total: 0, page: 1 })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('TestDataView 列表', () => {
  it('member：默认录入 tab + 列表加载渲染表格行；批次下拉一并拉取', async () => {
    vi.mocked(listTestData).mockResolvedValue({
      items: [makeRow(), makeRow({ id: 'td_2', measurement: '出口压强', value: 99 })],
      total: 2,
      page: 1
    })
    const wrapper = await mountView('member')
    expect(listTestData).toHaveBeenCalledWith('proj_01', expect.objectContaining({ page: 1, per_page: 20 }))
    expect(listRuns).toHaveBeenCalledWith('proj_01', expect.any(Object))
    // 默认在录入 tab（member 有录入权限）
    const tabs = wrapper.findAll('.el-tabs__item').map((t) => t.text().trim())
    expect(tabs).toEqual(expect.arrayContaining(['录入', '数据列表', '趋势图']))
    // 切到列表 tab 断言表格行
    const listTab = wrapper.findAll('.el-tabs__item').find((t) => t.text().trim() === '数据列表')!
    await listTab.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('入口压强')
    expect(wrapper.text()).toContain('出口压强')
  })

  it('viewer：无录入 tab 且无操作列（默认落列表 tab），行直接渲染', async () => {
    vi.mocked(listTestData).mockResolvedValue({ items: [makeRow()], total: 1, page: 1 })
    const wrapper = await mountView('viewer')
    const tabs = wrapper.findAll('.el-tabs__item').map((t) => t.text().trim())
    expect(tabs).not.toContain('录入')
    expect(wrapper.text()).toContain('入口压强')
    expect(listRuns).not.toHaveBeenCalled()
  })

  it('错误态：StateBlock 错误 + 重试按钮重新拉取', async () => {
    vi.mocked(listTestData).mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce({ items: [makeRow()], total: 1, page: 1 })
    const wrapper = await mountView('member')
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('测试数据加载失败')
    await wrapper.find('.state-block-retry').trigger('click')
    await flushPromises()
    expect(listTestData).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.state-block-error').exists()).toBe(false)
  })

  it('空态：无数据时 el-empty 显示', async () => {
    vi.mocked(listTestData).mockResolvedValue({ items: [], total: 0, page: 1 })
    const wrapper = await mountView('member')
    const listTab = wrapper.findAll('.el-tabs__item').find((t) => t.text().trim() === '数据列表')!
    await listTab.trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-empty__description').text()).toBe('暂无数据')
  })
})

describe('TestDataView invalidate 与图表', () => {
  it('invalidate 确认流：确认后调 deleteTestData 并刷新列表；invalid 行按钮禁用', async () => {
    // 首屏返回正常行；invalidate 后 run() 刷新返回 invalid 行
    vi.mocked(listTestData)
      .mockResolvedValueOnce({ items: [makeRow()], total: 1, page: 1 })
      .mockResolvedValueOnce({ items: [makeRow({ id: 'td_1', quality: 'invalid' })], total: 1, page: 1 })
    const wrapper = await mountView('member')
    const listTab = wrapper.findAll('.el-tabs__item').find((t) => t.text().trim() === '数据列表')!
    await listTab.trigger('click')
    await flushPromises()
    // 确认流（ElMessageBox.confirm 默认 resolve）→ 调删除
    const invalidateBtn = wrapper.findAll('button').find((b) => b.text().trim() === '标记无效')!
    await invalidateBtn.trigger('click')
    await flushPromises()
    expect(deleteTestData).toHaveBeenCalledWith('td_1')
    // 刷新后行已 invalid → 标记无效按钮禁用
    const disabledBtn = wrapper.findAll('button').find((b) => b.text().trim() === '标记无效')!
    expect(disabledBtn.attributes('disabled')).toBeDefined()
  })

  it('图表 tab：按测量项分组渲染 SVG 曲线与图例（chartGroups）', async () => {
    vi.mocked(listTestData).mockResolvedValue({
      items: [
        makeRow({ id: 'td_1', measurement: '入口压强', value: 101325 }),
        makeRow({ id: 'td_2', measurement: '入口压强', value: 101400 }),
        makeRow({ id: 'td_3', measurement: '出口压强', value: 99 })
      ],
      total: 3,
      page: 1
    })
    const wrapper = await mountView('member')
    const chartTab = wrapper.findAll('.el-tabs__item').find((t) => t.text().trim() === '趋势图')!
    await chartTab.trigger('click')
    await flushPromises()
    expect(wrapper.find('.trend-chart').exists()).toBe(true)
    const legends = wrapper.findAll('.legend-item')
    expect(legends.map((l) => l.text().trim())).toEqual(expect.arrayContaining(['入口压强', '出口压强']))
  })
})
