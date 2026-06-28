<template>
  <div class="file-list-container">
    <div class="toolbar">
      <div class="toolbar-group toolbar-group--nav">
        <el-tooltip content="Back" placement="bottom">
          <el-button class="toolbar-btn" type="default" @click="goBack">
            <el-icon><ArrowLeft /></el-icon>
            <span class="toolbar-label">Back</span>
          </el-button>
        </el-tooltip>
        <el-tooltip content="Refresh (F5)" placement="bottom">
          <el-button
            class="toolbar-btn toolbar-refresh"
            type="default"
            :class="{ 'toolbar-refresh--spinning': refreshing }"
            @click="handleRefresh"
          >
            <el-icon><Refresh /></el-icon>
            <span class="toolbar-label">Refresh</span>
          </el-button>
        </el-tooltip>
        <el-tooltip
          :content="showHidden ? 'Hide hidden files' : 'Show hidden files'"
          placement="bottom"
        >
          <el-button class="toolbar-btn" type="default" @click="toggleShowHidden">
            <el-icon><View /></el-icon>
            <span class="toolbar-label">{{ showHidden ? 'Hide' : 'Show' }} Hidden</span>
          </el-button>
        </el-tooltip>
        <!-- Mobile only: enter / exit selection mode for card-based multi-select.
             Hidden on desktop where the table has built-in checkbox columns. -->
        <el-tooltip
          v-if="!loading && sortedFiles.length > 0"
          :content="fileStore.selectionMode ? 'Exit selection mode' : 'Select files'"
          placement="bottom"
        >
          <el-button
            class="toolbar-btn toolbar-select-toggle"
            :type="fileStore.selectionMode ? 'primary' : 'default'"
            @click="handleToggleSelectionMode"
          >
            <el-icon><Check /></el-icon>
            <span class="toolbar-label">{{ fileStore.selectionMode ? 'Done' : 'Select' }}</span>
          </el-button>
        </el-tooltip>
      </div>

      <div v-if="auth.upload || auth.delete" class="toolbar-group toolbar-group--actions">
        <el-tooltip v-if="auth.upload" content="Upload files" placement="bottom">
          <el-button
            class="toolbar-btn"
            type="primary"
            @click="showUploadModal = true"
          >
            <el-icon><Upload /></el-icon>
            <span class="toolbar-label">Upload</span>
          </el-button>
        </el-tooltip>
        <el-tooltip v-if="auth.upload" content="Download a remote file via URL" placement="bottom">
          <el-button
            class="toolbar-btn"
            @click="showUrlDownloadModal = true"
          >
            <el-icon><Link /></el-icon>
            <span class="toolbar-label">From URL</span>
          </el-button>
        </el-tooltip>
        <el-tooltip v-if="auth.delete" content="New folder" placement="bottom">
          <el-button
            class="toolbar-btn"
            type="success"
            @click="handleCreateDirectory"
          >
            <el-icon><FolderAdd /></el-icon>
            <span class="toolbar-label">New Folder</span>
          </el-button>
        </el-tooltip>
      </div>
    </div>

    <!-- Empty state -->
    <div v-if="!loading && sortedFiles.length === 0" class="empty-state">
      <span class="empty-icon" aria-hidden="true">
        <svg viewBox="0 0 64 64" width="40" height="40" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <path d="M8 18v28a4 4 0 0 0 4 4h40a4 4 0 0 0 4-4V24a4 4 0 0 0-4-4H32l-4-4H12a4 4 0 0 0-4 4z"/>
          <path d="M8 26h48" stroke-dasharray="3 4" opacity="0.5"/>
        </svg>
      </span>
      <p class="empty-title">This folder is empty</p>
      <p class="empty-hint" v-if="auth.upload">
        Drop files here or click <strong>Upload</strong> to get started
      </p>
      <p class="empty-hint" v-else>
        Files uploaded to this directory will appear here
      </p>
    </div>

    <template v-else>
      <!-- Multi-select bar: visible when at least one row is selected.
           Sits between the toolbar and the file list so it's always in
           reach and clearly contextual to the table/cards below. -->
      <div v-if="selectedCount > 0" class="selection-bar" role="status">
        <div class="selection-bar-left">
          <span class="selection-bar-count">
            {{ selectedCount }} selected
          </span>
        </div>
        <div class="selection-bar-right">
          <el-tooltip content="Download selected" placement="bottom">
            <el-button
              class="selection-bar-btn selection-bar-download"
              text
              :icon="Download"
              aria-label="Download selected"
              @click="handleDownloadSelected"
            />
          </el-tooltip>
          <el-button
            v-if="!isAllSelected && sortedFiles.length > selectedCount"
            class="selection-bar-btn"
            text
            type="primary"
            @click="handleSelectAll"
          >
            Select All
          </el-button>
          <el-button
            v-else
            class="selection-bar-btn"
            text
            type="primary"
            @click="handleDeselectAll"
          >
            Deselect All
          </el-button>
          <el-tooltip content="Clear selection" placement="bottom">
            <el-button
              class="selection-bar-btn selection-bar-clear"
              text
              :icon="Close"
              circle
              aria-label="Clear selection"
              @click="handleDeselectAll"
            />
          </el-tooltip>
        </div>
      </div>

    <!-- Desktop / tablet: table layout -->
    <el-table
      class="file-table"
      :data="sortedFiles"
      v-loading="loading"
      style="width: 100%"
      :default-sort="{ prop: 'mtime', order: 'descending' }"
      :row-class-name="rowClassName"
      :tooltip-effect="'dark'"
      row-key="path"
      @sort-change="handleSortChange"
      @selection-change="handleSelectionChange"
      @row-click="handleRowClick"
      ref="elTableRef"
    >
      <el-table-column type="selection" width="40" />
      <el-table-column label="Name" min-width="300" prop="name" sortable="custom">
        <template #default="{ row }">
          <div
            class="file-name-cell-wrap"
            :title="clickActionLabel(getClickAction(row.name, row.type === 'dir'))"
          >
            <FileItem :file="row" />
          </div>
        </template>
      </el-table-column>

      <el-table-column label="Size" width="120" align="right" prop="size" sortable="custom">
        <template #default="{ row }">
          <!-- Directory size is already computed server-side via
               historyDirSize; just format it like a file. Empty dirs
               come through as 0 → "0 B", which is honest. -->
          <span class="data-mono">{{ formatBytes(row.size) }}</span>
        </template>
      </el-table-column>

      <el-table-column label="Modified" width="200" prop="mtime" sortable="custom" class-name="col-modified">
        <template #default="{ row }">
          <span class="data-mono mtime-col" @click.stop="toggleMtimeType">
            {{ formatMtime(row.mtime) }}
          </span>
        </template>
      </el-table-column>

      <el-table-column label="Actions" width="340" class-name="col-actions">
        <template #default="{ row }">
          <!-- Desktop actions -->
          <div class="action-buttons action-desktop" @click.stop>
            <!-- Directory actions -->
            <template v-if="row.type === 'dir'">
              <el-tooltip content="Download as ZIP" placement="top">
                <el-button
                  type="primary"
                  :icon="Download"
                  circle
                  size="small"
                  @click="handleDownloadArchive(row)"
                />
              </el-tooltip>
              <el-tooltip content="Info" placement="top">
                <el-button
                  type="info"
                  :icon="InfoFilled"
                  circle
                  size="small"
                  @click="handleShowInfo(row)"
                />
              </el-tooltip>
              <el-tooltip content="QR Code" placement="top">
                <el-button
                  type="warning"
                  :icon="Camera"
                  circle
                  size="small"
                  @click="handleShowQrCode(row)"
                />
              </el-tooltip>
              <el-tooltip v-if="auth.delete" content="Delete (hold Alt to skip confirm)" placement="top">
                <el-button
                  type="danger"
                  :icon="Delete"
                  circle
                  size="small"
                  @click="handleDeleteFile(row)"
                />
              </el-tooltip>
            </template>

            <!-- File actions -->
            <template v-else>
              <el-tooltip content="Download" placement="top">
                <el-button
                  type="primary"
                  :icon="Download"
                  circle
                  size="small"
                  @click="handleDownload(row)"
                />
              </el-tooltip>
              <el-tooltip content="Copy Link" placement="top">
                <el-button
                  type="success"
                  :icon="DocumentCopy"
                  circle
                  size="small"
                  @click="handleCopyLink(row)"
                />
              </el-tooltip>
              <el-tooltip content="QR Code" placement="top">
                <el-button
                  type="warning"
                  :icon="Camera"
                  circle
                  size="small"
                  @click="handleShowQrCode(row)"
                />
              </el-tooltip>
              <el-tooltip v-if="canPreview(row)" content="Preview" placement="top">
                <el-button
                  type="primary"
                  :icon="Reading"
                  circle
                  size="small"
                  @click="handleShowPreview(row)"
                />
              </el-tooltip>
              <el-tooltip content="Info" placement="top">
                <el-button
                  type="info"
                  :icon="InfoFilled"
                  circle
                  size="small"
                  @click="handleShowInfo(row)"
                />
              </el-tooltip>
              <el-tooltip
                v-if="isVideo(row)"
                content="Play Video"
                placement="top"
              >
                <el-button
                  type="primary"
                  :icon="VideoPlay"
                  circle
                  size="small"
                  @click="handleVideoPlay(row)"
                />
              </el-tooltip>
              <el-tooltip
                v-if="hasQrCode(row)"
                content="Install"
                placement="top"
              >
                <el-button
                  type="success"
                  :icon="Box"
                  circle
                  size="small"
                  @click="handleInstall(row)"
                />
              </el-tooltip>
              <el-tooltip v-if="auth.delete" content="Delete (hold Alt to skip confirm)" placement="top">
                <el-button
                  type="danger"
                  :icon="Delete"
                  circle
                  size="small"
                  @click="handleDeleteFile(row)"
                />
              </el-tooltip>
            </template>
          </div>
          <!-- Mobile: dropdown menu -->
          <div class="action-mobile" @click.stop>
            <el-dropdown trigger="click" @command="(cmd: string) => handleMobileAction(cmd, row)">
              <el-button :icon="MoreFilled" circle size="small" />
              <template #dropdown>
                <el-dropdown-menu>
                  <template v-if="row.type === 'dir'">
                    <el-dropdown-item command="zip">Download as ZIP</el-dropdown-item>
                    <el-dropdown-item command="info">Info</el-dropdown-item>
                    <el-dropdown-item command="qrcode">QR Code</el-dropdown-item>
                    <el-dropdown-item v-if="auth.delete" command="delete" divided>
                      Delete
                    </el-dropdown-item>
                  </template>
                  <template v-else>
                    <el-dropdown-item command="download">Download</el-dropdown-item>
                    <el-dropdown-item command="copy">Copy Link</el-dropdown-item>
                    <el-dropdown-item command="qrcode">QR Code</el-dropdown-item>
                    <el-dropdown-item v-if="canPreview(row)" command="preview">Preview</el-dropdown-item>
                    <el-dropdown-item v-if="isVideo(row)" command="video">Play Video</el-dropdown-item>
                    <el-dropdown-item v-if="hasQrCode(row)" command="install">Install</el-dropdown-item>
                    <el-dropdown-item command="info">Info</el-dropdown-item>
                    <el-dropdown-item v-if="auth.delete" command="delete" divided>
                      Delete
                    </el-dropdown-item>
                  </template>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <!-- Mobile: card list layout (no horizontal scroll) -->
    <div class="mobile-file-list" :class="{ 'is-loading': loading }">
      <div
        v-for="row in sortedFiles"
        :key="row.name"
        class="mobile-file-card"
        :class="{
          'mobile-file-card--dir': row.type === 'dir',
          'mobile-file-card--selected': fileStore.selectedFiles.has(row.path)
        }"
        :title="fileStore.selectionMode
          ? (fileStore.selectedFiles.has(row.path) ? 'Tap to deselect' : 'Tap to select')
          : clickActionLabel(getClickAction(row.name, row.type === 'dir'))"
        @click="handleRowClick(row, $event)"
      >
        <!-- Checkbox: always shown in selection mode, and on selected
             cards even after exiting mode (so the user can see what's
             still in their selection). -->
        <el-checkbox
          v-if="fileStore.selectionMode || fileStore.selectedFiles.has(row.path)"
          class="mobile-file-checkbox"
          :model-value="fileStore.selectedFiles.has(row.path)"
          @change="fileStore.toggleSelect(row.path)"
          @click.stop
        />
        <div class="mobile-file-icon">
          <el-icon :size="22">
            <component :is="getFileIcon(row.name, row.type)" />
          </el-icon>
        </div>
        <div class="mobile-file-info">
          <div class="mobile-file-name">{{ row.name }}</div>
          <div class="mobile-file-meta">
            <template v-if="row.type === 'dir'">
              <span>Folder</span>
              <span class="mobile-file-sep">·</span>
            </template>
            <span class="data-mono">{{ formatBytes(row.size) }}</span>
            <span class="mobile-file-sep">·</span>
            <span>{{ formatMtime(row.mtime) }}</span>
          </div>
        </div>
        <!-- Action dropdown: hidden in selection mode to avoid conflicting
             controls (tapping the card already toggles selection). -->
        <div v-if="!fileStore.selectionMode" class="mobile-file-actions" @click.stop>
          <el-dropdown
            trigger="click"
            @command="(cmd: string) => handleMobileAction(cmd, row)"
          >
            <el-button :icon="MoreFilled" circle size="small" />
            <template #dropdown>
              <el-dropdown-menu>
                <template v-if="row.type === 'dir'">
                  <el-dropdown-item command="zip">Download as ZIP</el-dropdown-item>
                  <el-dropdown-item command="info">Info</el-dropdown-item>
                  <el-dropdown-item command="qrcode">QR Code</el-dropdown-item>
                  <el-dropdown-item v-if="auth.delete" command="delete" divided>
                    Delete
                  </el-dropdown-item>
                </template>
                <template v-else>
                  <el-dropdown-item command="download">Download</el-dropdown-item>
                  <el-dropdown-item command="copy">Copy Link</el-dropdown-item>
                  <el-dropdown-item command="qrcode">QR Code</el-dropdown-item>
                  <el-dropdown-item v-if="canPreview(row)" command="preview">Preview</el-dropdown-item>
                  <el-dropdown-item v-if="isVideo(row)" command="video">Play Video</el-dropdown-item>
                  <el-dropdown-item v-if="hasQrCode(row)" command="install">Install</el-dropdown-item>
                  <el-dropdown-item command="info">Info</el-dropdown-item>
                  <el-dropdown-item v-if="auth.delete" command="delete" divided>
                    Delete
                  </el-dropdown-item>
                </template>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </div>
    </div>
    </template>

    <NewFolderDialog
      v-model:visible="showNewFolderDialog"
    />

    <UploadModal
      v-model:visible="showUploadModal"
    />

    <UrlDownloadModal
      v-model:visible="showUrlDownloadModal"
      :current-path="currentPath"
      @fetched="handleUrlDownloaded"
    />

    <QrCodeModal
      v-model:visible="showQrCodeModal"
      :file="currentQrFile"
      :current-path="currentPath"
    />

    <FileInfoModal
      v-model:visible="showFileInfoModal"
      :file="currentInfoFile"
      :current-path="currentPath"
    />

    <VideoPlayer
      v-model:visible="showVideoPlayerModal"
      :file="currentVideoFile"
      :current-path="currentPath"
    />

    <TextPreviewModal
      v-model:visible="showTextPreviewModal"
      :file="currentPreviewFile"
      :current-path="currentPath"
      :can-edit="auth.edit"
    />

    <ImagePreviewModal
      v-model:visible="showImagePreviewModal"
      :file="currentImageFile"
      :current-path="currentPath"
      :siblings="imageSiblings"
      @navigate="handleNavigateImage"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, defineAsyncComponent } from 'vue'
