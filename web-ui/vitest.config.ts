import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// 前端单元测试配置：jsdom 环境（纯逻辑 + 组件挂载）。
// vue() 插件是组件测试挂载前提（SFC 编译）；element-plus 按需注册仍留在 vite.config.ts，
// 测试侧改由 src/test-utils/setup.ts 全量注册（含 ElMessage/ElMessageBox 打桩）。
// 测试文件约定：web-ui/src/**/__tests__/*.test.ts（与被测源码同目录，便于就近维护）。
export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['src/test-utils/setup.ts'],
    include: ['src/**/__tests__/**/*.test.ts'],
    exclude: ['node_modules/**', 'dist/**'],
    coverage: {
      provider: 'v8',
      reporter: ['text'],
      include: ['src/stores/**', 'src/utils/**', 'src/i18n/**']
    }
  }
})
