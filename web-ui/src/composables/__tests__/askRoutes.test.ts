import { describe, it, expect } from 'vitest'
import { hasRowRoute, canOpenRow, tableToRoute } from '../askRoutes'

// AI 问答「查看详情」跳转映射（方案 §4 跳转映射表）：
// detail 需行含 id、project 需行含 project_id、list 恒可跳。
describe('hasRowRoute 映射表', () => {
  it('已知表命中；未映射表（logs/instrument_results）未命中', () => {
    expect(hasRowRoute('experiment_runs')).toBe(true)
    expect(hasRowRoute('daily_reports')).toBe(true)
    expect(hasRowRoute('issues')).toBe(true)
    expect(hasRowRoute('logs')).toBe(false)
    expect(hasRowRoute('instrument_results')).toBe(false)
  })
})

describe('canOpenRow 行级可跳转判断', () => {
  it('list 类型恒可跳（行可为空对象）；未映射表恒不可跳', () => {
    expect(canOpenRow({}, 'todos')).toBe(true)
    expect(canOpenRow({}, 'step_templates')).toBe(true)
    expect(canOpenRow({}, 'experiences')).toBe(true)
    expect(canOpenRow({}, 'attachments')).toBe(true)
    expect(canOpenRow({}, 'logs')).toBe(false)
  })

  it('detail 类型需行含非空 id', () => {
    expect(canOpenRow({ id: 'r1' }, 'experiment_runs')).toBe(true)
    expect(canOpenRow({}, 'experiment_runs')).toBe(false)
    expect(canOpenRow({ id: '' }, 'daily_reports')).toBe(false)
  })

  it('project 类型需行含非空 project_id', () => {
    expect(canOpenRow({ project_id: 'p1' }, 'issues')).toBe(true)
    expect(canOpenRow({}, 'test_data')).toBe(false)
    expect(canOpenRow({ project_id: null }, 'rf_matching_records')).toBe(false)
  })
})

describe('tableToRoute 路由拼装', () => {
  it('detail：build 拼接并 encodeURIComponent 转义 id', () => {
    expect(tableToRoute({ id: 'r-1/2' }, 'experiment_runs')).toBe('/experiment-runs/r-1%2F2')
    expect(tableToRoute({ id: 'dr1' }, 'daily_reports')).toBe('/daily-reports/dr1')
  })

  it('project：拼 /projects/:pid/ 资源路径', () => {
    expect(tableToRoute({ project_id: 'p1' }, 'issues')).toBe('/projects/p1/issues')
    expect(tableToRoute({ project_id: 'p1' }, 'test_data')).toBe('/projects/p1/test-data')
    expect(tableToRoute({ project_id: 'p1' }, 'assembly_steps')).toBe('/projects/p1/assembly')
  })

  it('list：直接返回固定 path', () => {
    expect(tableToRoute({}, 'todos')).toBe('/todos')
    expect(tableToRoute({}, 'experiences')).toBe('/experiences')
  })

  it('未映射表或缺字段返回 null', () => {
    expect(tableToRoute({ id: 'x' }, 'logs')).toBeNull()
    expect(tableToRoute({}, 'experiment_runs')).toBeNull()
    expect(tableToRoute({}, 'issues')).toBeNull()
  })
})