import { useFileStore } from '@/stores/fileStore'
import { useFileApi } from '@/composables/useFileApi'
import type { FileItem as FileItemType } from '@/types'
import { formatBytes } from '@/utils/formatBytes'
import { getEncodePath, parentDirectory } from '@/utils/path'
import { shouldHaveQrcode, isVideoFile, isImageFile, getFileIcon } from '@/utils/fileIcon'
import { isPreviewable, getClickAction, clickActionLabel } from '@/utils/previewable'
import { copyText } from '@/utils/clipboard'
import FileItem from './FileItem.vue'

// Modals are lazily loaded: each one pulls in its own chunk of
// Element Plus components, the qrcode lib, marked, etc. Users that
// never open "Preview" or "QR Code" never pay for those bundles.
// Suspense isn't needed because each modal has its own v-if guard.
const UploadModal = defineAsyncComponent(() => import('./UploadModal.vue'))
const UrlDownloadModal = defineAsyncComponent(() => import('./UrlDownloadModal.vue'))
const QrCodeModal = defineAsyncComponent(() => import('./QrCodeModal.vue'))
const FileInfoModal = defineAsyncComponent(() => import('./FileInfoModal.vue'))
const VideoPlayer = defineAsyncComponent(() => import('./VideoPlayer.vue'))
const NewFolderDialog = defineAsyncComponent(() => import('./NewFolderDialog.vue'))
const TextPreviewModal = defineAsyncComponent(() => import('./TextPreviewModal.vue'))
const ImagePreviewModal = defineAsyncComponent(() => import('./ImagePreviewModal.vue'))
import {
  ArrowLeft,
  View,
  Upload,
  Link,
  FolderAdd,
  Download,
  InfoFilled,
  Delete,
  DocumentCopy,
  Camera,
  VideoPlay,
  Box,
  MoreFilled,
  Reading,
  Refresh,
  Check,
  Close
} from '@element-plus/icons-vue'

