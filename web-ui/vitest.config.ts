import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// 前端单元测试配置：jsdom 环境（纯逻辑 + 组件挂载）。
// vue() 插件是组件测试挂载前提（SFC 编译）；element-plus 按需注册仍留在 vite.config.ts，
// 测试侧改由 src/test-utils/setup.ts 全量注册（含 ElMessage/ElMessageBox 打桩）。
// 测试文件约定：web-ui/src/**/__tests__/*.test.ts（与被测源码同目录，便于就近维护）。
// @ 别名与 vite.config.ts 保持一致（vitest.config.ts 存在时 vite.config.ts 不会加载）。
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  test: {
    environment: 'jsdom',
    globals: true,
    // 固定实验室时区（UTC+8）：datetime 测试断言依赖本地时区渲染，CI 默认 UTC 会红
    env: { TZ: 'Asia/Shanghai' },
    setupFiles: ['src/test-utils/setup.ts'],
    include: ['src/**/__tests__/**/*.test.ts'],
    exclude: ['node_modules/**', 'dist/**'],
    // el-table 重型行渲染用例（StepItemsEditor 30 行 / 批量录入 100 行）在全量并行下需更长时间
    testTimeout: 15000,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'json-summary'],
      // 窄口径收紧（重构方案 S6，§4）：接力链末端。唯一事实源 = 测试方案 §8.4 R4-8：
      // 测试 T4 现状平铺口径七目录 60/60/60/50 → 重构 S0 目录更名随迁 + 补 src/config/**
      // → 重构 S6 窄口径（components/base 等）收紧至 lines/functions ≥70%、branches ≥65%。
      // src/components/business/** 与 src/views/** 不纳入阈值（组件测试断言点 + E2E 兜底）。
      // 实测校准（2026-08-16）：窄口径 76.75/76.64/65.16/75.00（lines/stmts/funcs/branches）。
      // functions 默认目标 70% 按内收规则降为 60%（65.16-5≈60，不低于 T4 已立 60% 下限）。
      include: ['src/stores/**', 'src/utils/**', 'src/i18n/**', 'src/config/**', 'src/layouts/**', 'src/composables/**', 'src/router/**', 'src/api/**', 'src/components/base/**'],
      thresholds: {
        lines: 70,
        statements: 70,
        functions: 60,
        branches: 65
      }
    }
  }
})
