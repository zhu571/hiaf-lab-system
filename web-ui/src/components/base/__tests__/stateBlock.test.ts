import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { ElAlert, ElButton, ElEmpty, ElSkeleton } from 'element-plus'
import StateBlock from '@/components/base/StateBlock.vue'
import { createTestI18n } from '@/test-utils/setup'

// StateBlock 组件测试（重构方案 §5 关键路径 #9 + §3.7 契约）：
// loading/error/empty/内容四态优先级 loading > error > empty > 内容；error 态 retry 按钮 emit。
function mountBlock(props: Record<string, unknown> = {}) {
  return mount(StateBlock, {
    props,
    slots: { default: '<div class="content-slot">内容区</div>' },
    global: { plugins: [createTestI18n()] }
  })
}

describe('StateBlock 四态', () => {
  it('loading 态：渲染 el-skeleton，不渲染内容', () => {
    const wrapper = mountBlock({ loading: true, error: null, empty: false })
    expect(wrapper.findComponent(ElSkeleton).exists()).toBe(true)
    expect(wrapper.find('.content-slot').exists()).toBe(false)
  })

  it('error 态：渲染 el-alert（error.message）+ 重试按钮，点击 emit retry；不渲染内容', () => {
    const wrapper = mountBlock({ loading: false, error: { message: '加载失败' }, empty: false })
    const alert = wrapper.findComponent(ElAlert)
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toContain('加载失败')
    expect(wrapper.find('.content-slot').exists()).toBe(false)
    const retry = wrapper.findComponent(ElButton)
    expect(retry.text()).toBe('重试')
    retry.trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('errorText 覆盖 error.message 展示', () => {
    const wrapper = mountBlock({ loading: false, error: { message: '原始错误' }, empty: false, errorText: '自定义错误' })
    expect(wrapper.findComponent(ElAlert).text()).toContain('自定义错误')
    expect(wrapper.findComponent(ElAlert).text()).not.toContain('原始错误')
  })

  it('empty 态：渲染 el-empty，不渲染内容', () => {
    const wrapper = mountBlock({ loading: false, error: null, empty: true })
    expect(wrapper.findComponent(ElEmpty).exists()).toBe(true)
    expect(wrapper.find('.content-slot').exists()).toBe(false)
  })

  it('三态齐备时按 loading > error > empty 优先级渲染', () => {
    const wrapper = mountBlock({ loading: true, error: { message: 'x' }, empty: true })
    expect(wrapper.findComponent(ElSkeleton).exists()).toBe(true)
    expect(wrapper.findComponent(ElAlert).exists()).toBe(false)
    expect(wrapper.findComponent(ElEmpty).exists()).toBe(false)
  })

  it('四态全空时渲染默认插槽内容', () => {
    const wrapper = mountBlock()
    expect(wrapper.find('.content-slot').text()).toBe('内容区')
  })
})
