import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import DailyReportDetailView from '@/views/DailyReportDetailView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { ApiError } from '@/api/client'

// DailyReportDetailView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 详情渲染 + 三态区分。
// log-view-optimization 批：加载改 StateBlock（仅 404 归 notFound，其余错误走错误态 + 重试），
// 关联日志换 ResponsiveTable，补 quality_status 与附件区（AttachmentList → listAttachments mock）。

vi.mock('@/api/logs', () => ({
  getReport: vi.fn()
}))

vi.mock('@/api/attachments', () => ({
  listAttachments: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1 })
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([])
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'report_01' } }),
  useRouter: () => ({ push: vi.fn() })
}))

import { getReport } from '@/api/logs'

function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(DailyReportDetailView, {
    global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true, ElSelect: true } }
  })
}

describe('DailyReportDetailView 挂载冒烟', () => {
  it('详情渲染（原文 + 项目日志 + 质量状态）', async () => {
    vi.mocked(getReport).mockResolvedValueOnce({
      id: 'report_01',
      report_date: '2026-08-13',
      author_id: 'user_01',
      author_name: 'haofan',
      raw_text: '当日原文记录',
      summary: '汇总',
      content_status: 'confirmed',
      quality_status: 'passed',
      logs: []
    })
    const wrapper = mountView()
    await flushPromises()
    expect(getReport).toHaveBeenCalledWith('report_01')
    expect(wrapper.text()).toContain('日报详情')
    expect(wrapper.text()).toContain('当日原文记录')
    expect(wrapper.text()).toContain('项目化日志')
    expect(wrapper.text()).toContain('通过')
  })

  it('404 归 notFound 空态；网络错误走错误态 + 重试', async () => {
    vi.mocked(getReport).mockRejectedValueOnce(new ApiError('not found', 'not_found', { status: 404 }))
    const notFound = mountView()
    await flushPromises()
    expect(notFound.find('.el-empty__description').text()).toBe('日报不存在或无权查看')

    vi.mocked(getReport).mockRejectedValueOnce(new ApiError('网络异常', 'network'))
    const failed = mountView()
    await flushPromises()
    expect(failed.find('.el-alert').exists()).toBe(true)
    expect(failed.text()).toContain('重试')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
