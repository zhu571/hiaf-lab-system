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

createApp(App).use(createPinia()).use(router).use(i18n).mount('#app')
