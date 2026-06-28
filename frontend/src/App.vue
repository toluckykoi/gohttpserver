<template>
  <el-config-provider :locale="locale">
    <div class="app-shell" :data-theme="currentTheme">
      <!-- Header: frosted-glass bar that floats over content.
           Uses backdrop-filter for the modern translucent effect.
           On phones it collapses search/theme into a more compact row. -->
      <header class="app-header">
        <div class="header-inner">
          <div class="header-left">
            <div class="header-brand" @click="goHome">
              <span class="header-logo" aria-hidden="true">
                <img src="/favicon.png" alt="logo" width="22" height="22" style="border-radius:4px">
              </span>
              <div class="header-titles">
                <h1 class="header-title">GoHTTPServer</h1>
                <span class="header-subtitle">File Manager</span>
              </div>
            </div>
          </div>

          <div class="header-right">
            <!-- Phone QR button: hidden on phone (can't scan your own screen).
                 Kept as a square icon chip on desktop for symmetry with the
                 other controls. -->
            <el-tooltip content="View on phone (QR)" placement="bottom">
              <el-button
                class="header-chip header-chip--qr"
                circle
                aria-label="Show QR code"
                @click="handleShowMainQrCode"
              >
                <el-icon :size="17"><Camera /></el-icon>
              </el-button>
            </el-tooltip>

            <template v-if="fileStore.user">
              <div class="header-user" :title="fileStore.user.email || ''">
                <span class="header-avatar" aria-hidden="true">
                  {{ (fileStore.user.name || fileStore.user.email || '?').charAt(0).toUpperCase() }}
                </span>
                <span class="header-user-name">
                  {{ fileStore.user.name || fileStore.user.email || 'Guest' }}
                </span>
              </div>
            </template>

            <div class="header-search">
              <el-input
                v-model="searchValue"
                placeholder="Search files…"
                clearable
                @keyup.enter="handleSearch"
                @clear="handleClearSearch"
              >
                <template #prefix>
                  <el-icon :size="15" class="header-search-icon"><Search /></el-icon>
                </template>
              </el-input>
              <kbd v-if="!isPhone" class="header-search-kbd">/</kbd>
            </div>

            <!-- Theme picker: a small grid of swatches. Visual feedback
                 beats a dropdown of color names — you see what you get. -->
            <el-popover
              placement="bottom-end"
              :width="220"
              trigger="click"
              :show-arrow="false"
              popper-class="theme-popover"
            >
              <template #reference>
                <el-button
                  class="header-chip theme-toggle"
                  circle
                  :title="`Theme: ${currentTheme}`"
                  aria-label="Switch theme"
                >
                  <span class="theme-swatch" :data-theme="currentTheme" aria-hidden="true"></span>
                </el-button>
              </template>
              <div class="theme-grid">
                <button
                  v-for="theme in availableThemes"
                  :key="theme"
                  class="theme-card"
                  :class="{ 'theme-card--active': theme === currentTheme }"
                  :aria-pressed="theme === currentTheme"
                  @click="handleThemeChange(theme)"
                >
                  <span class="theme-card-swatch" :data-theme="theme" aria-hidden="true">
                    <span class="theme-card-swatch-light"></span>
                    <span class="theme-card-swatch-dark"></span>
                  </span>
                  <span class="theme-card-name">{{ theme }}</span>
                  <el-icon v-if="theme === currentTheme" :size="14" class="theme-card-check"><Check /></el-icon>
                </button>
              </div>
            </el-popover>
          </div>
        </div>
      </header>

      <!-- Main content area -->
      <main class="app-main">
        <div class="main-inner">
          <Breadcrumb />
          <FileList />
        </div>
        <footer class="app-footer">
          <div class="footer-card">
            <a
              href="https://github.com/codeskyblue/gohttpserver"
              target="_blank"
              rel="noopener"
              class="footer-product"
              title="View on GitHub"
            >
              gohttpserver
            </a>
            <span
              v-if="version && version !== 'unknown'"
              class="footer-version"
            >{{ version }}</span>
            <span class="footer-divider" aria-hidden="true">·</span>
            <span class="footer-byline">
              built with <span class="footer-heart" aria-hidden="true">♥</span> by
            </span>
            <a
              href="https://github.com/codeskyblue"
              target="_blank"
              rel="noopener"
              class="footer-author"
            >codeskyblue</a>
          </div>
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
import { onMounted, onBeforeUnmount, computed, ref, defineAsyncComponent } from 'vue'
import { useFileStore } from './stores/fileStore'
import { useTheme } from './composables/useTheme'
import Breadcrumb from './components/Breadcrumb.vue'
import FileList from './components/FileList.vue'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'
// Lazy-load QR modal: it's only shown when the user explicitly
// taps the "view on phone" chip in the header. Pulling qrcode + its
// canvas dependencies up front for that one tap would be wasteful.
const QRCodeModal = defineAsyncComponent(() => import('./components/QrCodeModal.vue'))
import type { FileItem } from './types'
import {
  Camera, Search, Check
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

// Reactive phone breakpoint. Used to hide kbd hint, toggle button
// labels, etc. without re-mounting the whole tree.
const isPhone = ref(window.innerWidth < 640)
function handleResize() {
  isPhone.value = window.innerWidth < 640
}

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
  ElMessage.success(`Theme: ${theme}`)
}

// "/" focuses search; Esc clears. These are file-manager-y shortcuts
// every user already expects from tools like VS Code / GitHub.
function handleShortcut(e: KeyboardEvent) {
  const t = e.target as HTMLElement | null
  const isInput =
    t?.tagName === 'INPUT' ||
    t?.tagName === 'TEXTAREA' ||
    (t?.isContentEditable ?? false)
  if (e.key === '/' && !isInput && !e.ctrlKey && !e.metaKey) {
    e.preventDefault()
    const el = document.querySelector<HTMLInputElement>('.header-search input')
    el?.focus()
  }
}

onMounted(async () => {
  await fileStore.loadFiles(window.location.pathname)
  await fileStore.loadUser()
  await fileStore.loadSystemInfo()

  window.addEventListener('popstate', () => {
    fileStore.loadFiles(window.location.pathname)
  })
  window.addEventListener('resize', handleResize, { passive: true })
  window.addEventListener('keydown', handleShortcut)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
  window.removeEventListener('keydown', handleShortcut)
})
</script>

<style scoped>
/* ════════════════════════════════════════════════════════════════
   Layout
   ════════════════════════════════════════════════════════════════ */

.app-shell {
  display: flex;
  flex-direction: column;
  min-height: 100dvh;
  background: var(--el-bg-color-page);
  transition: background-color var(--transition-base);
}

/* ════════════════════════════════════════════════════════════════
   Header — frosted glass
   ════════════════════════════════════════════════════════════════ */

.app-header {
  position: sticky;
  top: 0;
  z-index: var(--z-sticky);
  flex-shrink: 0;
  height: 64px;
  background: color-mix(in srgb, var(--el-bg-color) 70%, transparent);
  border-bottom: 1px solid color-mix(in srgb, var(--el-border-color) 50%, transparent);
  backdrop-filter: saturate(180%) blur(20px);
  -webkit-backdrop-filter: saturate(180%) blur(20px);
  transition:
    background-color var(--transition-base),
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
  padding: 0 28px;
  gap: 16px;
}

/* ── Brand ── */
.header-brand {
  display: inline-flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  user-select: none;
  padding: 6px 10px 6px 6px;
  margin: -6px -10px -6px -6px;
  border-radius: var(--radius-lg);
  transition: background var(--transition-base);
  min-width: 0;
  flex-shrink: 1;
}

.header-brand:hover {
  background: var(--el-fill-color-light);
}

.header-brand:active {
  transform: scale(0.985);
}

.header-logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  color: var(--el-color-primary);
  background: color-mix(in srgb, var(--el-color-primary) 12%, transparent);
  border-radius: var(--radius-md);
  transition: background var(--transition-base),
              color var(--transition-base);
}

