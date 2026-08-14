import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ElMessage } from 'element-plus'
import { useRunReports } from '../useRunReports'

// 批次详情「关联日报」面板（重构方案 S5：RunDetailView 拆出，行为与拆分前逐字等价）。

const mocks = vi.hoisted(() => ({
  listReports: vi.fn(),
  addReportLink: vi.fn(),
  removeReportLink: vi.fn()
}))

vi.mock('@/api/logs', () => ({
  listReports: mocks.listReports
}))
vi.mock('@/api/runs', () => ({
  addReportLink: mocks.addReportLink,
  removeReportLink: mocks.removeReportLink
}))

const messageMock = ElMessage as unknown as { success: ReturnType<typeof vi.fn> }

const report = (id: string, date: string, summary: string): import('@/api/logs').DailyReport => ({
  id,
  report_date: date,
  summary,
  content_status: 'draft',
  quality_status: 'ok',
  author_id: 'u1',
  raw_text: ''
})

beforeEach(() => {
  vi.clearAllMocks()
  messageMock.success.mockClear()
})

describe('useRunReports 日报候选', () => {
  it('loadReports 以 per_page=50 拉取候选列表', async () => {
    mocks.listReports.mockResolvedValue({ items: [report('r1', '2026-08-14', '今日摘要')] })
    const { reportOptions, reportsLoading, loadReports } = useRunReports('run_1')
    await loadReports()
    expect(mocks.listReports).toHaveBeenCalledWith({ per_page: 50 })
    expect(reportOptions.value).toHaveLength(1)
    expect(reportsLoading.value).toBe(false)
  })

  it('reportLabel：摘要超 24 字截断，空摘要只显示日期', () => {
    const { reportLabel } = useRunReports('run_1')
    expect(reportLabel(report('r1', '2026-08-14', '短'))).toBe('2026-08-14 · 短')
    expect(reportLabel(report('r2', '2026-08-14', '长'.repeat(30)))).toBe(`2026-08-14 · ${'长'.repeat(24)}…`)
    expect(reportLabel(report('r3', '2026-08-14', '  '))).toBe('2026-08-14')
  })
})

describe('useRunReports 关联/解绑', () => {
  it('link：响应 report_ids 为全量列表直接覆盖，成功后清空选择', async () => {
    mocks.addReportLink.mockResolvedValue({ run_id: 'run_1', report_ids: ['r1', 'r2'] })
    const { linkedReportIds, selectedReportId, linking, link } = useRunReports('run_1')
    selectedReportId.value = 'r2'
    await link()
    expect(mocks.addReportLink).toHaveBeenCalledWith('run_1', 'r2')
    expect(linkedReportIds.value).toEqual(['r1', 'r2'])
    expect(selectedReportId.value).toBe('')
    expect(messageMock.success).toHaveBeenCalled()
    expect(linking.value).toBe(false)
  })

  it('link：未选日报时直接返回不调接口', async () => {
    const { link } = useRunReports('run_1')
    await link()
    expect(mocks.addReportLink).not.toHaveBeenCalled()
  })

  it('unlink：删除后覆盖本地关联列表', async () => {
    mocks.removeReportLink.mockResolvedValue({ run_id: 'run_1', report_ids: ['r1'] })
    const { linkedReportIds, unlink } = useRunReports('run_1')
    linkedReportIds.value = ['r1', 'r2']
    await unlink('r2')
    expect(mocks.removeReportLink).toHaveBeenCalledWith('run_1', 'r2')
    expect(linkedReportIds.value).toEqual(['r1'])
  })
})
