import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LogDetailView from '@/views/LogDetailView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { ApiError } from '@/api/client'

// LogDetailView 页面测试（log-view-optimization 批）：详情渲染 + 附件区查询 + 404/网络错误分流。

vi.mock('@/api/logs', () => ({
  getLog: vi.fn(),
  listReportsByLog: vi.fn().mockResolvedValue([])
}))

vi.mock('@/api/issues', () => ({
  listIssuesByLog: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1 })
}))

vi.mock('@/api/attachments', () => ({
  listAttachments: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1 })
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([
    { id: 'proj_01', code: 'P01', name: '低温靶项目', short_name: '低温靶', description: '', status: 'active', visibility: 'internal' }
  ])
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'log_01' } }),
  useRouter: () => ({ push: vi.fn(), back: vi.fn() })
}))

import { getLog } from '@/api/logs'
import { listAttachments } from '@/api/attachments'

function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(LogDetailView, {
    global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true, ElSelect: true } }
  })
}

describe('LogDetailView 挂载冒烟', () => {
  it('详情渲染：正文/元信息/分类 i18n + 附件区按 entity_type=log 查询', async () => {
    vi.mocked(getLog).mockResolvedValueOnce({
      id: 'log_01',
      project_id: 'proj_01',
      author_id: 'user_01',
      occurred_at: '2026-08-14T09:00:00+08:00',
      category: 'cryo',
      content: '降温到 4K 并稳定',
      raw_snippet: '4K 稳定',
      source: 'agent',
      content_status: 'confirmed',
      created_at: '2026-08-14T09:05:00+08:00'
    })
    const wrapper = mountView()
    await flushPromises()
    expect(getLog).toHaveBeenCalledWith('log_01')
    expect(listAttachments).toHaveBeenCalledWith(expect.objectContaining({ entity_type: 'log', entity_id: 'log_01' }))
    expect(wrapper.text()).toContain('日志详情')
    expect(wrapper.text()).toContain('降温到 4K 并稳定')
    expect(wrapper.text()).toContain('低温') // category i18n
    expect(wrapper.text()).toContain('低温靶项目') // 项目名解析 + 跳转入口
    expect(wrapper.text()).toContain('已确认')
  })

  it('404 归 notFound 空态；网络错误走错误态 + 重试', async () => {
    vi.mocked(getLog).mockRejectedValueOnce(new ApiError('not found', 'not_found', { status: 404 }))
    const notFound = mountView()
    await flushPromises()
    expect(notFound.find('.el-empty__description').text()).toBe('日志不存在或无权查看')

    vi.mocked(getLog).mockRejectedValueOnce(new ApiError('网络异常', 'network'))
    const failed = mountView()
    await flushPromises()
    expect(failed.find('.el-alert').exists()).toBe(true)
    expect(failed.text()).toContain('重试')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
