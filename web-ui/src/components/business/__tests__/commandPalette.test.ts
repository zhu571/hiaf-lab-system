import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import CommandPalette from '@/components/business/CommandPalette.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useCommandPalette } from '@/composables/useCommandPalette'
import { useAskDialog } from '@/composables/useAskDialog'
import { useAuthStore } from '@/stores/auth'
import { useProjectStore } from '@/stores/project'
import type { UserInfo } from '@/api/auth'
import type { Project } from '@/api/projects'

// CommandPalette 组件测试（结构改版 R2 §3.1）：
// Ctrl/⌘+K 唤起关闭、三组渲染与角色过滤、过滤（label + 路径段）、键盘导航与执行、
// 新建批次空项目隐藏、普通按键不拦截。面板开关经 useCommandPalette 模块级单例。

const pushMock = vi.hoisted(() => vi.fn())

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock })
}))

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

function makeProject(id: string, name: string): Project {
  return { id, code: id.toUpperCase(), name, short_name: '', description: '', status: 'active', visibility: 'private' }
}

async function mountPalette(role: string, projectList: Project[] = []) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().user = makeUser(role)
  useProjectStore().projects = projectList
  const wrapper = mount(CommandPalette, {
    global: { plugins: [createTestI18n(), pinia], stubs: { teleport: true } }
  })
  await flushPromises()
  return wrapper
}

async function openAndFlush() {
  useCommandPalette().openPalette()
  await flushPromises()
}

function paletteInput(wrapper: Awaited<ReturnType<typeof mountPalette>>) {
  return wrapper.find('.palette-input')
}

function itemTexts(wrapper: Awaited<ReturnType<typeof mountPalette>>) {
  return wrapper.findAll('.palette-item').map((i) => i.text())
}

beforeEach(() => {
  pushMock.mockReset()
  useCommandPalette().closePalette()
  useAskDialog().askOpen.value = false
})

afterEach(() => {
  useCommandPalette().closePalette()
  vi.restoreAllMocks()
})

describe('CommandPalette 唤起与关闭', () => {
  it('Ctrl+K 唤起/关闭：preventDefault 拦截；普通按键不拦截、不污染输入', async () => {
    const wrapper = await mountPalette('admin')
    expect(paletteInput(wrapper).exists()).toBe(false)

    const open = new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, cancelable: true })
    window.dispatchEvent(open)
    await flushPromises()
    expect(open.defaultPrevented).toBe(true)
    expect(useCommandPalette().paletteOpen.value).toBe(true)
    expect(paletteInput(wrapper).exists()).toBe(true)

    // 再按 Ctrl+K 关闭
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true }))
    await flushPromises()
    expect(useCommandPalette().paletteOpen.value).toBe(false)

    // 普通按键（无修饰）不拦截、不唤起
    const plain = new KeyboardEvent('keydown', { key: 'k', cancelable: true })
    window.dispatchEvent(plain)
    expect(plain.defaultPrevented).toBe(false)
    expect(useCommandPalette().paletteOpen.value).toBe(false)
    wrapper.unmount()
  })

  it('⌘K（metaKey）同样唤起', async () => {
    const wrapper = await mountPalette('admin')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))
    await flushPromises()
    expect(useCommandPalette().paletteOpen.value).toBe(true)
    wrapper.unmount()
  })
})

describe('CommandPalette 三组数据源', () => {
  it('admin：页面/项目/动作三组渲染；动作组含全部 5 项', async () => {
    const wrapper = await mountPalette('admin', [makeProject('p1', 'HIAF气靶'), makeProject('p2', '束流调试')])
    await openAndFlush()
    await flushPromises()

    const groups = wrapper.findAll('.palette-group').map((g) => g.text())
    expect(groups).toEqual(['页面', '项目', '动作'])
    const texts = itemTexts(wrapper)
    expect(texts).toContain('首页')
    expect(texts).toContain('HIAF气靶')
    expect(texts).toContain('束流调试')
    expect(texts).toContain('写日报')
    expect(texts).toContain('新建待办')
    expect(texts).toContain('AI 问答')
    expect(texts).toContain('新建项目')
    expect(texts).toContain('新建批次')
    wrapper.unmount()
  })

  it('viewer：maintainer/admin 专属页面与动作被过滤（AI审核/写日报/新建项目/新建批次不可见）', async () => {
    const wrapper = await mountPalette('viewer', [makeProject('p1', 'HIAF气靶')])
    await openAndFlush()
    await flushPromises()

    const texts = itemTexts(wrapper)
    expect(texts).toContain('首页')
    expect(texts).not.toContain('AI审核')
    expect(texts).not.toContain('用户管理')
    expect(texts).toContain('新建待办')
    expect(texts).toContain('AI 问答')
    expect(texts).not.toContain('写日报')
    expect(texts).not.toContain('新建项目')
    expect(texts).not.toContain('新建批次')
    wrapper.unmount()
  })

  it('项目列表为空：项目组整组隐藏，新建批次动作隐藏（其余动作保留）', async () => {
    const wrapper = await mountPalette('admin', [])
    await openAndFlush()
    await flushPromises()

    const groups = wrapper.findAll('.palette-group').map((g) => g.text())
    expect(groups).toEqual(['页面', '动作'])
    const texts = itemTexts(wrapper)
    expect(texts).not.toContain('新建批次')
    expect(texts).toContain('新建项目')
    wrapper.unmount()
  })
})

