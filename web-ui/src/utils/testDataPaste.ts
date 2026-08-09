// 测试数据批量录入的纯函数解析器与客户端校验（无 DOM/组件依赖，可单测）。
// 职责：Excel/CSV 粘贴文本 → 结构化行；行数据 → 本地错误 map。

export type BatchRow = {
  data_type: string
  measurement: string
  value?: number
  unit: string
  quality: string
  measured_at?: Date
  run_id?: string
  notes: string
  // rawValue 仅粘贴行保留：值无法解析为数字时用于 not_a_number 本地报错与展示
  rawValue?: string
  // invalidDateRaw 仅粘贴行保留：日期无法解析或越界时保留原文，用于 not_a_date 本地报错
  invalidDateRaw?: string
}

export type RowApiError = {
  index: number
  field: string
  code: string
  message: string
}

export type PasteResult = {
  rows: BatchRow[]
  truncated: boolean
  headerDetected: boolean
}

// 列别名表：表头精确匹配（trim + 剥离括号后缀后），中英文均可
const HEADER_ALIASES: Record<string, string[]> = {
  data_type: ['data_type', 'datatype', '数据类型', '类型'],
  measurement: ['measurement', 'measure', 'measurement_value', '测量值', '测量项', '测量'],
  value: ['value', '数值', '值'],
  unit: ['unit', '单位'],
  quality: ['quality', '质量'],
  measured_at: ['measured_at', 'measurement_time', '测量时间', '时间'],
  run_id: ['run_id', 'batch', '批次', '关联批次'],
  notes: ['notes', '备注', '注释']
}

// 无表头时的位置映射：前 8 列依次为
const POSITIONAL_FIELDS = ['data_type', 'measurement', 'value', 'unit', 'quality', 'measured_at', 'run_id', 'notes'] as const

// 枚举别名精确映射；未命中 → 原样保留（提交时后端 invalid_enum 报错，明确可见）
const DATA_TYPE_ALIASES: Record<string, string> = {
  cryo: 'cryo', 低温: 'cryo',
  pressure: 'pressure', 压强: 'pressure', 压力: 'pressure',
  voltage: 'voltage', 电压: 'voltage',
  rf_voltage: 'rf_voltage', 射频电压: 'rf_voltage',
  efficiency: 'efficiency', 效率: 'efficiency'
}

const QUALITY_ALIASES: Record<string, string> = {
  normal: 'normal', 正常: 'normal',
  outlier: 'outlier', 异常: 'outlier', 离群: 'outlier',
  suspect: 'suspect', 可疑: 'suspect',
  invalid: 'invalid', 无效: 'invalid'
}

// 客户端校验错误码（与后端行级 code 对齐）
export type ClientErrorCode =
  | 'required'
  | 'not_a_number'
  | 'not_a_date'
  | 'invalid_enum'
  | 'too_long'
  | 'invalid_uuid'

export type RowErrorMap = Map<ClientErrorCode, string>

export const MAX_BATCH_ROWS = 100

const MAX_MEASUREMENT_LEN = 128
const MAX_UNIT_LEN = 16

const DATA_TYPES = ['cryo', 'pressure', 'voltage', 'rf_voltage', 'efficiency']
const QUALITIES = ['normal', 'outlier', 'suspect', 'invalid']

/**
 * 解析粘贴文本（制表符/换行分隔，兼容 Excel 直接复制与 CSV）：
 * 表头识别（中英别名，命中 ≥2 格判定有表头）、数值三步转换、日期多格式解析。
 * 解析结果超过 100 行时只取前 100 行并置 truncated。
 */
export function parsePastedTestData(text: string): PasteResult {
  const lines = text
    .replace(/\r\n/g, '\n')
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')

  if (lines.length === 0) {
    return { rows: [], truncated: false, headerDetected: false }
  }

  const first = splitCells(lines[0])
  const header = detectHeader(first)
  // 无表头数据行恰含 ≥2 个与列别名同名的单元格时会被误判为表头；此时若首行出现纯数字单元格，说明它实际是数据行，降级为无表头
  if (header.detected && first.some((cell) => /^[-+]?\d+\.?\d*$/.test(cell.trim()))) {
    header.detected = false
  }
  const start = header.detected ? 1 : 0

  const rawRows = lines.slice(start).map(splitCells)
  const truncated = rawRows.length > MAX_BATCH_ROWS
  const rows = rawRows.slice(0, MAX_BATCH_ROWS).map((cells) => cellToRow(cells, header))

  return { rows, truncated, headerDetected: header.detected }
}

