import { expect, test } from '@playwright/test'
import { ADMIN, login } from './helpers'

// 用例（结构改版 R2 §3.3 登记）：全局命令面板冒烟——
// 顶栏触发框 / Ctrl+K 唤起、过滤（label + 路径段）、Enter 跳转、Esc 关闭。
// 断言锚点：.palette-trigger（AppLayout 顶栏）、.command-palette / .palette-input /
// .palette-group / .palette-item（CommandPalette.vue）。
test.describe('命令面板', () => {
  test('Ctrl+K 唤起 → 三组渲染 → 过滤 sensors → Enter 跳转 /sensors → 再唤起 Esc 关闭', async ({ page }) => {
    await login(page, ADMIN.username, ADMIN.password)

    // 顶栏触发框（R2 落位：用户菜单左侧）
    await expect(page.locator('.palette-trigger')).toBeVisible()

    // Ctrl+K 唤起（全局面板，输入框自动聚焦）
    await page.keyboard.press('Control+k')
    const dialog = page.locator('.command-palette')
    await expect(dialog).toBeVisible()
    const input = page.locator('.palette-input')
    await expect(input).toBeFocused()

    // admin 三组渲染：页面 / 项目 / 动作
    const groups = dialog.locator('.palette-group')
    await expect(groups).toHaveCount(3)
    await expect(dialog.locator('.palette-item').filter({ hasText: '写日报' })).toBeVisible()

    // 过滤：路径段 sensors 命中「传感器」页（label 中文同时参与匹配）
    await input.fill('sensors')
    const hit = dialog.locator('.palette-item', { hasText: '传感器' })
    await expect(hit).toBeVisible()
    await expect(dialog.locator('.palette-item')).toHaveCount(1)

    // Enter 执行当前高亮项（过滤后首项）→ 跳转 /sensors，面板关闭
    await page.keyboard.press('Enter')
    await page.waitForURL((url) => url.pathname === '/sensors')
    await expect(dialog).toBeHidden()

    // 再次 Ctrl+K 唤起，Esc 关闭（el-dialog close-on-press-escape 既有行为）
    await page.keyboard.press('Control+k')
    await expect(dialog).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
  })
})
