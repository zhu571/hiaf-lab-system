import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useProjectStore } from '../project'
import type { Project } from '../../api/projects'

// mock api 层：listProjects 打桩，不发起真实网络请求
const mocks = vi.hoisted(() => ({
  listProjects: vi.fn()
}))

vi.mock('../../api/projects', () => ({
  listProjects: mocks.listProjects
}))

function makeProject(id: string, name = `项目${id}`): Project {
  return {
    id,
    code: `P-${id}`,
    name,
    short_name: name,
    description: '',
    status: 'active',
    visibility: 'internal'
  }
}

describe('project store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('load：填充项目列表并默认选中第一个', async () => {
    const projects = [makeProject('p1'), makeProject('p2')]
    mocks.listProjects.mockResolvedValue(projects)

    const store = useProjectStore()
    await store.load()

    expect(mocks.listProjects).toHaveBeenCalled()
    expect(store.projects).toEqual(projects)
    expect(store.currentId).toBe('p1')
  })

  it('load：currentId 已选中时不被首个项目覆盖', async () => {
    const projects = [makeProject('p1'), makeProject('p2')]
    mocks.listProjects.mockResolvedValue(projects)

    const store = useProjectStore()
    store.currentId = 'p2'
    await store.load()

    expect(store.currentId).toBe('p2')
  })

  it('load：空列表时 projects 为空且 currentId 不变', async () => {
    mocks.listProjects.mockResolvedValue([])

    const store = useProjectStore()
    await store.load()

    expect(store.projects).toEqual([])
    expect(store.currentId).toBe('')
  })

  it('load：api 失败时异常向上抛，状态保持初始', async () => {
    mocks.listProjects.mockRejectedValue(new Error('network'))

    const store = useProjectStore()
    await expect(store.load()).rejects.toThrow('network')
    expect(store.projects).toEqual([])
    expect(store.currentId).toBe('')
  })

  it('select：切换当前选中项目', () => {
    const store = useProjectStore()
    store.projects = [makeProject('p1'), makeProject('p2')]
    store.select('p2')
    expect(store.currentId).toBe('p2')
  })

  it('current getter：优先按 currentId 匹配，未匹配时回退到第一个', () => {
    const store = useProjectStore()
    const projects = [makeProject('p1'), makeProject('p2')]
    store.projects = projects

    store.currentId = 'p2'
    expect(store.current?.id).toBe('p2')

    store.currentId = 'nonexistent'
    expect(store.current?.id).toBe('p1')

    store.projects = []
    expect(store.current).toBeUndefined()
  })
})