import { ElMessage, ElMessageBox } from 'element-plus'
import type { TableInstance } from 'element-plus'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'

dayjs.extend(relativeTime)

const fileStore = useFileStore()
const fileApi = useFileApi()

// State
const showNewFolderDialog = ref(false)
const showUploadModal = ref(false)
const showUrlDownloadModal = ref(false)
const showQrCodeModal = ref(false)
const showFileInfoModal = ref(false)
const showVideoPlayerModal = ref(false)
const showTextPreviewModal = ref(false)
const showImagePreviewModal = ref(false)
const currentQrFile = ref<FileItemType | null>(null)
const currentInfoFile = ref<FileItemType | null>(null)
const currentVideoFile = ref<FileItemType | null>(null)
const currentPreviewFile = ref<FileItemType | null>(null)
const currentImageFile = ref<FileItemType | null>(null)
const mtimeTypeFromNow = ref(true)
const refreshing = ref(false)
// el-table instance ref. Used to drive the table's built-in selection from
// the selection bar's "Select All" / "Clear" buttons. Each toggleRowSelection
// call updates the table's internal state and fires selection-change, which
// then syncs back into the store — single round trip.
const elTableRef = ref<TableInstance>()

// Computed
const currentPath = computed(() => fileStore.currentPath)
const sortedFiles = computed(() => fileStore.sortedFiles)
const auth = computed(() => fileStore.auth)
const loading = computed(() => fileStore.loading)
const showHidden = computed(() => fileStore.showHidden)
const selectedCount = computed(() => fileStore.selectedCount)
const isAllSelected = computed(() => fileStore.isAllSelected)

