import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AttachmentList from '@/components/business/AttachmentList.vue'
import { createTestI18n } from '@/test-utils/setup'

// AttachmentList 组件测试（log-view-optimization 批）：按实体查询已上传附件 + 名称/大小/下载渲染 + 空态。

vi.mock('@/api/attachments', () => ({
  listAttachments: vi.fn(),
  getAttachmentContent: vi.fn()
}))

import { listAttachments } from '@/api/attachments'

describe('AttachmentList', () => {
  it('按 entity_type/entity_id 查询并渲染附件行', async () => {
    vi.mocked(listAttachments).mockResolvedValueOnce({
      items: [
        {
          id: 'att_01',
          original_name: '真空照片.png',
          sha256: 'x',
          description: '',
          mime_type: 'text/plain',
          file_size: 2048,
          created_at: '2026-08-14T09:00:00+08:00',
          updated_at: '2026-08-14T09:00:00+08:00'
        }
      ],
      total: 1,
      page: 1
    })
    const wrapper = mount(AttachmentList, {
      props: { entityType: 'daily_report', entityId: 'report_01' },
      global: { plugins: [createTestI18n()], stubs: { teleport: true } }
    })
    await flushPromises()
    expect(listAttachments).toHaveBeenCalledWith({ entity_type: 'daily_report', entity_id: 'report_01', per_page: 100 })
    expect(wrapper.text()).toContain('真空照片.png')
    expect(wrapper.text()).toContain('2.0KB')
    expect(wrapper.text()).toContain('下载')
  })

  it('无附件时空态', async () => {
    vi.mocked(listAttachments).mockResolvedValueOnce({ items: [], total: 0, page: 1 })
    const wrapper = mount(AttachmentList, {
      props: { entityType: 'log', entityId: 'log_01' },
      global: { plugins: [createTestI18n()], stubs: { teleport: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('暂无附件')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
