import { expect, test } from '@playwright/test'
import { ADMIN, login, todayInShanghai, unique } from './helpers'

// 用例 3：日报 —— 新建日报（填原文）→ 保存 → 出现在历史列表并可回看原文
// 说明：/daily-report 进入即由 todayReport() 自动创建今日日报，无独立「新建」按钮；
// 核心断言为「原文保存成功 + 历史列表可见今日日报 + 详情回看原文一致」。
test.describe('日报', () => {
  test('保存原文 → 历史列表出现今日日报 → 详情可见原文', async ({ page }) => {
    const rawText = unique('e2e日报原文')
    await login(page, ADMIN.username, ADMIN.password)

    // onMounted 里 todayReport() 异步完成会用后端返回值覆写 textarea（rawText.value = report.raw_text），
    // 必须先等该请求完成再填表，否则填充内容会被空字符串覆盖（真实竞态，需显式等待）。
    // 只等响应头仍不够：v-model 覆写发生在响应体完整到达之后，因此必须把响应体消费完（.json()）再填。
    const todayResp = page.waitForResponse(
      (r) => r.request().method() === 'POST' && r.url().includes('/api/v1/daily-reports/today')
    )
    await page.goto('/daily-report')
    await (await todayResp).json()
    await page.locator('textarea').fill(rawText)
    await expect(page.locator('textarea')).toHaveValue(rawText)
    await page.getByRole('button', { name: '保存原文' }).click()
    await expect(page.locator('.el-message--success')).toContainText('已保存')

    // 切到「历史查询」tab（S5 i18n 清偿后 label = dailyHistory.title「日报历史」）→ 出现今天的日报行
    await page.getByRole('tab', { name: /历史查询|日报历史/ }).click()
    await expect(page).toHaveURL(/\/daily-report\/history/)
    const today = todayInShanghai() // en-CA: '2026-08-14'
    // S4 后日期经 formatDate(zh-CN) 渲染为 '2026/08/14'（斜杠），断言兼容两种格式
    const todayRe = new RegExp(today.replace(/-/g, '[-/]'))
    const row = page.locator('.el-table__row').filter({ hasText: todayRe })
    await expect(row.first()).toBeVisible()

    // 点击进入详情：原文内容可回看
    await row.first().locator('td').first().click()
    await expect(page).toHaveURL(/\/daily-reports\//)
    await expect(page.getByText(rawText, { exact: false })).toBeVisible()
  })
})