/** Image siblings for the prev/next carousel inside ImagePreviewModal. */
const imageSiblings = computed(() =>
  sortedFiles.value.filter((f) => f.type !== 'dir' && isImageFile(f.name))
)

function goBack() {
  const parentPath = parentDirectory(currentPath.value)
  fileStore.loadFiles(parentPath || '/')
}

async function handleRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  // The store's loading flag is already wired to el-table's v-loading and
  // the empty-state condition, so we don't need a separate table spinner.
  // Keep the icon spinning for at least 400ms so users get clear visual
  // feedback even on a fast local server.
  const startedAt = Date.now()
  try {
    await fileStore.loadFiles()
  } finally {
    const elapsed = Date.now() - startedAt
    const minSpin = 400
    if (elapsed < minSpin) {
      setTimeout(() => {
        refreshing.value = false
      }, minSpin - elapsed)
    } else {
      refreshing.value = false
    }
  }
}

function onKeyDown(e: KeyboardEvent) {
  // F5 — refresh current directory. We intercept the default browser
  // refresh so a file manager F5 doesn't reload the whole page.
  if (e.key === 'F5' && !e.ctrlKey && !e.metaKey && !e.shiftKey && !e.altKey) {
    e.preventDefault()
    handleRefresh()
  }
}

// Global Alt-key state. Tracked at the window level so the same signal
// is available whether the delete was triggered by the desktop action
// button (which sees the click event) or the mobile dropdown (which
// synthesises its own click event with no modifier info).
const isAltPressed = ref(false)

function onAltChange(e: KeyboardEvent) {
  isAltPressed.value = e.altKey
}

// Belt-and-braces: if the window loses focus while Alt is held (alt-tab
// away, dialog opens, etc.), drop the flag so a subsequent click doesn't
// surprise the user by skipping confirmation.
function onWindowBlur() {
  isAltPressed.value = false
}

onMounted(() => {
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keydown', onAltChange)
  window.addEventListener('keyup', onAltChange)
  window.addEventListener('blur', onWindowBlur)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keydown', onAltChange)
  window.removeEventListener('keyup', onAltChange)
  window.removeEventListener('blur', onWindowBlur)
})

function toggleShowHidden() {
  fileStore.toggleShowHidden()
}

function handleSortChange({ prop, order }: { prop: string; order: string | null }) {
  fileStore.setSort(prop || 'mtime', order as 'ascending' | 'descending' | null)
}

function toggleMtimeType() {
  mtimeTypeFromNow.value = !mtimeTypeFromNow.value
}

function formatMtime(timestamp: number): string {
  if (mtimeTypeFromNow.value) {
    return dayjs(timestamp).fromNow()
  }
  return dayjs(timestamp).format('YYYY-MM-DD HH:mm:ss')
}

function hasQrCode(file: FileItemType): boolean {
  return shouldHaveQrcode(file.name)
}

function isVideo(file: FileItemType): boolean {
  return isVideoFile(file.name)
}

function canPreview(file: FileItemType): boolean {
  return file.type !== 'dir' && isPreviewable(file.name)
}

