import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AuditView from '@/views/AuditView.vue'
import { createTestI18n } from '@/test-utils/setup'

// AuditView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 事件列表 + 空态。

vi.mock('@/api/audit', () => ({
  listAuditEvents: vi.fn(),
  getAudit: vi.fn()
}))

import { listAuditEvents } from '@/api/audit'

describe('AuditView 挂载冒烟', () => {
  it('事件列表渲染（action/用户/时间），空数据 el-empty；按 request_id 查询面板存在', async () => {
    vi.mocked(listAuditEvents).mockResolvedValue({
      items: [
        {
          id: 1,
          request_id: 'req_1',
          username: 'haofan',
          method: 'POST',
          path: '/api/v1/logs',
          action: 'create',
          client_ip: '127.0.0.1',
          status_code: 200,
          actor_type: 'user',
          created_at: '2026-08-13T10:00:00+08:00'
        }
      ],
      total: 1,
      page: 1,
      per_page: 20
    })
    const wrapper = mount(AuditView, {
      global: { plugins: [createTestI18n()], stubs: { ElSelect: true, ElDatePicker: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('审计查询')
    expect(wrapper.text()).toContain('req_1')
    expect(wrapper.text()).toContain('按 request_id 查询')

    vi.mocked(listAuditEvents).mockResolvedValue({ items: [], total: 0, page: 1, per_page: 20 })
    const emptyWrapper = mount(AuditView, {
      global: { plugins: [createTestI18n()], stubs: { ElSelect: true, ElDatePicker: true } }
    })
    await flushPromises()
    expect(emptyWrapper.find('.el-empty__description').text()).toBe('暂无审计记录')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