.header-brand:hover .header-logo {
  background: color-mix(in srgb, var(--el-color-primary) 18%, transparent);
}

.header-titles {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.header-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  letter-spacing: -0.015em;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
  line-height: 1.2;
}

.header-subtitle {
  font-size: 11px;
  font-weight: 500;
  color: var(--el-text-color-placeholder);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  line-height: 1.3;
}

/* ── Right cluster ── */
.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.header-chip {
  /* Square icon button — uniform across the header.
     36×36 is the "comfortable desktop" target; min-width:0 lets it
     shrink on narrow viewports without overflowing. */
  width: 36px;
  height: 36px;
  padding: 0;
  color: var(--el-text-color-regular);
}

.header-chip :deep(.el-icon) {
  margin: 0;
}

/* Search field with embedded kbd hint */
.header-search {
  position: relative;
  width: 240px;
}

.header-search :deep(.el-input__wrapper) {
  padding: 4px 12px;
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--el-fill-color) 50%, transparent);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--el-border-color) 50%, transparent) inset !important;
  transition: background var(--transition-base),
              box-shadow var(--transition-base);
}

.header-search :deep(.el-input__wrapper:hover) {
  background: var(--el-fill-color-light);
  box-shadow: 0 0 0 1px var(--el-border-color) inset !important;
}

.header-search :deep(.el-input__wrapper.is-focus) {
  background: var(--el-bg-color) !important;
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--el-color-primary) 25%, transparent) inset,
              0 0 0 4px color-mix(in srgb, var(--el-color-primary) 12%, transparent) !important;
}

