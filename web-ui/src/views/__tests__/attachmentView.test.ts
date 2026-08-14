import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AttachmentView from '@/views/AttachmentView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { Attachment } from '@/api/attachments'

// AttachmentView 页面测试（测试方案 §3.2 🟡）：附件列表 + 权限显隐（viewer 无上传/绑定/删除）+ 空态。

vi.mock('@/api/attachments', () => ({
  ATTACHMENT_ENTITY_TYPES: [],
  listAttachments: vi.fn(),
  getAttachmentContent: vi.fn(),
  addAttachmentLink: vi.fn(),
  deleteAttachment: vi.fn(),
  uploadAttachment: vi.fn()
}))

import { listAttachments } from '@/api/attachments'

function makeAttachment(overrides: Partial<Attachment> = {}): Attachment {
  return {
    id: 'att_01',
    original_name: '数据表.xlsx',
    sha256: 'e2e-attachment-sha256',
    description: '',
    mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    file_size: 2048,
    uploaded_by: 'user_01',
    created_at: '2026-01-05T10:00:00+08:00',
    updated_at: '2026-01-05T10:00:00+08:00',
    ...overrides
  }
}

async function mountView(role = 'member') {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore(pinia).user = {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
  const wrapper = mount(AttachmentView, {
    global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true, ElSelect: true, ElUpload: true } }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listAttachments).mockReset().mockResolvedValue({ items: [], total: 0, page: 1 })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AttachmentView 附件列表', () => {
  it('member：附件卡片渲染（名称/大小/下载）与绑定/删除入口；空态 el-empty', async () => {
    vi.mocked(listAttachments).mockResolvedValue({ items: [makeAttachment()], total: 1, page: 1 })
    const wrapper = await mountView('member')
    expect(wrapper.find('.att-card').exists()).toBe(true)
    expect(wrapper.text()).toContain('数据表.xlsx')
    expect(wrapper.text()).toContain('下载')
    const actionButtons = wrapper.findAll('button').map((b) => b.text().trim())
    expect(actionButtons).toEqual(expect.arrayContaining(['绑定', '删除']))
    vi.mocked(listAttachments).mockResolvedValue({ items: [], total: 0, page: 1 })
    const emptyWrapper = await mountView('member')
    expect(emptyWrapper.find('.el-empty__description').text()).toBe('暂无附件')
  })

  it('viewer：无上传区，卡片仅下载入口；加载失败 StateBlock 错误 + 重试', async () => {
    vi.mocked(listAttachments)
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce({ items: [makeAttachment()], total: 1, page: 1 })
    const wrapper = await mountView('viewer')
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('附件加载失败')
    await wrapper.find('.state-block-retry').trigger('click')
    await flushPromises()
    expect(listAttachments).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.upload-panel').exists()).toBe(false)
    expect(wrapper.text()).toContain('下载')
    // viewer 无绑定/删除按钮（筛选提示文案含「绑定」字样，按按钮集断言）
    const actionButtons = wrapper.findAll('button').map((b) => b.text().trim())
    expect(actionButtons).not.toContain('绑定')
    expect(actionButtons).not.toContain('删除')
  })
})
