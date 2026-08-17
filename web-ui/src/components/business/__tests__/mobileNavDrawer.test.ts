import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MobileNavDrawer from '@/components/business/MobileNavDrawer.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { UserInfo } from '@/api/auth'

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

const admin: UserInfo = {
  id: 'u1',
  username: 'admin',
  display_name: 'Admin',
  role: 'admin',
  must_change_password: false,
  created_at: '2026-01-01T00:00:00+08:00',
  disabled: false,
  language: 'zh'
}

describe('MobileNavDrawer', () => {
  it('admin 可达全部 NAV_ITEMS，系统组包含 R2 缺失的 7 项', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore(pinia).user = admin
    const wrapper = mount(MobileNavDrawer, {
      props: { modelValue: true },
      global: {
        plugins: [createTestI18n(), pinia],
        stubs: {
          ElDrawer: { template: '<div class="drawer-stub"><slot /></div>' },
          RouterLink: { props: ['to'], template: '<a class="drawer-link" :href="to"><slot /></a>' },
          NotificationCenter: true
        }
      }
    })
    const paths = wrapper.findAll('.drawer-link').map((link) => link.attributes('href'))
    expect(paths).toHaveLength(15)
    expect(paths).toEqual(expect.arrayContaining([
      '/gas-control',
      '/instrument-measure',
      '/sensors',
      '/admin/users',
      '/alerts',
      '/audit',
      '/manual'
    ]))
  })
})
