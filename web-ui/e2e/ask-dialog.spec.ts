import { expect, test } from '@playwright/test'
import { ADMIN, login } from './helpers'

// 用例 10（J10）：AI 问答降级旅程（拆 2 用例）——
// E2E 环境无 py-agent-interpret 实例（test-e2e.sh:137 PY_AGENT_INTERPRET_URL 指向
// 127.0.0.1:18099 无服务），ask/chat 必然 502 upstream_error；
// ask/history 为普通 Go API（E2E 环境可达），历史 tab 断言列表加载（空态其一）。
// 断言锚点：.nav-ask（AppLayout.vue:28-30）、.ask-drawer（AskDialog.vue:2）、
// .ask-error（AskDialog.vue:16）、.ask-tabs（AskDialog.vue:4）。
test.describe('AI 问答降级旅程', () => {
  test('提问失败：interpret 不可达时 .ask-error 展示错误文案', async ({ page }) => {
    await login(page, ADMIN.username, ADMIN.password)

    // 侧栏 AI 问答入口 → 抽屉打开
    await page.locator('.nav-ask').click()
    await expect(page.locator('.ask-drawer')).toBeVisible()

    // 输入问题并发送
    const input = page.locator('.chat-input textarea')
    await expect(input).toBeVisible()
    await input.fill('最近的测试数据趋势？')
    await page.getByRole('button', { name: '发送' }).click()

    // interpret 不可达 502 → 回合内 .ask-error 可见（后端 upstream_error message）
    const error = page.locator('.ask-error')
    await expect(error).toBeVisible({ timeout: 15_000 })
    await expect(error).toContainText(/upstream_error|服务|失败|不可达/)
  })

  test('历史 tab：ask/history 可达，列表加载（空态或条目其一）', async ({ page }) => {
    await login(page, ADMIN.username, ADMIN.password)

    await page.locator('.nav-ask').click()
    await expect(page.locator('.ask-drawer')).toBeVisible()

    // 切历史 tab → 历史列表加载（ask/history 为普通 Go API，E2E 环境可达）
    await page.locator('.ask-tabs').getByRole('tab', { name: '历史' }).click()
    // 空历史 → 空态文案；有历史 → 列表条目。二者其一即通过（均为确定性加载结果）。
    // 注意：聊天 tab 也有 .ask-empty（隐藏），选择器须限定在历史列表容器内
    await expect(page.locator('.history-list .ask-empty, .history-item').first()).toBeVisible({ timeout: 15_000 })
  })
})