describe('CommandPalette 过滤', () => {
  it('按路径段过滤：sensors 命中传感器页（label 中文亦可命中）', async () => {
    const wrapper = await mountPalette('admin', [])
    await openAndFlush()
    await paletteInput(wrapper).setValue('sensors')
    await flushPromises()
    expect(itemTexts(wrapper)).toEqual(['传感器'])

    await paletteInput(wrapper).setValue('传感器')
    await flushPromises()
    expect(itemTexts(wrapper)).toEqual(['传感器'])
    wrapper.unmount()
  })

  it('按项目名过滤：气靶 命中项目组条目', async () => {
    const wrapper = await mountPalette('admin', [makeProject('p1', 'HIAF气靶'), makeProject('p2', '束流调试')])
    await openAndFlush()
    await paletteInput(wrapper).setValue('气靶')
    await flushPromises()
    expect(itemTexts(wrapper)).toEqual(['HIAF气靶'])
    wrapper.unmount()
  })

  it('无匹配显示空态', async () => {
    const wrapper = await mountPalette('viewer', [])
    await openAndFlush()
    await paletteInput(wrapper).setValue('zzz-no-match')
    await flushPromises()
    expect(wrapper.find('.palette-empty').text()).toBe('无匹配结果')
    wrapper.unmount()
  })
})

describe('CommandPalette 键盘导航与执行', () => {
  it('Enter 执行当前高亮项（默认第一项=首页）并关闭面板', async () => {
    const wrapper = await mountPalette('admin', [])
    await openAndFlush()
    await paletteInput(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(pushMock).toHaveBeenCalledWith('/')
    expect(useCommandPalette().paletteOpen.value).toBe(false)
    wrapper.unmount()
  })

  it('↓ 移动高亮后 Enter 执行次项（/projects）；↑ 环回', async () => {
    const wrapper = await mountPalette('admin', [])
    await openAndFlush()
    await paletteInput(wrapper).trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const items = wrapper.findAll('.palette-item')
    expect(items[1].classes()).toContain('is-active')
    expect(items[0].classes()).not.toContain('is-active')
    await paletteInput(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(pushMock).toHaveBeenCalledWith('/projects')

    // ↑ 环回：重开后高亮归 0，按 ↑ 到末项，再 ↓ 回首项
    await openAndFlush()
    await paletteInput(wrapper).trigger('keydown', { key: 'ArrowUp' })
    await flushPromises()
    const all = wrapper.findAll('.palette-item')
    expect(all[all.length - 1].classes()).toContain('is-active')
    await paletteInput(wrapper).trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    expect(wrapper.findAll('.palette-item')[0].classes()).toContain('is-active')
    wrapper.unmount()
  })

  it('过滤后 Enter 执行过滤结果首项（sensors → /sensors）', async () => {
    const wrapper = await mountPalette('admin', [])
    await openAndFlush()
    await paletteInput(wrapper).setValue('sensors')
    await flushPromises()
    await paletteInput(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(pushMock).toHaveBeenCalledWith('/sensors')
    wrapper.unmount()
  })

  it('点击条目执行跳转并关闭；悬停更新高亮', async () => {
    const wrapper = await mountPalette('admin', [makeProject('p1', 'HIAF气靶')])
    await openAndFlush()
    await flushPromises()

    const projectItem = wrapper.findAll('.palette-item').find((i) => i.text() === 'HIAF气靶')!
    await projectItem.trigger('mouseenter')
    expect(projectItem.classes()).toContain('is-active')
    await projectItem.trigger('click')
    await flushPromises()
    expect(pushMock).toHaveBeenCalledWith('/projects/p1')
    expect(useCommandPalette().paletteOpen.value).toBe(false)
    wrapper.unmount()
  })

  it('AI 问答动作：打开既有 AskDialog（不走路由）并关闭面板', async () => {
    const wrapper = await mountPalette('viewer', [])
    await openAndFlush()
    await flushPromises()
    const askItem = wrapper.findAll('.palette-item').find((i) => i.text() === 'AI 问答')!
    await askItem.trigger('click')
    await flushPromises()
    expect(useAskDialog().askOpen.value).toBe(true)
    expect(pushMock).not.toHaveBeenCalled()
    expect(useCommandPalette().paletteOpen.value).toBe(false)
    wrapper.unmount()
  })
})
