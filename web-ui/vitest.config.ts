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
    coverage: {
      provider: 'v8',
      reporter: ['text'],
      // 目录更名随迁（重构方案 S0，§4）：components/ 拆 base/business 子目录。
      // 注：components/** 尚未纳入 include——该口径属测试方案 §8.4 R4-8 接力链的 T4 状态，届时一并加。
      // include/阈值口径唯一事实源 = 测试方案 §8.4 R4-8 接力链，本批不动阈值。
      include: ['src/stores/**', 'src/utils/**', 'src/i18n/**', 'src/config/**']
    }
  }
})