// 切行切列：Excel 直接复制为 \t 分隔；整行无 \t 且含 , → CSV 按逗号切
function splitCells(line: string): string[] {
  const cells = line.includes('\t') ? line.split('\t') : line.includes(',') ? line.split(',') : [line]
  // 行尾空单元格丢弃
  while (cells.length > 0 && cells[cells.length - 1].trim() === '') {
    cells.pop()
  }
  return cells
}

type HeaderMap = { detected: boolean; byName: Record<string, number> }

// 表头识别：逐格与别名表不区分大小写精确比对；失败时剥离括号后缀重试（value (K) / 测量值（单位））
function detectHeader(cells: string[]): HeaderMap {
  const byName: Record<string, number> = {}
  let hits = 0
  cells.forEach((cell, index) => {
    for (const [field, aliases] of Object.entries(HEADER_ALIASES)) {
      if (matchAlias(cell, aliases)) {
        byName[field] = index
        hits++
        break
      }
    }
  })
  return { detected: hits >= 2, byName }
}

function matchAlias(cell: string, aliases: string[]): boolean {
  const normalized = cell.trim().toLowerCase()
  if (aliases.some((a) => a.toLowerCase() === normalized)) return true
  const stripped = normalized.replace(/\s*[（(].*?[)）]\s*$/, '').trim()
  return stripped !== normalized && aliases.some((a) => a.toLowerCase() === stripped)
}

function cellToRow(cells: string[], header: HeaderMap): BatchRow {
  const row: BatchRow = {
    data_type: '',
    measurement: '',
    unit: '',
    quality: 'normal',
    notes: '',
    run_id: undefined,
    measured_at: undefined
  }
  const get = (field: string, positionalIndex: number): string => {
    const index = header.detected ? header.byName[field] : positionalIndex
    if (index === undefined) return ''
    return (cells[index] ?? '').trim()
  }

  const dataTypeRaw = get('data_type', 0)
  row.data_type = DATA_TYPE_ALIASES[dataTypeRaw] ?? dataTypeRaw
  row.measurement = get('measurement', 1)

  const valueRaw = get('value', 2)
  row.value = parseNumber(valueRaw)
  if (row.value === undefined && valueRaw !== '') row.rawValue = valueRaw

  row.unit = get('unit', 3)

  const qualityRaw = get('quality', 4)
  row.quality = QUALITY_ALIASES[qualityRaw] ?? (qualityRaw || 'normal')

  const dateRaw = get('measured_at', 5)
  if (dateRaw) {
    row.measured_at = parseDate(dateRaw)
    // 解析失败（格式非法或月/日越界）时保留原文，validateRows 报 not_a_date，避免静默丢弃
    if (!row.measured_at) row.invalidDateRaw = dateRaw
  }

  const runID = get('run_id', 6)
  if (runID) row.run_id = runID

  row.notes = get('notes', 7)
  return row
}

// 数值三步转换：去千分位 → Number() → 正则提取首个数字 token（兼容 79.6 K / ~4.2）
// 逗号仅接受「3 位一组千分位」模式（如 1,234.5）；其余含逗号写法（如欧式小数 4,5）视为非法，返回 undefined 走 not_a_number
export function parseNumber(raw: string): number | undefined {
  const trimmed = raw.trim()
  if (trimmed === '') return undefined
  let cleaned = trimmed
  if (trimmed.includes(',') || trimmed.includes('，')) {
    const normalized = trimmed.replace(/，/g, ',')
    if (!/^[-+]?\d{1,3}(,\d{3})+(\.\d+)?$/.test(normalized)) return undefined
    cleaned = normalized.replace(/,/g, '')
  }
  const direct = Number(cleaned)
  if (Number.isFinite(direct)) return direct
  const token = cleaned.match(/[-+]?\d+(\.\d+)?/)
  if (token) {
    const parsed = Number(token[0])
    if (Number.isFinite(parsed)) return parsed
  }
  return undefined
}

