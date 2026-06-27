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
            <!-- QR button: hidden on phone, see CSS. There's no point
                 scanning the current page's QR from the same device
                 that already rendered it. Drop it to give the title
                 and the remaining controls the room they need. -->
            <el-button class="header-btn header-btn--qr" @click="handleShowMainQrCode">
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
              <el-button class="header-btn theme-toggle" :title="`Theme: ${currentTheme}`">
                <!-- Theme-coloured swatch: the only theme indicator that
                     makes sense across light AND dark themes. The old
                     hardcoded MoonNight icon was misleading on light
                     themes (and unreadable on a light header background)
                     because it stayed the same regardless of which
                     theme was actually active. The swatch mirrors the
                     theme's accent colour, so the user can tell at a
                     glance which theme they're on. -->
                <span class="theme-swatch" :data-theme="currentTheme" aria-hidden="true"></span>
                <span class="theme-name">{{ currentTheme }}</span>
                <el-icon :size="10" class="chevron"><ArrowDown /></el-icon>
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
            >v{{ version }}</span>
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
import { onMounted, computed, ref } from 'vue'
import { useFileStore } from './stores/fileStore'
import { useTheme } from './composables/useTheme'
import Breadcrumb from './components/Breadcrumb.vue'
import FileList from './components/FileList.vue'
import zhCn from 'element-plus/dist/locale/zh-cn.mjs'
import QRCodeModal from './components/QrCodeModal.vue'
import type { FileItem } from './types'
import {
  Camera, User, Search, ArrowDown
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
  /* min-width: 0 lets the flex item shrink below its content's
     intrinsic width, which the title needs to be able to ellipsise
     when the right-side controls (search, theme) push for space. */
  min-width: 0;
  flex-shrink: 1;
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
  overflow: hidden;
  text-overflow: ellipsis;
  /* Same rationale as .header-brand: without min-width: 0 the flex
     item refuses to shrink below its content width, so the right-side
     controls end up overflowing the viewport on narrow phones and
     covering the title. */
  min-width: 0;
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

/* Theme switcher: a small coloured dot that mirrors the current theme's
   accent. Fixed on screen and works on both light and dark headers,
   unlike the previous MoonNight icon which was misleading on light
   themes and visually identical across all four themes. */
.theme-swatch {
  display: inline-block;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 1.5px solid var(--el-border-color);
  box-sizing: border-box;
  flex-shrink: 0;
  vertical-align: middle;
  transition: background-color var(--transition-base),
    border-color var(--transition-base);
}

.theme-swatch[data-theme="white"] { background: #f5f5f5; }
.theme-swatch[data-theme="black"] { background: #1f1f1f; }
.theme-swatch[data-theme="green"] { background: #3d6b4a; }
.theme-swatch[data-theme="cyan"]  { background: #00b8d4; }

.theme-name {
  margin-left: 2px;
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
}

/* Pill card: the whole footer is one rounded container. Looks more
   deliberate than a flat row of text and survives theming cleanly. */
.footer-card {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 7px 14px;
  font-size: 12.5px;
  line-height: 1.2;
  background: color-mix(in srgb, var(--el-fill-color-blank) 60%, transparent);
  border: 1px solid var(--el-border-color-extra-light);
  border-radius: var(--radius-pill);
  color: var(--el-text-color-secondary);
  transition:
    border-color var(--transition-base),
    background-color var(--transition-base);
}

.footer-card:hover {
  border-color: var(--el-border-color-lighter);
  background: var(--el-fill-color-blank);
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
  color: #ef4444; /* stays red in all themes — universal "love" signal */
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

  /* Shrink the right-side control buttons to square icon-only chips
     so the brand area keeps its original logo size + title text.
     Element Plus' default button padding eats ~30px each side; that
     alone can push the title off-screen on a 360px phone. */
  .header-btn {
    padding-left: 8px;
    padding-right: 8px;
    min-height: 32px;
  }

  /* On phone there's no useful action for the "View in Phone" QR —
     you're already viewing it on the only device that could scan it.
     Hiding it frees ~40px of header width that the brand area needs. */
  .header-btn--qr {
    display: none;
  }

  .header-right {
    /* Tighter gap so the remaining right-side items sit closer
       together once the QR button is gone. */
    gap: 6px;
  }

  .header-search {
    /* On tablet/phone, drop the fixed width and grow into the space
       the rest of the header leaves over. min-width keeps the input
       usable (the prefix icon + clear button need ~80px on their
       own) and max-width stops it from swallowing everything when
       the title is short. */
    width: auto;
    flex: 1 1 0;
    min-width: 100px;
    max-width: 220px;
  }

  .header-btn-label,
  .header-user span,
  .theme-toggle .theme-name {
    /* Keep the swatch and chevron visible — they're how the user
       sees the current theme and recognises the dropdown trigger. */
    display: none;
  }

  .theme-toggle .chevron {
    /* Drop the chevron on tablet/phone. The swatch alone doesn't
       obviously telegraph "dropdown", but the button's tap target
       + the immediate menu on tap is the established mobile pattern
       for icon-only controls. Saves ~14px horizontally. */
    display: none;
  }

  /* Center the footer pill card on tablet/phone. */
  .app-footer {
    justify-content: center;
    padding: 20px 16px 12px;
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
    /* Below 380px the title's natural width plus the right-side
       controls exceed the viewport — even with ellipsis the title
       becomes a single character with "...". Drop the title text
       entirely on the smallest phones; the logo + URL bar still
       convey the brand. */
    max-width: 140px;
  }

  .header-brand {
    gap: 6px;
    padding: 4px 6px;
    margin: -4px -6px;
  }

  .header-search {
    /* Trim the search minimum on the smallest phones so the brand
       area can keep showing its logo + at least a few characters of
       title before hitting the ellipsis-or-hide boundary. */
    min-width: 90px;
    max-width: 160px;
  }

  .app-footer {
    padding: 16px 12px 12px;
    font-size: 11px;
  }

  /* On tiny phones the pill may exceed the viewport. Allow it to wrap
     to a second line so nothing overflows, and trim the inner padding. */
  .footer-card {
    flex-wrap: wrap;
    justify-content: center;
    row-gap: 6px;
    column-gap: 6px;
    padding: 6px 10px;
    font-size: 11.5px;
  }

  .footer-product {
    font-size: 12.5px;
  }
}

/* ── Tiny phones (iPhone SE, etc.) ──
   Below 360px the title is one character + "..." and the right-side
   controls still overlap it. Hide the title text outright — the
   logo alone is enough brand, and the breadcrumb below the header
   still tells the user where they are. */
@media (max-width: 360px) {
  .header-title {
    display: none;
  }
}
</style>
