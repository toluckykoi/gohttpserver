import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import 'virtual:uno.css'
import './styles/global.css'
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'

// Element Plus is loaded on demand via unplugin-vue-components:
//   - <el-button> etc. in templates get imported automatically
//   - ElMessage / ElMessageBox etc. are imported explicitly below
//     because they're command-style APIs (no template usage)
//   - Per-component CSS is bundled only for the components we use,
//     so the wire size reflects actual usage.
//
// We deliberately do NOT call `app.use(ElementPlus)` — the full
// library would re-introduce the 700+ KiB bundle this is avoiding.

const app = createApp(App)

app.use(createPinia())

app.mount('#app')