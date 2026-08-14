import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DailyHistoryView from '@/views/DailyHistoryView.vue'
import { createTestI18n } from '@/test-utils/setup'

// DailyHistoryView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 列表渲染 + 空态。

vi.mock('@/api/logs', () => ({
  listReports: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

import { listReports } from '@/api/logs'

describe('DailyHistoryView 挂载冒烟', () => {
  it('日报历史列表渲染（日期/状态），空数据时 el-empty', async () => {
    vi.mocked(listReports).mockResolvedValueOnce({
      items: [
        {
          id: 'r1',
          report_date: '2026-08-13',
          author_id: 'user_01',
          raw_text: '当日记录',
          summary: '汇总',
          content_status: 'confirmed',
          quality_status: 'ok'
        }
      ],
      total: 1,
      page: 1
    })
    const wrapper = mount(DailyHistoryView, {
      global: { plugins: [createTestI18n()], stubs: { ElSelect: true, ElDatePicker: true } }
    })
    await flushPromises()
    expect(listReports).toHaveBeenCalled()
    expect(wrapper.text()).toContain('日报历史')
    expect(wrapper.text()).toContain('2026/08/13')

    vi.mocked(listReports).mockResolvedValueOnce({ items: [], total: 0, page: 1 })
    const emptyWrapper = mount(DailyHistoryView, {
      global: { plugins: [createTestI18n()], stubs: { ElSelect: true, ElDatePicker: true } }
    })
    await flushPromises()
    expect(emptyWrapper.find('.el-empty__description').text()).toBe('暂无日报记录')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
