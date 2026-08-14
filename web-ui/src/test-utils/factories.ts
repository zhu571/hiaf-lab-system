// 测试数据工厂（方案 §5.3）：签名统一 (overrides?: Partial<T>) => T，
// 类型直接 import type 自 api 模块——类型同源，后端字段变更时 vue-tsc 编译期拦截，
// 测试与 API 契约不脱节。工厂只构造内存对象，不触碰后端与 localStorage。
// 数据全为虚构，不含真实人员/设备/内网信息。
// 扩展机制：新测试需要新实体时在同文件追加 makeX，默认值合理性由 PR 评审把关。

import type { UserInfo } from '../api/auth'
import type { Project } from '../api/projects'
import type { ExperimentRun, RunStep } from '../api/runs'
import type { Issue } from '../api/issues'
import type { TestData } from '../api/testdata'
import type { SensorPoint } from '../api/sensors'
import type { AskChatResponse } from '../api/ask'

export function makeUser(overrides: Partial<UserInfo> = {}): UserInfo {
  return {
    id: 'user_01',
    username: 'testuser',
    display_name: 'Test User',
    role: 'viewer',
    must_change_password: false,
    created_at: '2026-01-01T00:00:00+08:00',
    disabled: false,
    language: 'zh',
    ...overrides
  }
}

export function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 'proj_01',
    code: 'TEST-P01',
    name: '测试项目 01',
    short_name: '项目01',
    description: '',
    status: 'active',
    visibility: 'internal',
    ...overrides
  }
}

export function makeRun(overrides: Partial<ExperimentRun> = {}): ExperimentRun {
  return {
    id: 'run_01',
    project_id: 'proj_01',
    name: '实验运行 01',
    run_type: 'experiment',
    status: 'planned',
    gas_type: 'Ar',
    pressure_unit: 'Pa',
    has_beam: false,
    created_at: '2026-01-02T10:00:00+08:00',
    updated_at: '2026-01-02T10:00:00+08:00',
    ...overrides
  }
}

export function makeRunStep(overrides: Partial<RunStep> = {}): RunStep {
  return {
    id: 'step_01',
    run_id: 'run_01',
    name: '步骤 01',
    status: 'pending',
    step_order: 1,
    created_at: '2026-01-02T10:05:00+08:00',
    updated_at: '2026-01-02T10:05:00+08:00',
    ...overrides
  }
}

export function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: 'issue_01',
    project_id: 'proj_01',
    title: '测试问题 01',
    description: '问题描述',
    status: 'open',
    severity: 'medium',
    author_id: 'user_01',
    ...overrides
  }
}

export function makeTestData(overrides: Partial<TestData> = {}): TestData {
  return {
    id: 'td_01',
    project_id: 'proj_01',
    data_type: 'pressure',
    measurement: '入口压强',
    value: 101325,
    unit: 'Pa',
    quality: 'normal',
    source: 'manual',
    created_at: '2026-01-03T09:00:00+08:00',
    updated_at: '2026-01-03T09:00:00+08:00',
    ...overrides
  }
}

export function makeSensorPoint(overrides: Partial<SensorPoint> = {}): SensorPoint {
  return {
    time: '2026-01-03T09:00:00+08:00',
    tag: 'gascell_t',
    value: 293.15,
    ...overrides
  }
}

export function makeAskChatResponse(overrides: Partial<AskChatResponse> = {}): AskChatResponse {
  return {
    id: 'ask_01',
    question: '最近的测试数据',
    answer: '查询到 3 条记录',
    sql: 'SELECT * FROM test_data LIMIT 3',
    table_name: 'test_data',
    columns: ['id', 'value'],
    rows: [{ id: 'td_01', value: 12.5 }],
    row_count: 1,
    truncated: false,
    duration_ms: 120,
    created_at: '2026-01-04T10:00:00+08:00',
    ...overrides
  }
}
