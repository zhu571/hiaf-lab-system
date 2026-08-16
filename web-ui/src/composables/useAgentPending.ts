import { ref } from 'vue'
import { listAgentCandidates } from '@/api/agent'
import { useAuthStore } from '@/stores/auth'

// C11 待审候选计数单例（结构改版 R2 §3.2，自 AppLayout 原 :114-142 等价抽取）：
// 计数为模块级共享 ref——侧栏 badge（AppLayout）与通知中心待审组（NotificationCenter）
// 读同一计数，轮询仍只由 AppLayout 发起一处（usePolling 30s，行为与抽取前一致）；
// NotificationCenter 不新增轮询，仅复用计数并在面板打开时手动 refresh。
// 仅 admin/maintainer（与 ListCandidates 后端权限一致）拉取；失败静默降级不打断导航。
const agentPending = ref(0)

export function useAgentPending() {
  const auth = useAuthStore()

  async function refreshAgentPending() {
    if (!auth.canReviewAgent || document.hidden) return
    try {
      const data = await listAgentCandidates({ status: 'pending_review', page: 1, per_page: 1 })
      agentPending.value = data.total
    } catch {
      // 徽章拉取失败静默降级，下一轮轮询再试
    }
  }

  return { agentPending, refreshAgentPending }
}