function handleRowClick(row: FileItemType, event?: MouseEvent) {
  // Mobile: when in selection mode, tapping a card toggles selection
  // instead of navigating/previewing. Desktop uses the table's built-in
  // checkbox column and never reaches this branch from a checkbox click.
  if (fileStore.selectionMode) {
    fileStore.toggleSelect(row.path)
    return
  }

  // Ctrl / Cmd + click always forces a download — works for every file type,
  // including directories (which download as ZIP).
  if (event && (event.ctrlKey || event.metaKey)) {
    if (row.type === 'dir') {
      handleDownloadArchive(row)
    } else {
      handleDownload(row)
    }
    return
  }

  const action = getClickAction(row.name, row.type === 'dir')

  switch (action.kind) {
    case 'navigate': {
      const newPath = getEncodePath(row.name, currentPath.value)
      fileStore.loadFiles(newPath)
      return
    }
    case 'preview-text': {
      currentPreviewFile.value = row
      showTextPreviewModal.value = true
      return
    }
    case 'preview-image': {
      currentImageFile.value = row
      showImagePreviewModal.value = true
      return
    }
    case 'play-video': {
      handleVideoPlay(row)
      return
    }
    case 'download': {
      handleDownload(row)
      return
    }
  }
}

function handleNavigateImage(target: FileItemType) {
  currentImageFile.value = target
}

/**
 * Element Plus row-class-name hook. Used to apply a CSS class per row
 * so we can give the cursor/title a per-file hint about what click does.
 */
function rowClassName({ row }: { row: FileItemType }): string {
  const action = getClickAction(row.name, row.type === 'dir')
  return `row-action-${action.kind}`
}

function handleDownload(file: FileItemType) {
  fileApi.downloadFile(currentPath.value, file.name)
}

function handleDownloadArchive(file: FileItemType) {
  fileApi.downloadArchive(currentPath.value, file.name)
}

/**
 * Bulk download from the selection bar. Picks the cheapest path that
 * still gives the user a single artefact:
 *   - 1 file: stream directly via downloadFile (no zip overhead)
 *   - 1 dir:  reuse the existing archive endpoint
 *   - N > 1:  POST /-/zip with every selected path
 * Selection is intentionally not cleared — the user might want to keep
 * the same set around while a large archive downloads in the background.
 */
async function handleDownloadSelected() {
  if (selectedCount.value === 0) return
  const selected = fileStore.files.filter((f) =>
    fileStore.selectedFiles.has(f.path)
  )
  if (selected.length === 0) return

  try {
    if (selected.length === 1) {
      const only = selected[0]
      if (only.type === 'dir') {
        handleDownloadArchive(only)
      } else {
        handleDownload(only)
      }
      return
    }
    await fileApi.downloadMulti(selected.map((f) => f.path))
  } catch (error) {
    ElMessage.error(`Failed to download: ${error}`)
  }
}

function handleShowInfo(file: FileItemType) {
  currentInfoFile.value = file
  showFileInfoModal.value = true
}

function handleShowQrCode(file: FileItemType) {
  currentQrFile.value = file
  showQrCodeModal.value = true
}

function handleShowPreview(file: FileItemType) {
  currentPreviewFile.value = file
  showTextPreviewModal.value = true
}

function handleVideoPlay(file: FileItemType) {
  currentVideoFile.value = file
  showVideoPlayerModal.value = true
}

function handleInstall(file: FileItemType) {
  const url = fileApi.getIpaInstallUrl(currentPath.value, file.name)
  window.location.href = url
}

function handleMobileAction(cmd: string, file: FileItemType) {
  switch (cmd) {
    case 'download': handleDownload(file); break
    case 'zip': handleDownloadArchive(file); break
    case 'copy': handleCopyLink(file); break
    case 'info': handleShowInfo(file); break
    case 'qrcode': handleShowQrCode(file); break
    case 'video': handleVideoPlay(file); break
    case 'install': handleInstall(file); break
    case 'preview': handleShowPreview(file); break
    case 'delete': handleDeleteFile(file); break
  }
}

async function handleCopyLink(file: FileItemType) {
  const encodePath = getEncodePath(file.name, currentPath.value)
  const url = window.location.origin + encodePath
  const ok = await copyText(url)
  if (ok) {
    ElMessage.success('Link copied to clipboard')
  } else {
    ElMessage.error('Failed to copy link — please copy manually')
  }
}

async function handleDeleteFile(file: FileItemType) {
  // Alt held at click time → skip the confirmation dialog. Documented in
  // README_zh.md as the "power user" shortcut for batch cleanups.
  if (isAltPressed.value) {
    await fileStore.deleteFile(file.name)
    return
  }
  try {
    await ElMessageBox.confirm(
      `Delete ${file.name}?`,
      'Confirm',
      {
        confirmButtonText: 'Delete',
        cancelButtonText: 'Cancel',
        type: 'warning'
      }
    )
    await fileStore.deleteFile(file.name)
  } catch {
    // User cancelled
  }
}

function handleCreateDirectory() {
  showNewFolderDialog.value = true
}

// After a successful URL fetch, the listing has changed (a new file
// landed on disk). Force a silent refresh so the new row appears
// without re-triggering the loading spinner. The file API endpoint
// has already invalidated the server-side size cache.
function handleUrlDownloaded() {
  fileStore.loadFiles(undefined, undefined, { silent: true }).catch(err =>
    console.error('Refresh after URL fetch failed:', err)
  )
}

// ── Multi-select handlers ──

/**
 * Element Plus selection-change event. Fires for every individual toggle
 * and the final state is the full set of currently-selected rows. We mirror
 * the full set into the store so the rest of the UI (mobile cards, selection
 * bar) sees a single, consistent picture.
 */
function handleSelectionChange(selection: FileItemType[]) {
  fileStore.setSelection(selection.map(r => r.path))
}

