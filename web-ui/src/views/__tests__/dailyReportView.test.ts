import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import DailyReportView from '@/views/DailyReportView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { DailyReport, LogItem } from '@/api/logs'

// DailyReportView 页面测试（测试方案 §3.2 🟡）：todayReport 自动创建后
// raw_text 落位（覆写竞态场景的单元级固化）、保存成功流、AI 整理草稿确认。

vi.mock('@/api/logs', () => ({
  todayReport: vi.fn(),
  updateReportRawText: vi.fn(),
  submitReport: vi.fn(),
  aiParseReport: vi.fn(),
  createLog: vi.fn(),
  updateLog: vi.fn()
}))

vi.mock('@/api/attachments', () => ({
  uploadAttachment: vi.fn()
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([])
}))

import { todayReport, updateReportRawText, aiParseReport, createLog } from '@/api/logs'

function makeReport(overrides: Partial<DailyReport> = {}): DailyReport {
  return {
    id: 'report_01',
    report_date: '2026-08-14',
    author_id: 'user_01',
    author_name: 'haofan',
    raw_text: '',
    summary: '',
    content_status: 'draft',
    quality_status: 'ok',
    logs: [],
    ...overrides
  }
}

function makeLog(overrides: Partial<LogItem> = {}): LogItem {
  return {
    id: 'log_01',
    project_id: 'proj_01',
    author_id: 'user_01',
    occurred_at: '2026-08-14T09:00:00+08:00',
    category: 'test',
    content: '完成了 RF 匹配测试',
    source: 'manual',
    content_status: 'draft',
    ...overrides
  }
}

async function mountView(role = 'member') {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().user = {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
  const wrapper = mount(DailyReportView, {
    global: {
      plugins: [createTestI18n(), pinia],
      stubs: { teleport: true, ElSelect: true, ElDatePicker: true, ElUpload: true }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(todayReport).mockReset()
  vi.mocked(updateReportRawText).mockReset()
  vi.mocked(aiParseReport).mockReset()
  vi.mocked(createLog).mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('DailyReportView 日报编辑', () => {
  it('挂载：todayReport 自动创建后 raw_text 落位到编辑器（覆写竞态固化）', async () => {
    vi.mocked(todayReport).mockResolvedValue(makeReport({ raw_text: '今天完成了低温靶的抽真空' }))
    const wrapper = await mountView('member')
    expect(todayReport).toHaveBeenCalled()
    const textarea = wrapper.find('textarea')
    expect(textarea.element.value).toBe('今天完成了低温靶的抽真空')
    // 已保存日志行渲染（含状态徽标与确认入口）
  })

  it('保存草稿：updateReportRawText 提交后 toast 成功', async () => {
    vi.mocked(todayReport).mockResolvedValue(makeReport({ raw_text: '' }))
    vi.mocked(updateReportRawText).mockResolvedValue(makeReport({ raw_text: '新内容' }))
    const wrapper = await mountView('member')
    const textarea = wrapper.find('textarea')
    await textarea.setValue('新内容')
    const saveBtn = wrapper.findAll('button').find((b) => b.text().trim() === '保存原文')!
    await saveBtn.trigger('click')
    await flushPromises()
    expect(updateReportRawText).toHaveBeenCalledWith('report_01', '新内容')
  })

  it('AI 整理：草稿行渲染 + 确认单条草稿调 createLog 并刷新', async () => {
    vi.mocked(todayReport).mockResolvedValue(makeReport({ raw_text: '抽真空完成。RF 匹配通过。' }))
    vi.mocked(aiParseReport).mockResolvedValue({
      data: {
        status: 'ok',
        logs: [
          { category: 'test', project_id: 'proj_01', content: '抽真空完成', occurred_at: '2026-08-14T09:00:00+08:00' },
          { category: 'rf', project_id: 'proj_01', content: 'RF 匹配通过', occurred_at: '2026-08-14T09:30:00+08:00' }
        ],
        question: null,
        reason: null
      },
      requestId: 'req_1'
    } as never)
    vi.mocked(createLog).mockResolvedValue(makeLog())
    vi.mocked(todayReport).mockResolvedValue(makeReport({ raw_text: '抽真空完成。RF 匹配通过。' }))
    const wrapper = await mountView('member')
    const aiBtn = wrapper.findAll('button').find((b) => b.text().includes('AI 整理'))!
    await aiBtn.trigger('click')
    await flushPromises()
    expect(aiParseReport).toHaveBeenCalledWith('report_01')
    // 两条草稿渲染（草稿内容在行内 textarea value）
    const draftTexts = wrapper.findAll('textarea').map((t) => t.element.value)
    expect(draftTexts).toEqual(expect.arrayContaining(['抽真空完成', 'RF 匹配通过']))
    // 确认第一条 → createLog
    const confirmButtons = wrapper.findAll('button').filter((b) => b.text().trim() === '确认')
    await confirmButtons[0].trigger('click')
    await flushPromises()
    expect(createLog).toHaveBeenCalledWith(
      'proj_01',
      expect.objectContaining({ daily_report_id: 'report_01', category: 'test', content: '抽真空完成', source: 'agent' })
    )
  })
})
