import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAgentPending } from '@/composables/useAgentPending'
import { useAuthStore } from '@/stores/auth'
import type { UserInfo } from '@/api/auth'

// useAgentPending 单例测试（结构改版 R2 §3.2，自 AppLayout 等价抽取）：
// 拉取口径（pending_review + per_page 1 取 total）、canReviewAgent 门控、失败静默降级。

vi.mock('@/api/agent', () => ({
  listAgentCandidates: vi.fn()
}))

import { listAgentCandidates } from '@/api/agent'

function makeUser(role: string): UserInfo {
  return {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(listAgentCandidates).mockReset()
  // 模块级单例计数跨用例共享，逐个用例前置清零防串扰
  useAgentPending().agentPending.value = 0
})

describe('useAgentPending 待审计数单例', () => {
  it('admin：refresh 拉取 pending_review total 并写入共享计数', async () => {
    useAuthStore().user = makeUser('admin')
    vi.mocked(listAgentCandidates).mockResolvedValue({ items: [], total: 7, page: 1, per_page: 1 })
    const { agentPending, refreshAgentPending } = useAgentPending()

    await refreshAgentPending()

    expect(listAgentCandidates).toHaveBeenCalledWith({ status: 'pending_review', page: 1, per_page: 1 })
    expect(agentPending.value).toBe(7)
    // 单例语义：另一次 useAgentPending() 返回同一计数 ref
    expect(useAgentPending().agentPending.value).toBe(7)
  })

  it('viewer：无审核权限不发起请求，计数保持', async () => {
    useAuthStore().user = makeUser('viewer')
    const { agentPending, refreshAgentPending } = useAgentPending()

    await refreshAgentPending()

    expect(listAgentCandidates).not.toHaveBeenCalled()
    expect(agentPending.value).toBe(0)
  })

  it('拉取失败静默降级：不抛错、计数保持原值', async () => {
    useAuthStore().user = makeUser('maintainer')
    vi.mocked(listAgentCandidates).mockResolvedValueOnce({ items: [], total: 3, page: 1, per_page: 1 })
    const { agentPending, refreshAgentPending } = useAgentPending()
    await refreshAgentPending()
    expect(agentPending.value).toBe(3)

    vi.mocked(listAgentCandidates).mockRejectedValueOnce(new Error('network'))
    await expect(refreshAgentPending()).resolves.toBeUndefined()
    expect(agentPending.value).toBe(3)
  })
})
