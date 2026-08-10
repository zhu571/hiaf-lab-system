/// <reference types="node" />
import { defineConfig, devices } from '@playwright/test'

// E2E 冒烟测试（测试策略方案 §4.4，P2 首版仅本地跑，不进 CI 门禁）。
// 前置：scripts/test-e2e.sh 已拉起 postgres（迁移 001-036）+ Go server(8000) + 前端 dev server(5173)。
// baseURL 可用 E2E_BASE_URL 覆盖（默认 127.0.0.1:5173，vite 代理 /api → 8000）。
export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 60_000,
  expect: {
    timeout: 10_000
  },
  reporter: [['list']],
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://127.0.0.1:5173',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    viewport: { width: 1440, height: 900 }
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] }
    }
  ]
})
