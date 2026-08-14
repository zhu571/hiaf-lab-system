import { createApp } from 'vue'
import { createPinia } from 'pinia'
// element-plus 组件经 unplugin-vue-components 按需自动注册并引入样式；
// ElMessage / ElMessageBox 在代码里显式 import，样式需手动引入一次
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'
import './styles/index.css'
import App from './App.vue'
import router from './router'
import { i18n } from './i18n'
import { setupChartDefaults } from './utils/chartTheme'

// 图表配置收口（美术方案 §3.7 M3）：唯一 Chart.register 点 + 全局 defaults，
// 组件内不再各自注册；createApp 前调用一次（主题切换时由调用方 refreshDefaults + 销毁重建）
setupChartDefaults()

createApp(App).use(createPinia()).use(router).use(i18n).mount('#app')