function handleSelectAll() {
  const table = elTableRef.value
  if (table) {
    // Drive the table's built-in "select all" toggle, which is exactly
    // what clicking the header checkbox would do. clearSelection() first
    // puts the table in a known "all deselected" state so the subsequent
    // toggleAllSelection() always lands on "all selected" regardless of
    // any prior partial selection. Each fired selection-change routes
    // back to handleSelectionChange and ends with the store fully synced.
    table.clearSelection()
    table.toggleAllSelection()
  } else {
    // Fallback when the table is unmounted (mobile path): update the
    // store directly. Mobile cards read from the store, so they stay in
    // sync without needing the table.
    fileStore.selectAll(sortedFiles.value.map(f => f.path))
  }
}

function handleDeselectAll() {
  const table = elTableRef.value
  if (table) {
    table.clearSelection()
  } else {
    fileStore.clearSelection()
  }
}

/**
 * Mobile-only: toggle the cards' selection-mode. Entering turns on every
 * card's checkbox; exiting also clears any pending selection so the user
 * starts clean next time.
 */
function handleToggleSelectionMode() {
  fileStore.selectionMode = !fileStore.selectionMode
  if (!fileStore.selectionMode) {
    fileStore.clearSelection()
  }
}
</script>

<style scoped>
.file-list-container {
  padding: 2px 0 24px;
}

/* ── Toolbar ──
   Modern segmented bar: nav/view group sits in a pill, action group
   gets its own row with the primary CTA popping in accent color. */
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 16px;
  padding: 8px;
  background: color-mix(in srgb, var(--el-bg-color) 75%, transparent);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-xs);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  flex-wrap: wrap;
}

.toolbar-group {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  background: color-mix(in srgb, var(--el-fill-color) 50%, transparent);
  border-radius: var(--radius-md);
}

/* Action group: no chrome, primary CTA pops with accent. */
.toolbar-group--actions {
  padding: 0;
  background: transparent;
  gap: 6px;
  border-left: none;
  margin-left: 0;
}

.toolbar :deep(.el-button) {
  font-size: 13px;
  font-weight: 500;
  transition: all var(--transition-base);
}

.toolbar :deep(.el-button:active) {
  scale: 0.97;
}

/* Segmented buttons: subtle hover/active inside the nav pill */
.toolbar-group :deep(.el-button:not(.is-circle)) {
  padding: 6px 12px;
}

.toolbar-group :deep(.el-button:hover) {
  background: var(--el-bg-color);
}

.toolbar-group--actions :deep(.el-button--success:hover) {
  background: color-mix(in srgb, var(--el-color-success) 88%, white);
  color: #fff;
  transform: translateY(-1px);
  box-shadow: 0 2px 6px color-mix(in srgb, var(--el-color-success) 30%, transparent),
              0 6px 20px color-mix(in srgb, var(--el-color-success) 25%, transparent);
}

/* Primary CTA — Upload. Pops against the rest. */
.toolbar-group--actions :deep(.el-button--primary) {
  background: var(--el-color-primary);
  color: #fff;
  font-weight: 600;
  padding: 8px 14px;
  box-shadow: 0 1px 2px color-mix(in srgb, var(--el-color-primary) 30%, transparent),
              0 2px 8px color-mix(in srgb, var(--el-color-primary) 15%, transparent);
  transition: background var(--transition-base),
              transform var(--transition-fast),
              box-shadow var(--transition-base);
}

.toolbar-group--actions :deep(.el-button--primary:hover) {
  background: color-mix(in srgb, var(--el-color-primary) 88%, white);
  color: #fff;
  transform: translateY(-1px);
  box-shadow: 0 2px 6px color-mix(in srgb, var(--el-color-primary) 30%, transparent),
              0 6px 20px color-mix(in srgb, var(--el-color-primary) 25%, transparent);
}

.toolbar-group--actions :deep(.el-button--primary:active) {
  transform: translateY(0) scale(0.97);
}

/* Refresh button: icon spins while refreshing so the click feels
   acknowledged even on a fast server. */
.toolbar-refresh .el-icon {
  transition: transform 0.2s ease-out;
}

.toolbar-refresh--spinning .el-icon {
  animation: toolbar-refresh-spin 0.9s linear infinite;
}

.toolbar-refresh--spinning {
  pointer-events: none;
  opacity: 0.85;
}

@keyframes toolbar-refresh-spin {
  from { transform: rotate(0deg); }
  to   { transform: rotate(360deg); }
}

/* ── Selection bar (sits between toolbar and file list) ──
   Slim contextual bar that only appears when at least one item is
   selected. Uses a subtle accent fill so it reads as a contextual
   control without competing with the toolbar. */
.selection-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
  padding: 10px 14px;
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--el-color-primary) 10%, var(--el-bg-color));
  border: 1px solid color-mix(in srgb, var(--el-color-primary) 22%, transparent);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--el-color-primary) 6%, transparent);
  animation: fade-up 240ms var(--ease-out) both;
  transition:
    background-color var(--transition-base),
    border-color var(--transition-base);
}

.selection-bar-count {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.selection-bar-count::before {
  content: "";
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--el-color-primary);
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--el-color-primary) 18%, transparent);
}

