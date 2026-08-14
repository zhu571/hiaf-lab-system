import { describe, it, expect, vi, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ExperiencesView from '@/views/ExperiencesView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useProjectStore } from '@/stores/project'

// ExperiencesView 页面测试（测试方案 §3.2 🟢 smoke）：挂载 + 三列看板渲染 + 空列提示。

vi.mock('@/api/experiences', () => ({
  listExperiences: vi.fn(),
  createExperience: vi.fn(),
  publishExperience: vi.fn(),
  archiveExperience: vi.fn()
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([
    { id: 'proj_01', code: 'P01', name: '低温靶项目', short_name: '低温靶', description: '', status: 'active', visibility: 'internal' }
  ])
}))

import { listExperiences } from '@/api/experiences'

describe('ExperiencesView 挂载冒烟', () => {
  it('三列看板（待审核/已发布/已归档）渲染，空列显示提示', async () => {
    vi.mocked(listExperiences).mockResolvedValue({
      items: [{ id: 'exp_01', title: '真空腔体检漏经验', status: 'published', content: '检漏步骤', tags: [], author_id: 'user_01' }],
      total: 1,
      page: 1,
      per_page: 20
    })
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(ExperiencesView, {
      global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true, ElSelect: true } }
    })
    await flushPromises()
    expect(listExperiences).toHaveBeenCalled()
    expect(wrapper.text()).toContain('经验库')
    const columns = wrapper.findAll('.column')
    expect(columns).toHaveLength(3)
    expect(wrapper.text()).toContain('真空腔体检漏经验')
    // 已发布列有内容，其余两列空态
    expect(wrapper.findAll('.empty-hint')).toHaveLength(2)
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})
