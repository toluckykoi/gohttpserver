import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import UnoCSS from 'unocss/vite'
import Components from 'unplugin-vue-components/vite'
import AutoImport from 'unplugin-auto-import/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { resolve } from 'path'

export default defineConfig({
  plugins: [
    vue(),
    UnoCSS(),
    // Element Plus 按需自动引入（组件 + 各组件 CSS）。
    Components({
      resolvers: [
        ElementPlusResolver({
          importStyle: 'css',
          dts: false
        })
      ],
      dirs: ['src'],
      extensions: ['vue', 'ts', 'tsx', 'jsx', 'js'],
      deep: true,
    }),
    // Element Plus 命令式 API（ElMessage 等）的自动引入。
    AutoImport({
      resolvers: [
        ElementPlusResolver({
          importStyle: 'css'
        })
      ],
      imports: ['vue', 'vue-router'],
      dts: false,
      cache: true
    })
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  server: {
    proxy: {
      '/': {
        target: 'http://localhost:8000',
        changeOrigin: true,
        // 后端有 Origin 同源校验（CSRF 防护），dev 代理需把 Origin 重写为后端地址。
        headers: { origin: 'http://localhost:8000' },
        bypass: (req) => {
          const u = (req.url || '').split('?')[0]
          // Vite 的整个 base 前缀（入口 HTML、源码模块、依赖、虚拟模块、模板资产
          // ?import 等）一律由 Vite 伺服；其余（API、文件 JSON、文件内容、下载）
          // 代理到后端，与生产行为一致。漏掉任何一种都会让模块图卡住——例如
          // favicon.png?import 被代理到后端时返回 image/png，模块校验直接失败。
          if (
            u === '/-/frontend' ||
            u.startsWith('/-/frontend/') ||
            u.startsWith('/@') ||
            u.startsWith('/node_modules/')
          ) {
            return req.url
          }
          return null
        }
      }
    }
  },
  base: '/-/frontend/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      onwarn(warning, warn) {
        if (
          warning.code === 'INVALID_ANNOTATION' &&
          /@vueuse\/core/.test(warning.message ?? '')
        ) {
          return
        }
        warn(warning)
      },
      output: {
        manualChunks: (id) => {
          if (!id.includes('node_modules')) return undefined

          if (id.includes('/element-plus/')) {
            return 'element-plus'
          }

          if (
            id.includes('/vue/') ||
            id.includes('/@vue/') ||
            id.includes('/vue-router/') ||
            id.includes('/pinia/') ||
            id.includes('/@vueuse/') ||
            id.includes('/@element-plus/icons-vue/')
          ) {
            return 'vue-vendor'
          }

          if (id.includes('/qrcode/')) {
            return 'qrcode'
          }

          if (id.includes('/dayjs/')) {
            return 'dayjs'
          }

          if (id.includes('/marked/')) {
            return 'marked'
          }

          if (id.includes('/unocss/')) {
            return 'unocss'
          }
          // 不设 'vendor' 兜底分包；其余依赖都留在 app chunk 里。
        }
      }
    },
    // element-plus chunk 较大，但命令式 API 下无法避免。
    chunkSizeWarningLimit: 1024
  }
})