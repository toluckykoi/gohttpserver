<template>
  <el-config-provider :locale="locale">
    <div class="app-container" :class="themeClass">
      <!-- Header -->
      <header class="app-header">
        <div class="header-left">
          <el-icon :size="24" class="header-icon">
            <Files />
          </el-icon>
          <h1 class="header-title">GoHTTP File Server</h1>
        </div>
        <div class="header-right">
          <el-button type="primary" link @click="handleShowMainQrCode">
            <el-icon><Camera /></el-icon>
            View in Phone
          </el-button>

          <template v-if="fileStore.user">
            <el-button v-if="fileStore.user.email" type="info" link>
              <el-icon><User /></el-icon>
              {{ fileStore.user.name }}
            </el-button>
            <el-button v-else type="info" link>
              <el-icon><User /></el-icon>
              Guest
            </el-button>
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
            <el-button type="default" text>
              <el-icon><MoonNight /></el-icon>
              {{ currentTheme }}
              <el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item
                  v-for="theme in availableThemes"
                  :key="theme"
                  :command="theme"
                  :class="{ active: theme === currentTheme }"
                >
                  {{ theme }}
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </header>

      <!-- Main Content -->
      <main class="app-main">
        <div class="main-inner">
          <Breadcrumb />
          <FileList />
        </div>
        <footer class="app-footer">
          <el-link href="https://github.com/codeskyblue/gohttpserver" target="_blank" type="info">
            gohttpserver ({{ version }})
          </el-link>
          <span>, by </span>
          <el-link href="https://github.com/codeskyblue" target="_blank" type="info">
            codeskyblue
          </el-link>
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
  Files, Camera, User, Search, MoonNight, ArrowDown
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
/* Minimal reset — don't touch Element Plus internals */
html, body {
  margin: 0;
  padding: 0;
  height: 100%;
  width: 100%;
}

#app {
  height: 100%;
  width: 100%;
}

/* ── Layout ── */
.app-container {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  background-color: var(--el-bg-color-page);
}

/* ── Header ── */
.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 64px;
  padding: 0 20px;
  flex-shrink: 0;
  background: var(--el-bg-color);
  border-bottom: 1px solid var(--el-border-color);
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-icon {
  color: var(--el-color-primary);
}

.header-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  white-space: nowrap;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.header-search {
  width: 240px;
}

/* ── Main ── */
.app-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  padding: 20px;
  /* ensure main fills width */
  width: 100%;
  box-sizing: border-box;
}

.main-inner {
  flex: 1;
  width: 100%;
  max-width: 1400px;
  margin: 0 auto;
}

/* ── Footer ── */
.app-footer {
  max-width: 1400px;
  width: 100%;
  margin: 0 auto;
  text-align: right;
  padding: 20px 0 0;
  color: var(--el-text-color-secondary);
}

/* ── Theme ── */
.theme-black  { --theme-color: #1f2937; }
.theme-green  { --theme-color: #10b981; }
.theme-cyan   { --theme-color: #06b6d4; }

/* ── Dropdown active item ── */
.dropdown-active {
  background-color: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}
</style>