.header-search-icon {
  color: var(--el-text-color-placeholder);
}

.header-search-kbd {
  position: absolute;
  top: 50%;
  right: 8px;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 500;
  color: var(--el-text-color-placeholder);
  background: color-mix(in srgb, var(--el-fill-color) 80%, transparent);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-xs);
  pointer-events: none;
  user-select: none;
}

/* User pill */
.header-user {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px 4px 4px;
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-regular);
  background: color-mix(in srgb, var(--el-fill-color) 40%, transparent);
  border-radius: var(--radius-pill);
  white-space: nowrap;
  max-width: 200px;
  transition: background var(--transition-base);
}

.header-user:hover {
  background: var(--el-fill-color-light);
}

.header-avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  font-size: 11px;
  font-weight: 600;
  color: var(--el-color-primary);
  background: color-mix(in srgb, var(--el-color-primary) 15%, transparent);
  border-radius: 50%;
}

.header-user-name {
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
}

/* Theme toggle swatch — visible in any theme */
.theme-swatch {
  display: inline-block;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 1.5px solid var(--el-border-color);
  box-sizing: border-box;
  flex-shrink: 0;
  background: linear-gradient(135deg,
    color-mix(in srgb, var(--el-bg-color) 100%, transparent) 50%,
    color-mix(in srgb, var(--el-color-primary) 100%, transparent) 50%);
  transition: border-color var(--transition-base),
              transform var(--transition-base);
}

.theme-toggle:hover .theme-swatch {
  transform: scale(1.08);
}

/* ── Theme popover (visual swatch grid) ── */
:deep(.theme-popover) {
  padding: 12px !important;
}

.theme-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 6px;
}

.theme-card {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-regular);
  background: transparent;
  border: 1px solid transparent;
  border-radius: var(--radius-md);
  cursor: pointer;
  text-align: left;
  transition: background var(--transition-base),
              border-color var(--transition-base),
              color var(--transition-base);
}

.theme-card:hover {
  background: var(--el-fill-color-light);
}

.theme-card--active {
  background: color-mix(in srgb, var(--el-color-primary) 8%, transparent);
  border-color: color-mix(in srgb, var(--el-color-primary) 25%, transparent);
  color: var(--el-text-color-primary);
}

.theme-card-swatch {
  display: inline-flex;
  width: 22px;
  height: 22px;
  flex-shrink: 0;
  border-radius: 6px;
  overflow: hidden;
  border: 1px solid var(--el-border-color-lighter);
}

/* Each theme renders as a small split-swatch showing its dominant
   light + dark side. The user sees the actual palette at a glance. */
