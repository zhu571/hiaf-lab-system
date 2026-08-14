import { describe, it, expect } from 'vitest'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { ElButton, ElDialog, ElForm } from 'element-plus'
import FormDialog from '@/components/base/FormDialog.vue'
import { createTestI18n } from '@/test-utils/setup'

// FormDialog 组件测试（重构方案 §3.7 契约）：
// props { modelValue, title, loading?, width? } + slots（default=表单、footer 默认取消/确定双按钮，emit submit）；
// 内置 el-form label-position="top"。
// el-dialog 默认 teleport 到 body + 打开过渡：测试用 teleport stub 并等待 nextTick 让内容渲染进 wrapper。
async function mountDialog(props: Record<string, unknown> = {}, slots: Record<string, string> = {}) {
  const wrapper = mount(FormDialog, {
    props: { modelValue: true, title: '测试弹窗', ...props },
    slots,
    global: { plugins: [createTestI18n()], stubs: { teleport: true } }
  })
  await nextTick()
  return wrapper
}

describe('FormDialog 基本渲染', () => {
  it('渲染 title + default 插槽表单内容 + 内置 label-position=top 表单', async () => {
    const wrapper = await mountDialog({}, { default: '<el-form-item label="名称"><input /></el-form-item>' })
    expect(wrapper.findComponent(ElDialog).exists()).toBe(true)
    expect(wrapper.text()).toContain('测试弹窗')
    expect(wrapper.find('input').exists()).toBe(true)
    expect(wrapper.findComponent(ElForm).props('labelPosition')).toBe('top')
  })

  it('modelValue=false 时不显示弹窗；width 透传', async () => {
    const closed = await mountDialog({ modelValue: false })
    expect(closed.findComponent(ElDialog).props('modelValue')).toBe(false)
    expect(closed.find('.el-dialog').exists()).toBe(false)
    const sized = await mountDialog({ width: 620 })
    expect(sized.findComponent(ElDialog).props('width')).toBe(620)
  })
})

describe('FormDialog 默认 footer', () => {
  it('取消按钮 emit update:modelValue(false)，确定按钮 emit submit；loading 时确定按钮带 loading', async () => {
    const wrapper = await mountDialog()
    const buttons = wrapper.findAllComponents(ElButton)
    const cancel = buttons.find((b) => b.text() === '取消')!
    const confirm = buttons.find((b) => b.text() === '确定')!
    await cancel.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
    expect(wrapper.emitted('submit')).toBeUndefined()
    await confirm.trigger('click')
    expect(wrapper.emitted('submit')).toHaveLength(1)
    expect(confirm.props('loading')).toBe(false)

    const loadingWrapper = await mountDialog({ loading: true })
    const loadingConfirm = loadingWrapper.findAllComponents(ElButton).find((b) => b.text() === '确定')!
    expect(loadingConfirm.props('loading')).toBe(true)
  })

  it('footer 插槽覆盖默认按钮', async () => {
    const wrapper = await mountDialog({}, { footer: '<el-button class="custom-foot">自定义</el-button>' })
    expect(wrapper.find('.custom-foot').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('确定')
  })
})
