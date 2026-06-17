<template>
  <el-config-provider :locale="locale">
    <div class="app-container" :class="themeClass">
      <!-- Header -->
      <header class="app-header">
        <div class="header-inner">
          <div class="header-left">
            <div class="header-brand" @click="goHome">
              <img src="/favicon.png" class="header-logo" alt="logo" />
              <h1 class="header-title">Go HTTP File Server</h1>
            </div>
          </div>

          <div class="header-right">
            <el-button class="header-btn" @click="handleShowMainQrCode">
              <el-icon :size="16"><Camera /></el-icon>
              <span class="header-btn-label">View in Phone</span>
            </el-button>

            <template v-if="fileStore.user">
              <div class="header-user">
                <el-icon :size="16"><User /></el-icon>
                <span>{{ fileStore.user.email ? fileStore.user.name : 'Guest' }}</span>
              </div>
            </template>

            <el-input
              v-model="searchValue"
              class="header-search"
              placeholder="Search files"
              clearable
              @keyup.enter="handleSearch"
              @clear="handleClearSearch"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>

            <el-dropdown @command="handleThemeChange">
              <el-button class="header-btn theme-toggle">
                <el-icon :size="16"><MoonNight /></el-icon>
                <span>{{ currentTheme }}</span>
                <el-icon :size="12" class="chevron"><ArrowDown /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item
                    v-for="theme in availableThemes"
                    :key="theme"
                    :command="theme"
                    :class="{ 'dropdown-active': theme === currentTheme }"
                  >
                    {{ theme }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>
      </header>

      <!-- Main Content -->
      <main class="app-main">
        <div class="main-inner">
          <Breadcrumb />
          <FileList />
        </div>
        <footer class="app-footer">
          <a
            href="https://github.com/codeskyblue/gohttpserver"
            target="_blank"
            class="footer-link"
          >gohttpserver</a>
          <span class="footer-sep">v{{ version }}</span>
          <span class="footer-sep">by</span>
          <a
            href="https://github.com/codeskyblue"
            target="_blank"
            class="footer-link footer-link--dim"
          >codeskyblue</a>
        </footer>
      </main>

      <QRCodeModal
        v-model:visible="showQrCodeModal"
        :file="currentQrFile"
        :current-path="currentPath"
      />
    </div>
  </el-config-provider>
</template>

<script setup lang="ts">
import { onMounted, computed, ref } from 'vue'
import { useFileStore } from './stores/fileStore'
import { useTheme } from './composables/useTheme'
import Breadcrumb from './components/Breadcrumb.vue'
import FileList from './components/FileList.vue'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'
import QRCodeModal from './components/QrCodeModal.vue'
import type { FileItem } from './types'
import {
  Camera, User, Search, MoonNight, ArrowDown
} from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

const fileStore = useFileStore()
const { currentTheme, availableThemes, setTheme } = useTheme()

const locale = zhCn
const showQrCodeModal = ref(false)
const currentQrFile = ref<FileItem | null>(null)
const searchValue = ref('')

const version = computed(() => fileStore.version)
const currentPath = computed(() => window.location.pathname)
const themeClass = computed(() => `theme-${currentTheme.value}`)

function goHome() {
  fileStore.loadFiles('/')
}

function handleShowMainQrCode() {
  currentQrFile.value = null
  showQrCodeModal.value = true
}

function handleSearch() {
  fileStore.loadFiles('/', searchValue.value)
}

function handleClearSearch() {
  searchValue.value = ''
  fileStore.loadFiles('/')
}

function handleThemeChange(theme: string) {
  setTheme(theme as any)
  ElMessage.success(`Theme changed to ${theme}`)
}

onMounted(async () => {
  await fileStore.loadFiles(window.location.pathname)
  await fileStore.loadUser()
  await fileStore.loadSystemInfo()

  window.addEventListener('popstate', () => {
    fileStore.loadFiles(window.location.pathname)
  })
})
</script>

<style>
/* ── Reset ── */
html, body, #app {
  margin: 0;
  padding: 0;
  height: 100%;
  width: 100%;
}

/* ── Layout ── */
.app-container {
  display: flex;
  flex-direction: column;
  min-height: 100dvh;
  background-color: var(--el-bg-color-page);
  transition: background-color var(--transition-base);
}

/* ── Header ── */
.app-header {
  position: sticky;
  top: 0;
  z-index: 100;
  flex-shrink: 0;
  height: 56px;
  background: color-mix(in srgb, var(--el-bg-color) 82%, transparent);
  border-bottom: 1px solid var(--el-border-color-lighter);
  backdrop-filter: saturate(180%) blur(12px);
  -webkit-backdrop-filter: saturate(180%) blur(12px);
  transition: background-color var(--transition-base),
    border-color var(--transition-base);
}

@media (prefers-reduced-transparency: reduce) {
  .app-header {
    background: var(--el-bg-color);
    backdrop-filter: none;
    -webkit-backdrop-filter: none;
  }
}

.header-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 100%;
  max-width: 1440px;
  margin: 0 auto;
  padding: 0 24px;
}

.header-left {
  display: flex;
  align-items: center;
}

.header-brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  user-select: none;
  padding: 4px 8px;
  margin: -4px -8px;
  border-radius: var(--radius-md);
  transition: background var(--transition-base);
}

.header-brand:hover {
  background: var(--el-fill-color-light);
}

.header-brand:active {
  scale: 0.985;
}

.header-logo {
  width: 22px;
  height: 22px;
  flex-shrink: 0;
}