.theme-card-swatch-light,
.theme-card-swatch-dark {
  flex: 1;
  height: 100%;
}
.theme-card-swatch[data-theme="white"] .theme-card-swatch-light    { background: #f5f5f7; }
.theme-card-swatch[data-theme="white"] .theme-card-swatch-dark     { background: #0066cc; }
.theme-card-swatch[data-theme="black"] .theme-card-swatch-light    { background: #4b5563; }
.theme-card-swatch[data-theme="black"] .theme-card-swatch-dark     { background: #1f2937; }
.theme-card-swatch[data-theme="green"] .theme-card-swatch-light    { background: #ecfdf5; }
.theme-card-swatch[data-theme="green"] .theme-card-swatch-dark     { background: #059669; }
.theme-card-swatch[data-theme="cyan"]  .theme-card-swatch-light    { background: #ecfeff; }
.theme-card-swatch[data-theme="cyan"]  .theme-card-swatch-dark     { background: #0891b2; }

.theme-card-name {
  flex: 1;
  text-transform: capitalize;
}

.theme-card-check {
  color: var(--el-color-primary);
}

/* ════════════════════════════════════════════════════════════════
   Main
   ════════════════════════════════════════════════════════════════ */

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
  padding: 8px 28px;
  box-sizing: border-box;
}

/* ════════════════════════════════════════════════════════════════
   Footer
   ════════════════════════════════════════════════════════════════ */

.app-footer {
  max-width: 1440px;
  width: 100%;
  margin: 0 auto;
  padding: 28px 28px 20px;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: flex-end;
}

.footer-card {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  font-size: 12.5px;
  line-height: 1.2;
  background: color-mix(in srgb, var(--el-bg-color) 70%, transparent);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-pill);
  color: var(--el-text-color-secondary);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  transition:
    border-color var(--transition-base),
    background-color var(--transition-base);
}

.footer-card:hover {
  border-color: var(--el-border-color);
  background: var(--el-bg-color);
}

.footer-product {
  font-weight: 600;
  font-size: 13px;
  color: var(--el-text-color-primary);
  text-decoration: none;
  letter-spacing: -0.01em;
  transition: color var(--transition-base);
}

.footer-product:hover {
  color: var(--el-color-primary);
}

.footer-version {
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  padding: 2px 7px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color);
  border-radius: var(--radius-pill);
  line-height: 1.4;
}

.footer-divider {
  color: var(--el-text-color-placeholder);
  opacity: 0.5;
  user-select: none;
}

.footer-byline {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.footer-heart {
  color: #ef4444;
  display: inline-block;
  transform: translateY(-0.5px);
  transition: transform var(--transition-base);
}

.footer-card:hover .footer-heart {
  transform: translateY(-0.5px) scale(1.15);
}

.footer-author {
  font-weight: 500;
  color: var(--el-text-color-regular);
  text-decoration: none;
  transition: color var(--transition-base);
}

.footer-author:hover {
  color: var(--el-color-primary);
}

/* ════════════════════════════════════════════════════════════════
   Responsive — Tablet
   ════════════════════════════════════════════════════════════════ */

@media (max-width: 768px) {
  .app-header { height: 56px; }
  .header-inner { padding: 0 16px; gap: 8px; }
  .main-inner { padding: 8px 16px; }

  .header-subtitle { display: none; }

  /* Phone QR is unusable from the device rendering the page. */
  .header-chip--qr { display: none; }

  .header-right { gap: 6px; }
  .header-user-name { display: none; }
  .header-user { padding: 4px; }

  .header-search {
    flex: 1 1 0;
    min-width: 100px;
    max-width: 220px;
  }

  .app-footer {
    justify-content: center;
    padding: 20px 16px 16px;
  }
}

/* ════════════════════════════════════════════════════════════════
   Responsive — Phone
   ════════════════════════════════════════════════════════════════ */

@media (max-width: 480px) {
  .header-inner { padding: 0 12px; }
  .main-inner { padding: 8px 12px; }

  .header-title { font-size: 14px; }

  .header-search {
    min-width: 80px;
    max-width: 160px;
  }

  .header-search-kbd { display: none; }

  .app-footer { padding: 16px 12px 14px; }

  .footer-card {
    flex-wrap: wrap;
    justify-content: center;
    row-gap: 6px;
    column-gap: 6px;
    padding: 6px 10px;
    font-size: 11.5px;
  }

  .footer-product { font-size: 12.5px; }
}

/* ════════════════════════════════════════════════════════════════
   Tiny phones — drop brand title text, keep just the logo
   ════════════════════════════════════════════════════════════════ */

@media (max-width: 360px) {
  .header-titles { display: none; }
}
</style>

<!--
  ════════════════════════════════════════════════════════════════
  Theme palette — mounted on <html data-theme="..."> by useTheme.
  Element Plus tokens are remapped per theme so every component
  inherits the chosen look without per-component overrides.
  ════════════════════════════════════════════════════════════════
-->

<style>
/* ────────────────────────────────────────────────────────────────
   Black — original cool-grey theme, refined.
   ──────────────────────────────────────────────────────────────── */
[data-theme="black"] {
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

/* ────────────────────────────────────────────────────────────────
   White — Apple + Notion inspired warm minimalism.
   ──────────────────────────────────────────────────────────────── */
[data-theme="white"] {
  --el-color-primary: #0066cc;
  --el-color-primary-light-3: #7ab8f5;
  --el-color-primary-light-5: #b0d4f7;
  --el-color-primary-light-7: #d4e8fb;
  --el-color-primary-light-8: #e5f0fc;
  --el-color-primary-light-9: #f0f6fd;
  --el-color-primary-dark-2: #0055aa;
  --el-text-color-primary: #1d1d1f;
  --el-text-color-regular: #3a3a3c;
  --el-text-color-secondary: #6e6e73;
  --el-text-color-placeholder: #8e8e93;
  --el-bg-color: #ffffff;
  --el-bg-color-page: #f5f5f7;
  --el-bg-color-overlay: #ffffff;
  --el-fill-color: #efeff1;
  --el-fill-color-light: #f5f5f7;
  --el-fill-color-lighter: #fafafa;
  --el-fill-color-extra-light: #fafafc;
  --el-fill-color-blank: #ffffff;
  --el-border-color: #d2d2d7;
  --el-border-color-light: #dfdfe3;
  --el-border-color-lighter: #e8e8ed;
  --el-border-color-extra-light: #f0f0f2;
  --el-border-color-dark: #c4c4c9;
  --el-border-color-darker: #aeaeb5;
}

/* ────────────────────────────────────────────────────────────────
   Green — emerald accent.
   ──────────────────────────────────────────────────────────────── */
[data-theme="green"] {
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

/* ────────────────────────────────────────────────────────────────
   Cyan — bright sky accent.
   ──────────────────────────────────────────────────────────────── */
[data-theme="cyan"] {
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
</style>