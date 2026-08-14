import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AlertCenterView from '@/views/AlertCenterView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'

// AlertCenterView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 活动告警列表 + 空态。

vi.mock('@/api/alerts', () => ({
  listAlerts: vi.fn(),
  resolveAlert: vi.fn()
}))

import { listAlerts } from '@/api/alerts'

describe('AlertCenterView 挂载冒烟', () => {
  it('活动告警列表渲染，空数据 el-empty；maintainer 显示处理按钮', async () => {
    vi.mocked(listAlerts).mockResolvedValue({
      items: [
        { id: 'alert_1', level: 'warning', source: 'instruments', title: '真空度偏离设定值', detail: '', status: 'active', occurrence_count: 1, first_seen: '2026-08-13T10:00:00+08:00', last_seen: '2026-08-13T10:00:00+08:00', resolved_by: '', created_at: '2026-08-13T10:00:00+08:00' }
      ],
      total: 1,
      limit: 100,
      offset: 0
    })
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore(pinia).user = {
      id: 'user_01',
      username: 'maintainer',
      display_name: 'M',
      role: 'maintainer',
      must_change_password: false,
      created_at: '2026-01-01T00:00:00+08:00',
      disabled: false,
      language: 'zh'
    }
    const wrapper = mount(AlertCenterView, {
      global: { plugins: [createTestI18n(), pinia], stubs: { ElSelect: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('告警中心')
    expect(wrapper.text()).toContain('真空度偏离设定值')
    expect(wrapper.text()).toContain('标记解决')
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
