import { expect, test } from '@playwright/test'
import { ADMIN, VIEWER, api, login, unique } from './helpers'

// 用例（结构改版 R2 §3.3 登记）：通知中心冒烟——
// 铃铛 + 角标、面板三组渲染、查看全部跳转；viewer 无待审组。
// 数据确定性：用例内经 API 建一条今日待办（待办组必有条目，其余组按真实数据渲染）。
// 断言锚点：.notify-trigger / .notify-badge（NotificationCenter.vue）、
// .notify-panel / .notify-group-title / .notify-item / .notify-viewall（NotificationPanel.vue）。
test.describe('通知中心', () => {
  test('admin：铃铛打开面板，三组渲染含新建待办条目，查看全部跳 /todos', async ({ page }) => {
    await login(page, ADMIN.username, ADMIN.password)
    const title = unique('R2通知')
    await api(page, 'POST', '/api/v1/todos', { title, priority: 'high' })

    // 铃铛 + 角标（新建待办后总数 ≥1）
    const trigger = page.locator('.notify-trigger')
    await expect(trigger).toBeVisible()
    await expect(page.locator('.notify-badge .el-badge__content')).toBeVisible()

    // 打开面板：三组标题（admin 含待审组）+ 待办条目
    await trigger.click()
    const panel = page.locator('.notify-panel')
    await expect(panel).toBeVisible()
    await expect(panel).toContainText('待办（今日）')
    await expect(panel).toContainText('活跃告警')
    await expect(panel).toContainText('待审候选')
    await expect(panel.locator('.notify-item', { hasText: title })).toBeVisible()

    // 待办组「查看全部」→ /todos，面板关闭
    await panel.locator('.notify-viewall').first().click()
    await page.waitForURL((url) => url.pathname === '/todos')
    await expect(panel).toBeHidden()
  })

  test('viewer：面板渲染但无待审候选组', async ({ page }) => {
    await login(page, VIEWER.username, VIEWER.password)
    const title = unique('R2通知viewer')
    await api(page, 'POST', '/api/v1/todos', { title, priority: 'medium' })

    await page.locator('.notify-trigger').click()
    const panel = page.locator('.notify-panel')
    await expect(panel).toBeVisible()
    await expect(panel).toContainText('待办（今日）')
    await expect(panel).toContainText('活跃告警')
    await expect(panel).not.toContainText('待审候选')
    await expect(panel.locator('.notify-item', { hasText: title })).toBeVisible()
  })
})
