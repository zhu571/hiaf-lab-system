import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { ElEmpty, ElSkeleton } from 'element-plus'
import ListPage from '@/components/base/ListPage.vue'
import { createTestI18n } from '@/test-utils/setup'

// ListPage 组件测试（结构改版 R3 §4.1/§4.4 契约）：
// toolbar（title prop + actions 槽，皆空则不渲染）、filters 槽可空 panel、
// 内容 .panel 内 StateBlock 四态优先级（loading > error > empty > 内容）、pagination 槽、retry 透传。

function mountPage(options: { props?: Record<string, unknown>; slots?: Record<string, string> } = {}) {
  return mount(ListPage, {
    props: options.props,
    slots: options.slots,
    global: { plugins: [createTestI18n()] }
  })
}

describe('ListPage 骨架结构', () => {
  it('title prop 渲染 toolbar h2；无 title 且无 actions 槽时不渲染 toolbar', () => {
    const withTitle = mountPage({ props: { title: '日报历史' } })
    expect(withTitle.find('.toolbar h2').text()).toBe('日报历史')
    const noTitle = mountPage()
    expect(noTitle.find('.toolbar').exists()).toBe(false)
  })

  it('actions 槽渲染在 toolbar 右侧容器内', () => {
    const wrapper = mountPage({
      props: { title: 't' },
      slots: { actions: '<button class="act-btn">新建</button>' }
    })
    expect(wrapper.find('.toolbar .list-page-actions .act-btn').exists()).toBe(true)
  })

  it('filters 槽渲染在独立 panel；缺省不渲染该 panel', () => {
    const withFilters = mountPage({ slots: { filters: '<input class="f" />' } })
    expect(withFilters.find('.panel.list-page-filters .f').exists()).toBe(true)
    const without = mountPage()
    expect(without.find('.list-page-filters').exists()).toBe(false)
  })

  it('pagination 槽渲染在内容 panel 内；缺省不渲染容器', () => {
    const withPager = mountPage({ slots: { default: '<div class="tbl" />', pagination: '<div class="pg" />' } })
    expect(withPager.find('.list-page-pagination .pg').exists()).toBe(true)
    const without = mountPage({ slots: { default: '<div class="tbl" />' } })
    expect(without.find('.list-page-pagination').exists()).toBe(false)
  })
})

describe('ListPage 四态（继承 StateBlock 优先级）', () => {
  it('loading：骨架优先于 error/empty/内容/分页', () => {
    const wrapper = mountPage({
      props: { loading: true, error: { message: 'x' }, empty: true },
      slots: { default: '<div class="tbl" />', pagination: '<div class="pg" />' }
    })
    expect(wrapper.findComponent(ElSkeleton).exists()).toBe(true)
    expect(wrapper.find('.tbl').exists()).toBe(false)
    expect(wrapper.find('.pg').exists()).toBe(false)
  })

  it('error：展示 errorText 与重试按钮，点击 emit retry；不渲染内容与分页', async () => {
    const wrapper = mountPage({
      props: { error: { message: '原始错误' }, errorText: '加载失败' },
      slots: { default: '<div class="tbl" />', pagination: '<div class="pg" />' }
    })
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('加载失败')
    expect(wrapper.find('.tbl').exists()).toBe(false)
    expect(wrapper.find('.pg').exists()).toBe(false)
    await wrapper.find('.state-block-retry').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
  })

  it('empty：展示 emptyText，不渲染内容', () => {
    const wrapper = mountPage({
      props: { empty: true, emptyText: '暂无数据' },
      slots: { default: '<div class="tbl" />' }
    })
    expect(wrapper.findComponent(ElEmpty).exists()).toBe(true)
    expect(wrapper.text()).toContain('暂无数据')
    expect(wrapper.find('.tbl').exists()).toBe(false)
  })

  it('内容态：default 与 pagination 槽均渲染', () => {
    const wrapper = mountPage({
      slots: { default: '<div class="tbl">表格</div>', pagination: '<div class="pg" />' }
    })
    expect(wrapper.find('.tbl').text()).toBe('表格')
    expect(wrapper.find('.list-page-pagination .pg').exists()).toBe(true)
  })
})
