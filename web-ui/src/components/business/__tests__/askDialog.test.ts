import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AskDialog from '@/components/business/AskDialog.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAskDialog } from '@/composables/useAskDialog'
import type { AskChatResponse, AskHistoryItem } from '@/api/ask'

// AskDialog 组件测试（测试方案 §3.2 🔴 深测）：
// 提问失败 .ask-error、历史列表加载、提问中 loading 防重。
// 抽屉开关经 useAskDialog 模块级单例（AppLayout 持有渲染，测试直接操作单例打开）。

vi.mock('@/api/ask', () => ({
  askChat: vi.fn(),
  askHistory: vi.fn(),
  askHistoryDetail: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

import { askChat, askHistory, askHistoryDetail } from '@/api/ask'

function makeAskResponse(overrides: Partial<AskChatResponse> = {}): AskChatResponse {
  return {
    id: 'ask_01',
    question: '最近的测试数据',
    answer: '查询到 3 条记录',
    sql: 'SELECT 1',
    table_name: 'test_data',
    columns: ['id', 'value'],
    rows: [],
    row_count: 0,
    truncated: false,
    duration_ms: 100,
    created_at: '2026-01-04T10:00:00+08:00',
    ...overrides
  }
}

async function mountDialog() {
  const wrapper = mount(AskDialog, {
    global: { plugins: [createTestI18n()], stubs: { teleport: true } }
  })
  await flushPromises()
  return wrapper
}

function chatInput(wrapper: Awaited<ReturnType<typeof mountDialog>>) {
  const input = wrapper.find('textarea')
  return input
}

function sendButton(wrapper: Awaited<ReturnType<typeof mountDialog>>) {
  return wrapper.findAll('button').find((b) => b.text().trim() === '发送')!
}

beforeEach(() => {
  vi.mocked(askChat).mockReset()
  vi.mocked(askHistory).mockReset()
  vi.mocked(askHistoryDetail).mockReset()
  useAskDialog().askOpen.value = true
})

afterEach(() => {
  useAskDialog().askOpen.value = false
  vi.restoreAllMocks()
})

describe('AskDialog 提问', () => {
  it('提问失败：.ask-error 显示后端错误文案，历史回合保留问题原文', async () => {
    vi.mocked(askChat).mockRejectedValue(new Error('upstream_error: interpret 服务不可达'))
    const wrapper = await mountDialog()
    await chatInput(wrapper).setValue('昨天压强的最大值？')
    await sendButton(wrapper).trigger('click')
    await flushPromises()
    expect(askChat).toHaveBeenCalledWith('昨天压强的最大值？', expect.any(String))
    const error = wrapper.find('.ask-error')
    expect(error.exists()).toBe(true)
    expect(error.text()).toContain('upstream_error')
    expect(wrapper.text()).toContain('昨天压强的最大值？')
    expect(wrapper.find('.chat-loading').exists()).toBe(false)
  })

  it('提问成功：回合渲染结果面板（AskResultPanel），输入框清空', async () => {
    vi.mocked(askChat).mockResolvedValue(makeAskResponse({ answer: '查询到 3 条记录' }))
    const wrapper = await mountDialog()
    await chatInput(wrapper).setValue('最近压力数据')
    await sendButton(wrapper).trigger('click')
    await flushPromises()
    expect(wrapper.find('.ask-error').exists()).toBe(false)
    expect(wrapper.find('.result-panel').exists()).toBe(true)
    expect(wrapper.text()).toContain('查询到 3 条记录')
    expect(chatInput(wrapper).element.value).toBe('')
  })

  it('提问中 loading 防重：发送按钮 loading + textarea 禁用，二次点击不重复请求', async () => {
    let resolveChat: (v: AskChatResponse) => void = () => {}
    vi.mocked(askChat).mockImplementation(
      () => new Promise<AskChatResponse>((resolve) => (resolveChat = resolve))
    )
    const wrapper = await mountDialog()
    await chatInput(wrapper).setValue('压力曲线')
    await sendButton(wrapper).trigger('click')
    await flushPromises()
    const btn = sendButton(wrapper)
    expect(btn.classes()).toContain('is-loading')
    expect(chatInput(wrapper).attributes('disabled')).toBeDefined()
    await btn.trigger('click')
    expect(askChat).toHaveBeenCalledTimes(1)
    resolveChat(makeAskResponse())
    await flushPromises()
    expect(wrapper.find('.chat-loading').exists()).toBe(false)
  })

  it('空输入/纯空格不发请求，发送按钮禁用', async () => {
    const wrapper = await mountDialog()
    expect(sendButton(wrapper).attributes('disabled')).toBeDefined()
    await chatInput(wrapper).setValue('   ')
    await sendButton(wrapper).trigger('click')
    expect(askChat).not.toHaveBeenCalled()
    expect(sendButton(wrapper).attributes('disabled')).toBeDefined()
  })
})

describe('AskDialog 历史', () => {
  it('切历史 tab：askHistory 加载列表——空历史显示空态；有条目时渲染并支持明细跳转', async () => {
    // 场景 1：空历史 → 空态文案
    vi.mocked(askHistory).mockResolvedValue({ items: [], total: 0, page: 1, per_page: 20 })
    const wrapper = await mountDialog()
    const historyTab = wrapper.findAll('.el-tabs__item').find((t) => t.text().trim() === '历史')!
    await historyTab.trigger('click')
    await flushPromises()
    expect(askHistory).toHaveBeenCalledWith({ page: 1, per_page: 20 })
    expect(wrapper.text()).toContain('暂无问答历史')

    // 场景 2：有条目 → 渲染列表；点击条目加载明细（askHistoryDetail）
    const item: AskHistoryItem = {
      id: 'h_01',
      user_id: 'u1',
      request_id: 'req_1',
      question: '上周 RF 匹配',
      answer: '',
      sql_text: '',
      table_name: 'rf_matching_records',
      columns: [],
      row_count: 5,
      duration_ms: 50,
      model: 'deepseek',
      created_at: '2026-01-04T10:00:00+08:00'
    }
    vi.mocked(askHistory).mockResolvedValue({ items: [item], total: 1, page: 1, per_page: 20 })
    vi.mocked(askHistoryDetail).mockResolvedValue({ ...item, rows: [] })
    const wrapper2 = await mountDialog()
    const historyTab2 = wrapper2.findAll('.el-tabs__item').find((t) => t.text().trim() === '历史')!
    await historyTab2.trigger('click')
    await flushPromises()
    expect(wrapper2.find('.history-item').exists()).toBe(true)
    expect(wrapper2.text()).toContain('上周 RF 匹配')
    await wrapper2.find('.history-item').trigger('click')
    await flushPromises()
    expect(askHistoryDetail).toHaveBeenCalledWith('h_01')
    expect(wrapper2.find('.history-detail').exists()).toBe(true)
  })
})