.header-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  transition: color var(--transition-base);
}

.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-btn {
  font-size: 13px;
  color: var(--el-text-color-regular);
  transition: color var(--transition-base);
}

.header-btn-label {
  margin-left: 2px;
}

.header-user {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0 4px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}

.header-search {
  width: 220px;
}

.theme-toggle .chevron {
  margin-left: 2px;
  opacity: 0.5;
}

/* ── Main ── */
.app-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  width: 100%;
  box-sizing: border-box;
}

.main-inner {
  flex: 1;
  width: 100%;
  max-width: 1440px;
  margin: 0 auto;
  padding: 0 24px;
  box-sizing: border-box;
}

/* ── Footer ── */
.app-footer {
  max-width: 1440px;
  width: 100%;
  margin: 0 auto;
  padding: 24px 24px 16px;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.footer-link {
  color: var(--el-text-color-secondary);
  text-decoration: none;
  transition: color var(--transition-base);
}

.footer-link:hover {
  color: var(--el-color-primary);
}

.footer-link--dim {
  color: var(--el-text-color-placeholder);
}

.footer-sep {
  color: var(--el-border-color-darker);
}

/* ── Themes ── */
/* Each theme shifts the page background, primary accent, and surface tints.
   The changes are subtle — like room lighting, not paint colors. */

.theme-black {
  --el-color-primary: #4b5563;
  --el-color-primary-light-3: #9ca3af;
  --el-color-primary-light-5: #d1d5db;
  --el-color-primary-light-7: #e5e7eb;
  --el-color-primary-light-8: #f3f4f6;
  --el-color-primary-light-9: #f9fafb;
  --el-bg-color-page: #f5f6f8;
  --el-fill-color-light: #f1f3f5;
  --el-fill-color: #eef0f2;
}

/* White — Apple + Notion inspired warm minimalism.
   Not pure-white: subtle warmth in surfaces, soft hairlines, deep ink. */
.theme-white {
  /* Accent: refined blue (Apple Action Blue) */
  --el-color-primary: #0066cc;
  --el-color-primary-light-3: #7ab8f5;
  --el-color-primary-light-5: #b0d4f7;
  --el-color-primary-light-7: #d4e8fb;
  --el-color-primary-light-8: #e5f0fc;
  --el-color-primary-light-9: #f0f6fd;
  --el-color-primary-dark-2: #0055aa;
  /* Ink: deep near-black with warmth (Apple #1d1d1f) */
  --el-text-color-primary: #1d1d1f;
  --el-text-color-regular: #3a3a3c;
  --el-text-color-secondary: #6e6e73;
  --el-text-color-placeholder: #8e8e93;
  /* Surface: warm-tinted whites (Apple canvas-parchment + Notion surface) */
  --el-bg-color: #ffffff;
  --el-bg-color-page: #f5f5f7;
  --el-bg-color-overlay: #ffffff;
  --el-fill-color: #efeff1;
  --el-fill-color-light: #f5f5f7;
  --el-fill-color-lighter: #fafafa;
  --el-fill-color-extra-light: #fafafc;
  --el-fill-color-blank: #ffffff;
  /* Borders: soft warm-gray hairlines (Notion hairline #e5e3df adapted) */
  --el-border-color: #d2d2d7;
  --el-border-color-light: #dfdfe3;
  --el-border-color-lighter: #e8e8ed;
  --el-border-color-extra-light: #f0f0f2;
  --el-border-color-dark: #c4c4c9;
  --el-border-color-darker: #aeaeb5;
}

.theme-green {
  --el-color-primary: #059669;
  --el-color-primary-light-3: #6ee7b7;
  --el-color-primary-light-5: #a7f3d0;
  --el-color-primary-light-7: #d1fae5;
  --el-color-primary-light-8: #ecfdf5;
  --el-color-primary-light-9: #f0fdf6;
  --el-bg-color-page: #f4f7f5;
  --el-fill-color-light: #eef5f0;
  --el-fill-color: #eaf2ec;
}

.theme-cyan {
  --el-color-primary: #0891b2;
  --el-color-primary-light-3: #67e8f9;
  --el-color-primary-light-5: #a5f3fc;
  --el-color-primary-light-7: #cffafe;
  --el-color-primary-light-8: #e6fcfe;
  --el-color-primary-light-9: #f0fdff;
  --el-bg-color-page: #f3f7f9;
  --el-fill-color-light: #edf4f7;
  --el-fill-color: #e9f1f5;
}

/* ── Dropdown active item ── */
.dropdown-active {
  background-color: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

/* ── Responsive: Tablet ── */
@media (max-width: 768px) {
  .header-inner {
    padding: 0 16px;
  }

  .main-inner {
    padding: 0 16px;
  }

  .header-title {
    font-size: 14px;
  }

  .header-search {
    width: 140px;
  }

  .header-btn-label,
  .header-user span,
  .theme-toggle span {
    display: none;
  }
}

/* ── Responsive: Phone ── */
@media (max-width: 480px) {
  .header-inner {
    padding: 0 12px;
  }

  .main-inner {
    padding: 0 12px;
  }

  .header-title {
    font-size: 13px;
  }

  .header-brand {
    gap: 6px;
    padding: 4px 6px;
    margin: -4px -6px;
  }

  .header-logo {
    width: 20px;
    height: 20px;
  }

  .header-search {
    width: 100px;
  }

  .app-footer {
    padding: 16px 12px 12px;
    font-size: 11px;
  }
}
</style>
