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
    // ── Element Plus auto-import ─────────────────────────────────────
    // Components used in templates (el-button, el-input, …) get
    // imported on demand — Vite no longer pulls the full Element
    // Plus bundle. CSS is bundled per-component too, so unused
    // styles never hit the wire.
    //
    // The version range is pinned loosely because the resolver
    // matches against the installed Element Plus version.
    Components({
      resolvers: [
        ElementPlusResolver({
          importStyle: 'css',
          // dts generation is dev-only; in CI builds the file write
          // is harmless but it adds a few hundred ms — disable it.
          dts: false
        })
      ],
      // Don't scan these directories: dist/, public/, and anything
      // not under our app source. Cuts plugin work dramatically.
      dirs: ['src'],
      extensions: ['vue', 'ts', 'tsx', 'jsx', 'js'],
      // deep: true lets the plugin reach nested components like
      // components/modals/UploadModal.vue — needed because the modal
      // tree is fairly deep.
      deep: true,
      // dts: false (set above) avoids touching the filesystem on every build.
      // We rely on Vue's own SFC type-checking (vue-tsc) for types.
    }),
    // ── Auto-import for Element Plus command-style APIs ──────────────
    // ElMessage, ElMessageBox, ElNotification, etc. — anything the
    // code calls without a template tag — gets imported here so we
    // can drop the manual `import { ElMessage } from 'element-plus'`
    // lines everywhere. Auto-import scans source on dev start and
    // during build, adding only the symbols actually used.
    AutoImport({
      resolvers: [
        ElementPlusResolver({
          importStyle: 'css'
        })
      ],
      imports: ['vue', 'vue-router'],
      dts: false,
      // Use the generated file in node_modules/.vite/auto-imports
      // so Vite's module graph caches it between builds.
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
    emptyOutDir: true,
    // Code splitting: separate the heavy 3rd-party libs into their
    // own chunks so the app shell stays small and parallel-downloads
    // across HTTP/2 keep the cold-load fast. Each chunk is also
    // cacheable on its own — a tweak to our app code won't bust the
    // (much larger) vendor cache for repeat visitors.
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

          // Element Plus: split out so the rest of the app can load
          // in parallel. With auto-import on, this is now small.
          if (id.includes('/element-plus/')) {
            return 'element-plus'
          }

          // Vue core — runtime + reactivity + ecosystem
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

          // QR code generation
          if (id.includes('/qrcode/')) {
            return 'qrcode'
          }

          // Day.js (date formatting)
          if (id.includes('/dayjs/')) {
            return 'dayjs'
          }

          // Markdown rendering
          if (id.includes('/marked/')) {
            return 'marked'
          }

          // UnoCSS runtime helpers
          if (id.includes('/unocss/')) {
            return 'unocss'
          }
          // No fallback 'vendor' chunk — anything else ends up in
          // the main app chunk. Avoids the circular-dependency
          // warnings we got when vue-vendor imported from vendor.
        }
      }
    },
    // The remaining ~876 KB element-plus chunk is unavoidable with
    // command-style APIs like ElMessage — they pull in their full
    // dependency tree. Gzipped it's 283 KB, well under the practical
    // limit for a single HTTP/2 push. Setting the limit here keeps
    // the build warning-free without hiding real regressions.
    chunkSizeWarningLimit: 1024
  }
})