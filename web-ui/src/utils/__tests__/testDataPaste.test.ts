import { describe, it, expect } from 'vitest'
import {
  parsePastedTestData,
  parseNumber,
  parseDate,
  validateRows,
  MAX_BATCH_ROWS,
  type BatchRow
} from '../testDataPaste'

describe('parsePastedTestData 粘贴解析', () => {
  it('空文本：返回空结果', () => {
    expect(parsePastedTestData('')).toEqual({ rows: [], truncated: false, headerDetected: false })
    expect(parsePastedTestData('  \n\t\n  ')).toEqual({ rows: [], truncated: false, headerDetected: false })
  })

  it('Excel Tab 粘贴 + 中文表头：表头识别并按列映射', () => {
    const text = '数据类型\t测量值\t数值\t单位\t质量\t测量时间\ncryo\t温度\t12.5\tK\tnormal\t2026-08-01 10:00'
    const result = parsePastedTestData(text)

    expect(result.headerDetected).toBe(true)
    expect(result.truncated).toBe(false)
    expect(result.rows).toHaveLength(1)
    expect(result.rows[0]).toMatchObject({
      data_type: 'cryo',
      measurement: '温度',
      value: 12.5,
      unit: 'K',
      quality: 'normal',
      notes: ''
    })
    expect(result.rows[0].measured_at?.getHours()).toBe(10)
  })

  it('英文表头（含括号单位后缀）：剥离后缀识别', () => {
    const text = 'data_type\tmeasurement\tvalue (K)\tquality\nvoltage\tV1\t3.3\tnormal'
    const result = parsePastedTestData(text)

    expect(result.headerDetected).toBe(true)
    expect(result.rows[0]).toMatchObject({ data_type: 'voltage', value: 3.3, unit: '' })
  })

  it('无表头时按位置映射前 8 列', () => {
    const text = 'pressure\t入口压强\t101325\tPa\tnormal\t2026/8/1 9:30\trun-123\t备注内容'
    const result = parsePastedTestData(text)

    expect(result.headerDetected).toBe(false)
    const row = result.rows[0]
    expect(row.data_type).toBe('pressure')
    expect(row.measurement).toBe('入口压强')
    expect(row.value).toBe(101325)
    expect(row.unit).toBe('Pa')
    expect(row.run_id).toBe('run-123')
    expect(row.notes).toBe('备注内容')
  })

  it('首行数字行不会误判为表头（纯数字单元格降级为无表头）', () => {
    const text = 'cryo\t温度\t12.5\tK\tnormal\npressure\t压力\t99\tPa\tnormal'
    const result = parsePastedTestData(text)

    expect(result.headerDetected).toBe(false)
    expect(result.rows).toHaveLength(2)
    expect(result.rows[0]).toMatchObject({ data_type: 'cryo', value: 12.5 })
    expect(result.rows[1]).toMatchObject({ data_type: 'pressure', value: 99 })
  })

  it('CSV 逗号分隔：整行无 Tab 时按逗号切列', () => {
    const text = 'data_type,measurement,value\nrf_voltage,Vpp,80'
    const result = parsePastedTestData(text)

    expect(result.headerDetected).toBe(true)
    expect(result.rows[0]).toMatchObject({ data_type: 'rf_voltage', value: 80 })
  })

  it('CRLF 换行与行尾空单元格：归一化并丢弃', () => {
    const text = 'data_type\tmeasurement\tvalue\r\ncryo\t温度\t1.5\t\t\t\r\n'
    const result = parsePastedTestData(text)

    expect(result.rows).toHaveLength(1)
    expect(result.rows[0]).toMatchObject({ data_type: 'cryo', unit: '', quality: 'normal' })
  })

  it('超过 MAX_BATCH_ROWS 行：截断并置 truncated', () => {
    const rows = Array.from({ length: MAX_BATCH_ROWS + 5 }, (_, i) => `cryo\tm${i}\t${i}`)
    const result = parsePastedTestData(rows.join('\n'))

    expect(result.truncated).toBe(true)
    expect(result.rows).toHaveLength(MAX_BATCH_ROWS)
  })

  it('未命中别名枚举：原样保留（交给后端校验）', () => {
    const text = 'what_ever\t温度\t1\tK\n'
    const result = parsePastedTestData(text)
    expect(result.rows[0].data_type).toBe('what_ever')
  })

  it('非法日期：保留 invalidDateRaw 供本地校验报错', () => {
    const text = 'cryo\t温度\t1\tK\tnormal\t2026-13-99'
    const result = parsePastedTestData(text)
    expect(result.rows[0].measured_at).toBeUndefined()
    expect(result.rows[0].invalidDateRaw).toBe('2026-13-99')
  })
})