.selection-bar-left {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.selection-bar-right {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.selection-bar-btn {
  font-size: 13px;
  font-weight: 500;
}

.selection-bar-btn :deep(.el-icon) {
  font-size: 16px;
}

.selection-bar-clear {
  margin-left: 2px;
}

/* Mobile-only: the "Select" / "Done" toggle button in the toolbar.
   Hidden on desktop where the table has built-in selection columns. */
.toolbar-select-toggle {
  display: none;
}

/* ── Responsive: Tablet / phone ── */
@media (max-width: 640px) {
  .toolbar {
    gap: 8px;
  }

  /* Hide the textual label on every toolbar button — they're
     self-explanatory via the tooltip wrappers added above. */
  .toolbar-label {
    display: none;
  }

  /* Convert each button into a uniform square icon button.
     40×40 meets both Apple HIG (44pt target — close) and
     Material Design (48dp — also close) for touch ergonomics. */
  .toolbar-btn {
    width: 40px !important;
    min-width: 40px;
    height: 40px;
    min-height: 40px;
    padding: 0 !important;
    margin: 0;
  }

  .toolbar-btn .el-icon {
    margin: 0;
    font-size: 18px;
  }

  /* The desktop divider doesn't make sense once buttons are
     uniform-sized icons. Use a small gap instead. */
  .toolbar-group--actions {
    padding-left: 0;
    margin-left: 0;
    border-left: none;
  }

  /* Wrap toolbar groups onto their own line if the row is too
     wide — but the action group should never be split. */
  .toolbar-group {
    flex-wrap: nowrap;
  }

  /* On mobile, reveal the "Select" / "Done" toggle so users can
     enter selection mode on cards. The table's built-in checkbox
     column is hidden on mobile (the card list takes over), so
     this is the only way to multi-select on phones. */
  .toolbar-select-toggle {
    display: inline-flex;
  }
}

/* On tiny phones we shrink the touch target a touch to fit more
   icons in a single row when possible. */
@media (max-width: 400px) {
  .toolbar {
    gap: 6px;
  }

  .toolbar-btn {
    width: 36px !important;
    min-width: 36px;
    height: 36px;
    min-height: 36px;
  }

  .toolbar-btn .el-icon {
    font-size: 16px;
  }
}

/* ── Table ──
   Glass card surface, hairline borders, generous row padding,
   tighter typography for a refined, modern feel. */
.file-list-container :deep(.el-table) {
  --el-table-border-color: transparent;
  --el-table-border: none;
  --el-table-row-hover-bg-color: color-mix(
    in srgb,
    var(--el-color-primary) 4%,
    transparent
  );
  /* Theme-aware selection row tint: stronger than Element Plus' default
     so the highlight is visible against the row hover state too. */
  --el-table-selected-row-bg-color: color-mix(
    in srgb,
    var(--el-color-primary) 12%,
    transparent
  );
  --el-table-header-bg-color: color-mix(in srgb, var(--el-fill-color) 50%, transparent);
  background: color-mix(in srgb, var(--el-bg-color) 80%, transparent);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-lg);
  overflow: hidden;
  box-shadow: var(--shadow-sm);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
}

.file-list-container :deep(.el-table__header-wrapper) {
  border-radius: var(--radius-lg) var(--radius-lg) 0 0;
}

.file-list-container :deep(.el-table__header th) {
  font-family: var(--font-sans);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--el-text-color-placeholder);
  background: transparent;
  border-bottom: 1px solid var(--el-border-color-lighter);
  padding: 12px 0;
}

.file-list-container :deep(.el-table__header th .cell) {
  padding: 0 16px;
}

.file-list-container :deep(.el-table__body td) {
  padding: 10px 0;
  border-bottom: 1px solid color-mix(in srgb, var(--el-border-color-lighter) 60%, transparent);
  background: transparent;
  transition: background-color var(--transition-base);
}

.file-list-container :deep(.el-table__body td .cell) {
  padding: 0 16px;
}

/* Tighten the gap between the leading selection checkbox and the
   file name column. Keep 12px on the left of the checkbox so it
   doesn't hug the table border, then 2px on the right + 4px on the
   left of the name cell = 6px total — close, but still reads as a
   deliberate separation between the control and the file name. */
.file-list-container :deep(.el-table__body td.el-table-column--selection .cell),
.file-list-container :deep(.el-table__header th.el-table-column--selection .cell) {
  padding: 0 2px 0 12px;
}

/* Bump the desktop table checkbox a touch larger than Element Plus'
   default 14px — the rows are 32px+ tall and the default size reads
   as fiddly next to the larger file name text. */
.file-list-container :deep(.el-table .el-checkbox__inner) {
  width: 18px;
  height: 18px;
}

.file-list-container :deep(.el-table__body td:nth-child(2) .cell),
.file-list-container :deep(.el-table__header th:nth-child(2) .cell) {
  padding-left: 4px;
}

.file-list-container :deep(.el-table__body tr) {
  cursor: pointer;
  transition: background-color var(--transition-base);
}

.file-list-container :deep(.el-table__body tr:hover > td) {
  background: var(--el-fill-color-light);
}

.file-list-container :deep(.el-table__body tr:last-child td) {
  border-bottom: none;
}

/* Row click feedback */
.file-list-container :deep(.el-table__body tr:active) {
  background: var(--el-fill-color);
}

/* ── Data columns (mono) ── */
.data-mono {
  font-family: var(--font-mono);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
  color: var(--el-text-color-regular);
}

.data-muted {
  color: var(--el-text-color-placeholder);
}

.mtime-col {
  color: var(--el-text-color-regular);
  cursor: pointer;
  transition: color var(--transition-base);
}

.mtime-col:hover {
  color: var(--el-color-primary);
}

/* ── Action buttons ── */
.action-buttons {
  display: flex;
  gap: 4px;
  flex-wrap: nowrap;
}

/* ── Smart click row hints ── */
.file-name-cell-wrap {
  display: flex;
  align-items: center;
  width: 100%;
  min-width: 0;
}

/* Per-action cursor cue. Tables are already cursor: pointer, but for
   previewable files the cursor goes zoom-in to telegraph "this opens
   a preview, not a download". */
