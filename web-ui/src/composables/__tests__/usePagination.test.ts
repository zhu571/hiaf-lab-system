import { describe, it, expect } from 'vitest'
import { usePagination } from '../usePagination'

describe('usePagination（S2，§3.5）', () => {
  it('初始状态：page=1、perPage 默认 10、total=0、loading=false', () => {
    const p = usePagination()
    expect(p.page.value).toBe(1)
    expect(p.perPage.value).toBe(10)
    expect(p.total.value).toBe(0)
    expect(p.loading.value).toBe(false)
  })

  it('自定义 perPage 选项生效', () => {
    const p = usePagination({ perPage: 50 })
    expect(p.perPage.value).toBe(50)
  })

  it('onCurrentChange 更新页码（el-pagination current-change）', () => {
    const p = usePagination()
    p.onCurrentChange(3)
    expect(p.page.value).toBe(3)
    expect(p.perPage.value).toBe(10)
  })

  it('onSizeChange 更新每页条数并回到第一页（el-pagination size-change）', () => {
    const p = usePagination()
    p.onCurrentChange(5)
    p.onSizeChange(100)
    expect(p.perPage.value).toBe(100)
    expect(p.page.value).toBe(1)
  })

  it('setTotal 更新 total（配合后端分页响应）', () => {
    const p = usePagination()
    p.setTotal(257)
    expect(p.total.value).toBe(257)
  })

  it('reset 回到第一页并清空 total（perPage 保留）', () => {
    const p = usePagination({ perPage: 20 })
    p.onCurrentChange(4)
    p.setTotal(99)
    p.reset()
    expect(p.page.value).toBe(1)
    expect(p.total.value).toBe(0)
    expect(p.perPage.value).toBe(20)
  })
})
