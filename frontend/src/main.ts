import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import i18n from './i18n'
import 'virtual:uno.css'
import './styles/global.css'
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'

// Element Plus 由 unplugin-vue-components 按需加载（只为用到的组件打包各组件 CSS）；
// ElMessage / ElMessageBox 属于命令式 API，由调用方显式 import。

const app = createApp(App)

app.use(createPinia())
app.use(i18n)

app.mount('#app')