.file-list-container :deep(tr.row-action-preview-text) {
  cursor: zoom-in;
}
.file-list-container :deep(tr.row-action-preview-image) {
  cursor: zoom-in;
}
.file-list-container :deep(tr.row-action-play-video) {
  cursor: pointer;
}

.action-buttons :deep(.el-button) {
  transition: all var(--transition-base);
}

.action-buttons :deep(.el-button:hover) {
  transform: translateY(-1px);
}

.action-buttons :deep(.el-button:active) {
  transform: translateY(0) scale(0.95);
}

/* ── Empty state ──
   Big, friendly, illustrated card. SVG folder icon, soft gradient
   halo, gentle prompt — makes "this folder is empty" feel like a
   feature, not an error. */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 64px 24px 56px;
  text-align: center;
  border: 1px dashed var(--el-border-color);
  border-radius: var(--radius-xl);
  background:
    radial-gradient(600px 200px at 50% 0%,
      color-mix(in srgb, var(--el-color-primary) 4%, transparent) 0%,
      transparent 70%),
    color-mix(in srgb, var(--el-bg-color) 60%, transparent);
  animation: fade-up 480ms var(--ease-out) both;
}

.empty-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 80px;
  height: 80px;
  margin-bottom: 20px;
  border-radius: var(--radius-2xl);
  background: color-mix(in srgb, var(--el-color-primary) 10%, transparent);
  color: var(--el-color-primary);
  box-shadow: 0 8px 24px color-mix(in srgb, var(--el-color-primary) 12%, transparent);
}

.empty-title {
  margin: 0 0 8px;
  font-size: 18px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  letter-spacing: -0.015em;
}

.empty-hint {
  margin: 0;
  font-size: 13.5px;
  color: var(--el-text-color-secondary);
  max-width: 360px;
  line-height: 1.55;
}

.empty-hint strong {
  color: var(--el-color-primary);
  font-weight: 600;
}

@keyframes fade-up {
  from { opacity: 0; transform: translateY(8px); }
  to   { opacity: 1; transform: translateY(0); }
}

/* ── Loading ── */
.file-list-container :deep(.el-loading-mask) {
  border-radius: var(--radius-lg);
}

/* ── Mobile dropdown ── */
.action-mobile {
  display: none;
}

/* ── Mobile card list (hidden on desktop) ── */
.mobile-file-list {
  display: none;
}

/* ── Responsive: Phone / Small tablet ── */
@media (max-width: 640px) {
  .file-list-container {
    /* No more horizontal scroll — full-width card list */
    overflow-x: visible;
  }

  .toolbar .el-button {
    font-size: 12px;
    padding: 6px 10px;
  }

  /* Hide the table, show the card list */
  .file-list-container :deep(.file-table) {
    display: none !important;
  }

  .mobile-file-list {
    display: block;
  }

  /* Hide Modified column (still applies if the table ever leaks through) */
  .file-list-container :deep(.col-modified) {
    display: none !important;
  }

  .data-mono {
    font-size: 12px;
  }
}

/* ── Mobile card styles ──
   Layered, hoverable cards with a generous icon tile. */
.mobile-file-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
  background: color-mix(in srgb, var(--el-bg-color) 80%, transparent);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-lg);
  margin-bottom: 8px;
  cursor: pointer;
  box-shadow: var(--shadow-xs);
  transition:
    background-color var(--transition-base),
    border-color var(--transition-base),
    transform var(--transition-base),
    box-shadow var(--transition-base);
}

.mobile-file-card:last-child {
  margin-bottom: 0;
}

.mobile-file-card:hover {
  background: var(--el-bg-color);
  border-color: var(--el-border-color);
  box-shadow: var(--shadow-md);
}

.mobile-file-card:active {
  transform: scale(0.99);
}

/* Selected state: same primary tint as the desktop table selection
   row, so a user moving between desktop and mobile sees consistent
   visual feedback. The border becomes the primary color too so the
   selection reads at a glance even on small screens. */
.mobile-file-card--selected {
  background: color-mix(in srgb, var(--el-color-primary) 12%, var(--el-bg-color));
  border-color: color-mix(in srgb, var(--el-color-primary) 45%, transparent);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--el-color-primary) 20%, transparent),
              var(--shadow-sm);
}

.mobile-file-card--selected:hover {
  background: color-mix(in srgb, var(--el-color-primary) 16%, var(--el-bg-color));
  border-color: color-mix(in srgb, var(--el-color-primary) 60%, transparent);
}

/* Leading checkbox in a mobile card. Sized to a 36px target with
   some breathing room; @click.stop keeps the checkbox tap from
   triggering the card's row click. */
.mobile-file-checkbox {
  flex-shrink: 0;
  margin-right: 2px;
}

.mobile-file-checkbox :deep(.el-checkbox__inner) {
  width: 20px;
  height: 20px;
}

.mobile-file-icon {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--el-color-primary) 12%, transparent);
  color: var(--el-color-primary);
  transition: background var(--transition-base),
              transform var(--transition-base);
}

.mobile-file-card:hover .mobile-file-icon {
  background: color-mix(in srgb, var(--el-color-primary) 18%, transparent);
  transform: scale(1.04);
}

.mobile-file-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.mobile-file-name {
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  letter-spacing: -0.01em;
}

.mobile-file-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mobile-file-sep {
  color: var(--el-text-color-placeholder);
  opacity: 0.6;
}

.mobile-file-actions {
  flex-shrink: 0;
}

/* ── Responsive: Tiny phone ── */
@media (max-width: 400px) {
  .mobile-file-card {
    padding: 12px 14px;
    gap: 10px;
  }

  .mobile-file-icon {
    width: 38px;
    height: 38px;
  }

  .mobile-file-name {
    font-size: 13px;
  }

  .mobile-file-meta {
    font-size: 11px;
  }
}
</style>
