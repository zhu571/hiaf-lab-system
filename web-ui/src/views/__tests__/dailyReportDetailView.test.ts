import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import DailyReportDetailView from '@/views/DailyReportDetailView.vue'
import { createTestI18n } from '@/test-utils/setup'

// DailyReportDetailView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 详情渲染 + notFound 空态。

vi.mock('@/api/logs', () => ({
  getReport: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'report_01' } })
}))

import { getReport } from '@/api/logs'

describe('DailyReportDetailView 挂载冒烟', () => {
  it('详情渲染（原文 + 项目日志），报告不存在时 notFound 空态', async () => {
    vi.mocked(getReport).mockResolvedValueOnce({
      id: 'report_01',
      report_date: '2026-08-13',
      author_id: 'user_01',
      author_name: 'haofan',
      raw_text: '当日原文记录',
      summary: '汇总',
      content_status: 'confirmed',
      quality_status: 'ok',
      logs: []
    })
    const wrapper = mount(DailyReportDetailView, {
      global: { plugins: [createTestI18n()], stubs: { teleport: true, ElSelect: true } }
    })
    await flushPromises()
    expect(getReport).toHaveBeenCalledWith('report_01')
    expect(wrapper.text()).toContain('日报详情')
    expect(wrapper.text()).toContain('当日原文记录')
    expect(wrapper.text()).toContain('项目化日志')

    vi.mocked(getReport).mockRejectedValueOnce(new Error('not found'))
    const notFound = mount(DailyReportDetailView, {
      global: { plugins: [createTestI18n()], stubs: { teleport: true, ElSelect: true } }
    })
    await flushPromises()
    expect(notFound.find('.el-empty__description').text()).toBe('日报不存在或无权查看')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
