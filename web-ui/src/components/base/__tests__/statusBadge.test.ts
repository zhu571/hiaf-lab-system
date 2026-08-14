import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ElTag } from 'element-plus'
import StatusBadge from '@/components/base/StatusBadge.vue'
import { createTestI18n } from '@/test-utils/setup'
import type { StatusDomain } from '@/utils/statusMeta'

// R4-7 定稿（测试方案 §8.4）：T0 示范用例改造为注册表驱动（重构 S3 顺手改造，断言意图不变）——
// 已知 domain/value 渲染正确 tone + i18n label、未注册降级原文 + warn。
// 行为兼容核对（美术 §3.8）：原 3 组硬编码映射 active/published/confirmed/resolved→success、
// draft/candidate/open→warning、archived/closed/locked→info 全部保留。
function mountBadge(domain: StatusDomain | undefined, value: string) {
  return mount(StatusBadge, {
    props: { domain, value },
    global: { plugins: [createTestI18n()] }
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('StatusBadge（注册表驱动）', () => {
  it('success 组：active/published/confirmed/resolved 映射为 success 类型并渲染 i18n label', () => {
    const cases: Array<[StatusDomain | undefined, string]> = [
      ['runStatus', 'active'],
      ['experienceStatus', 'published'],
      ['reportStatus', 'confirmed'],
      ['issueStatus', 'resolved'],
      [undefined, 'active']
    ]
    for (const [domain, value] of cases) {
      const wrapper = mountBadge(domain, value)
      expect(wrapper.findComponent(ElTag).props('type'), `value=${value} 应为 success`).toBe('success')
      const label = wrapper.text()
      expect(label, `value=${value} 应为 i18n label`).not.toBe(value)
      expect(label).toBeTruthy()
    }
  })

  it('warning 组与 info 组：draft/candidate/open → warning；archived/closed/locked → info', () => {
    for (const [domain, value] of [
      ['reportStatus', 'draft'],
      ['experienceStatus', 'candidate'],
      ['issueStatus', 'open']
    ] as Array<[StatusDomain, string]>) {
      const wrapper = mountBadge(domain, value)
      expect(wrapper.findComponent(ElTag).props('type'), `value=${value} 应为 warning`).toBe('warning')
      expect(wrapper.text()).not.toBe(value)
    }
    for (const [domain, value] of [
      ['experienceStatus', 'archived'],
      ['issueStatus', 'closed'],
      ['reportStatus', 'locked']
    ] as Array<[StatusDomain, string]>) {
      const wrapper = mountBadge(domain, value)
      expect(wrapper.findComponent(ElTag).props('type'), `value=${value} 应为 info`).toBe('info')
      expect(wrapper.text()).not.toBe(value)
    }
  })

  it('未注册值兜底（M4）：tone 落 info、label 显示原文（下划线替换为空格）、console.warn 提醒登记', () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const wrapper = mountBadge('runStatus', 'in_progress_future')
    expect(wrapper.findComponent(ElTag).props('type')).toBe('info')
    expect(wrapper.text()).toBe('in progress future')
    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining('[statusMeta]'))
  })
})