describe('parseNumber 数值解析', () => {
  it('空串返回 undefined', () => {
    expect(parseNumber('')).toBeUndefined()
    expect(parseNumber('   ')).toBeUndefined()
  })

  it('普通数字直接解析', () => {
    expect(parseNumber('12.5')).toBe(12.5)
    expect(parseNumber('-3')).toBe(-3)
    expect(parseNumber('+4.2')).toBe(4.2)
  })

  it('千分位逗号仅接受 3 位一组', () => {
    expect(parseNumber('1,234.5')).toBe(1234.5)
    expect(parseNumber('1234,5')).toBeUndefined()
    expect(parseNumber('1,23')).toBeUndefined()
  })

  it('首 token 提取：79.6 K / ~4.2', () => {
    expect(parseNumber('79.6 K')).toBe(79.6)
    expect(parseNumber('~4.2')).toBe(4.2)
  })

  it('无数字 token 返回 undefined', () => {
    expect(parseNumber('abc')).toBeUndefined()
  })
})

describe('parseDate 日期解析', () => {
  it('纯日期按本地时间解析（无 8 小时 UTC 偏差）', () => {
    const d = parseDate('2026-08-01')
    expect(d).not.toBeUndefined()
    expect(d!.getFullYear()).toBe(2026)
    expect(d!.getMonth()).toBe(7)
    expect(d!.getDate()).toBe(1)
    expect(d!.getHours()).toBe(0)
  })

  it('YYYY/M/D H:mm 与带秒格式', () => {
    const d1 = parseDate('2026/8/1 9:30')
    expect(d1!.getHours()).toBe(9)
    expect(d1!.getMinutes()).toBe(30)

    const d2 = parseDate('2026-08-01 09:30:45')
    expect(d2!.getHours()).toBe(9)
    expect(d2!.getSeconds()).toBe(45)
  })

  it('月/日越界返回 undefined（不静默进位）', () => {
    expect(parseDate('2026-13-01')).toBeUndefined()
    expect(parseDate('2026-00-01')).toBeUndefined()
    expect(parseDate('2026-01-32')).toBeUndefined()
  })

  it('ISO 带时区字符串走 new Date 兜底', () => {
    const d = parseDate('2026-08-01T02:00:00Z')
    expect(d).not.toBeUndefined()
    expect(Number.isNaN(d!.getTime())).toBe(false)
  })

  it('非法字符串返回 undefined', () => {
    expect(parseDate('not a date')).toBeUndefined()
    expect(parseDate('')).toBeUndefined()
  })
})

describe('validateRows 客户端校验', () => {
  function baseRow(overrides: Partial<BatchRow> = {}): BatchRow {
    return {
      data_type: 'cryo',
      measurement: '温度',
      value: 1,
      unit: 'K',
      quality: 'normal',
      notes: '',
      ...overrides
    }
  }

  it('合法行无错误', () => {
    const errors = validateRows([baseRow()])
    expect(errors.size).toBe(0)
  })

  it('缺 data_type/measurement/value：required', () => {
    const errors = validateRows([baseRow({ data_type: '', measurement: '', value: undefined })])
    const codes = errors.get(0)!
    expect(codes.get('data_type')!.has('required')).toBe(true)
    expect(codes.get('measurement')!.has('required')).toBe(true)
    expect(codes.get('value')!.has('required')).toBe(true)
  })

  it('未知枚举：invalid_enum', () => {
    const errors = validateRows([baseRow({ data_type: 'foo', quality: 'bar' })])
    const codes = errors.get(0)!
    expect(codes.get('data_type')!.has('invalid_enum')).toBe(true)
    expect(codes.get('quality')!.has('invalid_enum')).toBe(true)
  })

  it('rawValue 无法解析为数字：not_a_number', () => {
    const errors = validateRows([baseRow({ value: undefined, rawValue: 'abc' })])
    expect(errors.get(0)!.get('value')!.has('not_a_number')).toBe(true)
  })

  it('非法 run_id：invalid_uuid；合法 UUID 通过', () => {
    const bad = validateRows([baseRow({ run_id: 'not-a-uuid' })])
    expect(bad.get(0)!.get('run_id')!.has('invalid_uuid')).toBe(true)

    const good = validateRows([baseRow({ run_id: '3f2a1b9c-8e6d-4a5b-9c0d-1e2f3a4b5c6d' })])
    expect(good.size).toBe(0)
  })

  it('多行错误按行索引独立收集', () => {
    const errors = validateRows([baseRow(), baseRow({ data_type: '' })])
    expect(errors.has(0)).toBe(false)
    expect(errors.get(1)!.get('data_type')!.has('required')).toBe(true)
  })
})
