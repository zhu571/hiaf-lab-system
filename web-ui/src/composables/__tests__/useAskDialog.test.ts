import { describe, it, expect } from 'vitest'
import { useAskDialog } from '../useAskDialog'

describe('useAskDialog 抽屉全局开关', () => {
  it('初始态为关闭', () => {
    const { askOpen } = useAskDialog()
    expect(askOpen.value).toBe(false)
  })

  it('openAskDialog 置为打开、closeAskDialog 置回关闭', () => {
    const { askOpen, openAskDialog, closeAskDialog } = useAskDialog()
    openAskDialog()
    expect(askOpen.value).toBe(true)
    closeAskDialog()
    expect(askOpen.value).toBe(false)
  })

  it('多调用方共享同一实例：一处 open 另一处可见（桌面/移动端同源）', () => {
    const a = useAskDialog()
    const b = useAskDialog()
    expect(a.askOpen).toBe(b.askOpen)
    a.openAskDialog()
    expect(b.askOpen.value).toBe(true)
  })
})