// 日期多格式：RFC3339/ISO → YYYY-MM-DD HH:mm(:ss) → YYYY/M/D H:mm → YYYY-MM-DD
// dateOnly 分支先于 new Date 匹配：纯日期统一按本地时间解析，避免 ISO 纯日期被 new Date 按 UTC 零点解析造成 8 小时时区差
export function parseDate(raw: string): Date | undefined {
  const value = raw.trim()
  if (value === '') return undefined

  const dateOnly = value.match(/^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})$/)
  if (dateOnly) {
    return buildDate(dateOnly)
  }
  const withSeconds = value.match(/^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})[ T](\d{1,2}):(\d{1,2}):(\d{1,2})$/)
  if (withSeconds) {
    return buildDate(withSeconds)
  }
  const withMinutes = value.match(/^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})[ T](\d{1,2}):(\d{1,2})$/)
  if (withMinutes) {
    return buildDate(withMinutes)
  }
  const iso = new Date(value)
  if (!Number.isNaN(iso.getTime())) return iso
  return undefined
}

// 由正则分支构造本地时间 Date；带时区（Z / ±hh:mm）或带毫秒的 ISO 字符串走 parseDate 中的 new Date 兜底
function buildDate(match: RegExpMatchArray): Date | undefined {
  const month = Number(match[2])
  const day = Number(match[3])
  // 显式校验月/日范围：new Date(2026, 12, 1) / new Date(2026, 0, 32) 会静默进位到次月，这里直接判非法
  if (month < 1 || month > 12 || day < 1 || day > 31) return undefined
  return new Date(
    Number(match[1]),
    month - 1,
    day,
    match[4] ? Number(match[4]) : 0,
    match[5] ? Number(match[5]) : 0,
    match[6] ? Number(match[6]) : 0
  )
}

// 客户端校验（规则与后端 1.4 逐条对应）：rowIndex → field → 错误码
export function validateRows(rows: BatchRow[]): Map<number, Map<string, Set<ClientErrorCode>>> {
  const errors = new Map<number, Map<string, Set<ClientErrorCode>>>()
  rows.forEach((row, index) => {
    const fieldErrors = new Map<string, Set<ClientErrorCode>>()
    const add = (field: string, code: ClientErrorCode) => {
      let codes = fieldErrors.get(field)
      if (!codes) {
        codes = new Set()
        fieldErrors.set(field, codes)
      }
      codes.add(code)
    }

    if (!row.data_type.trim()) {
      add('data_type', 'required')
    } else if (!DATA_TYPES.includes(row.data_type)) {
      add('data_type', 'invalid_enum')
    }

    if (!row.measurement.trim()) {
      add('measurement', 'required')
    } else if (row.measurement.length > MAX_MEASUREMENT_LEN) {
      add('measurement', 'too_long')
    }

    // el-input-number 清空时 emit null，== null 同时覆盖 undefined 与 null，避免绕过必填校验
    if (row.value == null) {
      add('value', 'required')
    } else if (!Number.isFinite(row.value)) {
      add('value', 'not_a_number')
    }
    if (row.rawValue && row.value == null) {
      add('value', 'not_a_number')
    }

    if (row.unit.length > MAX_UNIT_LEN) {
      add('unit', 'too_long')
    }

    if (!QUALITIES.includes(row.quality)) {
      add('quality', 'invalid_enum')
    }

    // invalidDateRaw：粘贴的日期原文解析失败（格式非法或月/日越界）→ not_a_date，不静默丢弃
    if (row.invalidDateRaw) {
      add('measured_at', 'not_a_date')
    } else if (row.measured_at && Number.isNaN(row.measured_at.getTime())) {
      add('measured_at', 'not_a_date')
    }

    if (row.run_id && !/^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(row.run_id.trim())) {
      add('run_id', 'invalid_uuid')
    }

    if (fieldErrors.size > 0) {
      errors.set(index, fieldErrors)
    }
  })
  return errors
}
