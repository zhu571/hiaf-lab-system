import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import RFMatchingView from '@/views/RFMatchingView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { RFMatchingRecord } from '@/api/rfmatch'

// RFMatchingView 页面测试（测试方案 §3.2 🟡）：匹配记录列表 + 错误态 + viewer 权限隐藏写入口。

vi.mock('@/api/rfmatch', () => ({
  listRFMatching: vi.fn(),
  createRFMatching: vi.fn(),
  deleteRFMatching: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'proj_01' } })
}))

import { listRFMatching } from '@/api/rfmatch'

function makeRecord(overrides: Partial<RFMatchingRecord> = {}): RFMatchingRecord {
  return {
    id: 'rf_01',
    project_id: 'proj_01',
    device: 'rf_carpet',
    frequency_mhz: 2,
    status: 'pass',
    is_void: false,
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
  const wrapper = mount(RFMatchingView, {
    global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true, ElSelect: true } }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listRFMatching).mockReset().mockResolvedValue({ items: [], total: 0, page: 1 })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RFMatchingView 匹配记录', () => {
  it('member：记录列表渲染（设备/频率/状态）+ 作废入口；空态 el-empty', async () => {
    vi.mocked(listRFMatching).mockResolvedValue({ items: [makeRecord()], total: 1, page: 1 })
    const wrapper = await mountView('member')
    expect(listRFMatching).toHaveBeenCalledWith('proj_01', expect.any(Object))
    expect(wrapper.text()).toContain('rf_carpet')
    expect(wrapper.text()).toContain('pass')
    expect(wrapper.text()).toContain('作废')
    vi.mocked(listRFMatching).mockResolvedValue({ items: [], total: 0, page: 1 })
    const emptyWrapper = await mountView('member')
    expect(emptyWrapper.find('.el-empty__description').text()).toBe('暂无记录')
  })

  it('错误态：加载失败 StateBlock 错误 + 重试；viewer 隐藏新增/作废入口', async () => {
    vi.mocked(listRFMatching)
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce({ items: [makeRecord()], total: 1, page: 1 })
    const wrapper = await mountView('viewer')
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    await wrapper.findAll('button').find((b) => b.text().trim() === '重试')!.trigger('click')
    await flushPromises()
    expect(listRFMatching).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('新增记录')
    expect(wrapper.findAll('button').filter((b) => b.text().trim() === '作废')).toHaveLength(0)
  })
})
