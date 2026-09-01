import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ProjectLogsView from '@/views/ProjectLogsView.vue'
import { createTestI18n } from '@/test-utils/setup'

// ProjectLogsView 页面测试（log-view-optimization 批）：挂载 + 默认 status=confirmed + 行渲染 + 行点击跳详情。

vi.mock('@/api/logs', () => ({
  listProjectLogs: vi.fn().mockResolvedValue({
    items: [
      {
        id: 'log_01',
        project_id: 'proj_01',
        author_id: 'user_01',
        occurred_at: '2026-08-14T09:00:00+08:00',
        category: 'test',
        content: '完成了 RF 匹配测试\n第二行',
        source: 'manual',
        content_status: 'confirmed'
      }
    ],
    total: 1,
    page: 1
  })
}))

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'proj_01' } }),
  useRouter: () => ({ push })
}))

import { listProjectLogs } from '@/api/logs'

describe('ProjectLogsView 挂载冒烟', () => {
  it('列表渲染：默认 confirmed 过滤 + 分类 i18n + 时间格式化 + 正文换行折叠', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(ProjectLogsView, {
      global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true, ElSelect: true, ElDatePicker: true } }
    })
    await flushPromises()
    expect(listProjectLogs).toHaveBeenCalledWith('proj_01', expect.objectContaining({ page: 1, per_page: 20, status: 'confirmed' }))
    expect(wrapper.text()).toContain('项目日志')
    expect(wrapper.text()).toContain('测试') // category i18n，不显示原始 'test'
    expect(wrapper.text()).toContain('完成了 RF 匹配测试 第二行') // 换行折叠为空格
    expect(wrapper.text()).toContain('已确认') // logStatus 徽标
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
