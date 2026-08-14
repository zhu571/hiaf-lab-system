import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AgentCandidatesView from '@/views/AgentCandidatesView.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { AgentCandidate } from '@/api/agent'

// AgentCandidatesView 页面测试（测试方案 §3.2 🟡）：候选列表、approve/reject
// 按钮与确认流（ElMessageBox.confirm）、空态。

vi.mock('@/api/agent', () => ({
  listAgentCandidates: vi.fn(),
  approveCandidate: vi.fn(),
  rejectCandidate: vi.fn(),
  getCandidateTrace: vi.fn()
}))

import { listAgentCandidates, approveCandidate, rejectCandidate } from '@/api/agent'

function makeCandidate(overrides: Partial<AgentCandidate> = {}): AgentCandidate {
  return {
    id: 'cand_01',
    task_id: 'task_01',
    action_type: 'issue',
    status: 'pending_review',
    payload: { title: '真空度异常' },
    agent_confidence: 0.92,
    created_at: '2026-01-05T10:00:00+08:00',
    ...overrides
  }
}

async function mountView() {
  const wrapper = mount(AgentCandidatesView, {
    global: {
      plugins: [createTestI18n()],
      stubs: { teleport: true, RouterLink: { template: '<a><slot /></a>' }, ElSelect: true }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listAgentCandidates).mockReset()
  vi.mocked(approveCandidate).mockReset()
  vi.mocked(rejectCandidate).mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('AgentCandidatesView 候选列表', () => {
  it('挂载加载候选列表：条目渲染标题/置信度/审批按钮', async () => {
    vi.mocked(listAgentCandidates).mockResolvedValue({
      items: [makeCandidate(), makeCandidate({ id: 'cand_02', status: 'approved', payload: { title: '经验候选' } })],
      total: 2,
      page: 1,
      per_page: 20
    })
    const wrapper = await mountView()
    expect(listAgentCandidates).toHaveBeenCalledWith(expect.objectContaining({ status: 'pending_review' }))
    expect(wrapper.text()).toContain('真空度异常')
    // pending_review 行显示 批准/拒绝；approved 行不显示
    const approveBtns = wrapper.findAll('button').filter((b) => b.text().trim() === '批准')
    const rejectBtns = wrapper.findAll('button').filter((b) => b.text().trim() === '拒绝')
    expect(approveBtns.length).toBe(1)
    expect(rejectBtns.length).toBe(1)
  })

  it('空态：无候选时 el-empty 显示', async () => {
    vi.mocked(listAgentCandidates).mockResolvedValue({ items: [], total: 0, page: 1, per_page: 20 })
    const wrapper = await mountView()
    expect(wrapper.find('.el-empty__description').text()).toContain('暂无候选')
  })

  it('审批确认流：approve 先 confirm 再调 approveCandidate 并刷新列表', async () => {
    vi.mocked(listAgentCandidates).mockResolvedValue({
      items: [makeCandidate()],
      total: 1,
      page: 1,
      per_page: 20
    })
    vi.mocked(approveCandidate).mockResolvedValue(makeCandidate({ status: 'approved' }))
    const wrapper = await mountView()
    const approveBtn = wrapper.findAll('button').find((b) => b.text().trim() === '批准')!
    await approveBtn.trigger('click')
    await flushPromises()
    expect(approveCandidate).toHaveBeenCalledWith('cand_01')
    // 成功后刷新列表
    expect(listAgentCandidates).toHaveBeenCalledTimes(2)
    // reject 入口：点击打开拒绝对话框 → 填写理由 → rejectCandidate
    vi.mocked(rejectCandidate).mockResolvedValue(makeCandidate({ status: 'rejected' }))
    vi.mocked(listAgentCandidates).mockResolvedValue({ items: [makeCandidate()], total: 1, page: 1, per_page: 20 })
    const rejectBtn = wrapper.findAll('button').find((b) => b.text().trim() === '拒绝')!
    await rejectBtn.trigger('click')
    await flushPromises()
    const reasonInput = wrapper.find('.el-dialog textarea')
    expect(reasonInput.exists()).toBe(true)
    await reasonInput.setValue('重复问题')
    const confirmReject = wrapper.findAll('.el-dialog button').find((b) => b.text().trim() === '确认拒绝')!
    await confirmReject.trigger('click')
    await flushPromises()
    expect(rejectCandidate).toHaveBeenCalledWith('cand_01', '重复问题')
  })
})
