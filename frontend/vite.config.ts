import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import UnoCSS from 'unocss/vite'
import { resolve } from 'path'

export default defineConfig({
  plugins: [
    vue(),
    UnoCSS()
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
        bypass: (req) => {
          if (req.url?.startsWith('/@') || req.url?.endsWith('.ts') || req.url?.endsWith('.vue')) {
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
    emptyOutDir: true
  }
})