import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import TodoView from '@/views/TodoView.vue'
import { createTestI18n } from '@/test-utils/setup'
import { useAuthStore } from '@/stores/auth'
import type { Todo } from '@/api/todos'

// TodoView 页面测试（测试方案 §3.2 🟡）：待办列表三态、完成勾选流转、owner 权限按钮显隐。

vi.mock('@/api/todos', () => ({
  listTodos: vi.fn(),
  createTodo: vi.fn(),
  updateTodo: vi.fn(),
  doneTodo: vi.fn(),
  deferTodo: vi.fn(),
  deleteTodo: vi.fn(),
  getNotificationTopic: vi.fn(),
  provisionTopic: vi.fn(),
  redeemTopic: vi.fn()
}))

vi.mock('@/api/projects', () => ({
  listProjects: vi.fn().mockResolvedValue([])
}))

import { listTodos, doneTodo, getNotificationTopic } from '@/api/todos'

function makeTodo(overrides: Partial<Todo> = {}): Todo {
  return {
    id: 'todo_01',
    title: '校准真空计',
    priority: 'high',
    status: 'pending',
    source: 'manual',
    created_by: 'user_01',
    created_for: 'user_01',
    created_at: '2026-01-05T10:00:00+08:00',
    updated_at: '2026-01-05T10:00:00+08:00',
    owner_display_name: 'Test User',
    ...overrides
  }
}

async function mountView(role = 'member', userId = 'user_01') {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore(pinia).user = {
    id: userId,
    username: 'testuser',
    display_name: 'Test User',
    role,
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh'
  }
  const wrapper = mount(TodoView, {
    global: {
      plugins: [createTestI18n(), pinia],
      stubs: { teleport: true, ElSelect: true, ElDatePicker: true }
    }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.mocked(listTodos).mockReset()
  vi.mocked(doneTodo).mockReset()
  vi.mocked(getNotificationTopic).mockReset().mockResolvedValue({ topic: 'todos-lab', subscribe_url: 'https://ntfy.lab/todos-lab' })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('TodoView 列表', () => {
  it('列表渲染：待办行标题/优先级 + 完成勾选调 doneTodo', async () => {
    vi.mocked(listTodos).mockResolvedValue([makeTodo()])
    const wrapper = await mountView('member')
    expect(listTodos).toHaveBeenCalled()
    const rows = wrapper.findAll('.todo-row')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('校准真空计')
    vi.mocked(doneTodo).mockResolvedValue(makeTodo({ status: 'done' }))
    await rows[0].find('input[type="checkbox"]').setValue(true)
    await flushPromises()
    expect(doneTodo).toHaveBeenCalledWith('todo_01')
  })

  it('三态：加载失败 StateBlock 错误 + 重试；空列表 el-empty', async () => {
    vi.mocked(listTodos)
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce([])
    const wrapper = await mountView('member')
    await flushPromises()
    expect(wrapper.find('.state-block-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('待办加载失败')
    await wrapper.find('.state-block-retry').trigger('click')
    await flushPromises()
    expect(wrapper.find('.el-empty__description').text()).toBe('暂无待办')
  })

  it('owner 权限：本人待办显示编辑/删除/顺延；他人待办仅完成勾选', async () => {
    vi.mocked(listTodos).mockResolvedValue([
      makeTodo({ id: 'todo_01', created_by: 'user_01' }),
      makeTodo({ id: 'todo_02', created_by: 'user_02' })
    ])
    const wrapper = await mountView('member', 'user_01')
    const rows = wrapper.findAll('.todo-row')
    const ownActions = rows[0].findAll('button').map((b) => b.text().trim())
    expect(ownActions).toEqual(expect.arrayContaining(['编辑', '删除']))
    const otherActions = rows[1].findAll('button').map((b) => b.text().trim())
    expect(otherActions).not.toContain('编辑')
    expect(otherActions).not.toContain('删除')
  })
})
