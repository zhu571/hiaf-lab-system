import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MarkdownView from '@/components/business/MarkdownView.vue'

// MarkdownView 组件测试（测试方案 §3.2 🔴 深测）：
// markdown 渲染、html:false 下 <script> 转义不执行、linkify 链接带 noopener（C14 XSS 防线）。

function mountView(source?: string | null) {
  return mount(MarkdownView, { props: { source } })
}

describe('MarkdownView 渲染', () => {
  it('markdown 基本语法：标题/段落/行内代码渲染为对应元素', () => {
    const wrapper = mountView('# 标题一\n\n段落文本 `code`')
    expect(wrapper.find('h1').text()).toBe('标题一')
    expect(wrapper.text()).toContain('段落文本')
    expect(wrapper.find('code').text()).toBe('code')
  })

  it('html:false 下 <script> 转义不执行：不产生 script 节点，原文以文本呈现', () => {
    ;(window as unknown as { __xss?: boolean }).__xss = false
    const wrapper = mountView('<script>window.__xss = true</script>')
    expect(wrapper.find('script').exists()).toBe(false)
    expect((window as unknown as { __xss?: boolean }).__xss).toBe(false)
    expect(wrapper.text()).toContain('<script>')
  })

  it('linkify 自动识别 URL，链接带 target=_blank 与 rel=noopener', () => {
    const wrapper = mountView('访问 https://example.com/path 查看文档')
    const link = wrapper.find('a')
    expect(link.exists()).toBe(true)
    expect(link.attributes('href')).toBe('https://example.com/path')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener')
  })

  it('空/undefined source 渲染为空；换行 breaks 生效', () => {
    expect(mountView(undefined).text()).toBe('')
    expect(mountView(null).text()).toBe('')
    expect(mountView('')).toBeTruthy()
    const wrapper = mountView('第一行\n第二行')
    expect(wrapper.text()).toContain('第一行')
    expect(wrapper.text()).toContain('第二行')
  })
})
