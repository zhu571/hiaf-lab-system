// AI 问答「查看详情」跳转映射（方案 §4 跳转映射表，前端维护）。
// v1 严格单表：行 id 归属唯一表，映射成立；无映射的表（logs、instrument_results、
// 关联表等）只展示不跳转。项目类表行须含 project_id，否则不跳。

type RouteSpec =
  | { kind: 'detail'; build: (id: string) => string }
  | { kind: 'project'; build: (projectId: string) => string }
  | { kind: 'list'; path: string }

const TABLE_ROUTES: Record<string, RouteSpec> = {
  experiment_runs: { kind: 'detail', build: (id) => `/experiment-runs/${encodeURIComponent(id)}` },
  daily_reports: { kind: 'detail', build: (id) => `/daily-reports/${encodeURIComponent(id)}` },
  step_templates: { kind: 'list', path: '/step-templates' },
  todos: { kind: 'list', path: '/todos' },
  experiences: { kind: 'list', path: '/experiences' },
  attachments: { kind: 'list', path: '/attachments' },
  issues: { kind: 'project', build: (pid) => `/projects/${encodeURIComponent(pid)}/issues` },
  test_data: { kind: 'project', build: (pid) => `/projects/${encodeURIComponent(pid)}/test-data` },
  rf_matching_records: { kind: 'project', build: (pid) => `/projects/${encodeURIComponent(pid)}/rf-matching` },
  assembly_steps: { kind: 'project', build: (pid) => `/projects/${encodeURIComponent(pid)}/assembly` }
}

export function hasRowRoute(tableName: string): boolean {
  return tableName in TABLE_ROUTES
}

function keyOf(spec: RouteSpec): string {
  return spec.kind === 'detail' ? 'id' : 'project_id'
}

function cellValue(row: Record<string, unknown>, key: string): string | null {
  const v = row[key]
  if (v === null || v === undefined || v === '') return null
  return String(v)
}

/** 单行是否可跳转：list 类型恒可跳；detail 需行含 id；project 需行含 project_id */
export function canOpenRow(row: Record<string, unknown>, tableName: string): boolean {
  const spec = TABLE_ROUTES[tableName]
  if (!spec) return false
  if (spec.kind === 'list') return true
  return cellValue(row, keyOf(spec)) !== null
}

/** 计算行跳转路由；无映射或条件不满足返回 null */
export function tableToRoute(row: Record<string, unknown>, tableName: string): string | null {
  const spec = TABLE_ROUTES[tableName]
  if (!spec) return null
  if (spec.kind === 'list') return spec.path
  const value = cellValue(row, keyOf(spec))
  return value ? spec.build(value) : null
}
