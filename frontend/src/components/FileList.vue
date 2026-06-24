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
      <el-icon :size="48" class="empty-icon">
        <FolderOpened />
      </el-icon>
      <p class="empty-title">This folder is empty</p>
      <p class="empty-hint" v-if="auth.upload">
        Drop files here or click <strong>Upload</strong> to get started
      </p>
    </div>

    <template v-else>
    <!-- Desktop / tablet: table layout -->
    <el-table
      class="file-table"
      :data="sortedFiles"
      v-loading="loading"
      style="width: 100%"
      :default-sort="{ prop: 'mtime', order: 'descending' }"
      :row-class-name="rowClassName"
      :tooltip-effect="'dark'"
      @sort-change="handleSortChange"
      @row-click="handleRowClick"
    >
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
          <span v-if="row.type === 'dir'" class="data-mono data-muted">-</span>
          <span v-else class="data-mono">{{ formatBytes(row.size) }}</span>
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
              <el-tooltip v-if="auth.delete" content="Delete" placement="top">
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
              <el-tooltip v-if="auth.delete" content="Delete" placement="top">
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
        :class="{ 'mobile-file-card--dir': row.type === 'dir' }"
        :title="clickActionLabel(getClickAction(row.name, row.type === 'dir'))"
        @click="handleRowClick(row, $event)"
      >
        <div class="mobile-file-icon">
          <el-icon :size="22">
            <component :is="getFileIcon(row.name, row.type)" />
          </el-icon>
        </div>
        <div class="mobile-file-info">
          <div class="mobile-file-name">{{ row.name }}</div>
          <div class="mobile-file-meta">
            <span>{{ row.type === 'dir' ? 'Folder' : formatBytes(row.size) }}</span>
            <span class="mobile-file-sep">·</span>
            <span>{{ formatMtime(row.mtime) }}</span>
          </div>
        </div>
        <div class="mobile-file-actions" @click.stop>
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

    <UploadModal
      v-model:visible="showUploadModal"
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
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useFileStore } from '@/stores/fileStore'
import { useFileApi } from '@/composables/useFileApi'
import type { FileItem as FileItemType } from '@/types'
import { formatBytes } from '@/utils/formatBytes'
import { getEncodePath, parentDirectory } from '@/utils/path'
import { shouldHaveQrcode, isVideoFile, isImageFile, getFileIcon } from '@/utils/fileIcon'
import { isPreviewable, getClickAction, clickActionLabel } from '@/utils/previewable'
import { copyText } from '@/utils/clipboard'
import FileItem from './FileItem.vue'
import UploadModal from './UploadModal.vue'
import QrCodeModal from './QrCodeModal.vue'
import FileInfoModal from './FileInfoModal.vue'
import VideoPlayer from './VideoPlayer.vue'
import TextPreviewModal from './TextPreviewModal.vue'
import ImagePreviewModal from './ImagePreviewModal.vue'
import {
  ArrowLeft,
  View,
  Upload,
  FolderAdd,
  FolderOpened,
  Download,
  InfoFilled,
  Delete,
  DocumentCopy,
  Camera,
  VideoPlay,
  Box,
  MoreFilled,
  Reading,
  Refresh
} from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'

dayjs.extend(relativeTime)

const fileStore = useFileStore()
const fileApi = useFileApi()

// State
const showUploadModal = ref(false)
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

// Computed
const currentPath = computed(() => fileStore.currentPath)
const sortedFiles = computed(() => fileStore.sortedFiles)
const auth = computed(() => fileStore.auth)
const loading = computed(() => fileStore.loading)
const showHidden = computed(() => fileStore.showHidden)

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

onMounted(() => {
  window.addEventListener('keydown', onKeyDown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
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

async function handleCreateDirectory() {
  try {
    const { value: name } = await ElMessageBox.prompt(
      'Enter directory name',
      'New Folder',
      {
        confirmButtonText: 'Create',
        cancelButtonText: 'Cancel'
      }
    )
    if (name) {
      await fileStore.createDirectory(name)
    }
  } catch {
    // User cancelled
  }
}
</script>

<style scoped>
.file-list-container {
  padding: 8px 0 24px;
}

/* ── Toolbar ── */
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.toolbar-group {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

/* Vertical divider between the nav/view group and the action group.
   Hidden on mobile where the toolbar wraps; the gap is enough. */
.toolbar-group--actions {
  padding-left: 10px;
  margin-left: 4px;
  border-left: 1px solid var(--el-border-color-lighter);
}

.toolbar :deep(.el-button) {
  font-size: 13px;
  font-weight: 500;
  transition: all var(--transition-base);
}

.toolbar :deep(.el-button:active) {
  scale: 0.97;
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

/* ── Table ── */
.file-list-container :deep(.el-table) {
  --el-table-border-color: transparent;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-lg);
  overflow: hidden;
}

.file-list-container :deep(.el-table__header-wrapper) {
  border-radius: var(--radius-lg) var(--radius-lg) 0 0;
}

.file-list-container :deep(.el-table__header th) {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--el-text-color-placeholder);
  background: var(--el-fill-color);
  border-bottom: 1px solid var(--el-border-color-lighter);
  padding: 8px 0;
}

.file-list-container :deep(.el-table__header th .cell) {
  padding: 0 12px;
}

.file-list-container :deep(.el-table__body td) {
  padding: 8px 0;
  border-bottom: 1px solid var(--el-border-color-extra-light);
}

.file-list-container :deep(.el-table__body td .cell) {
  padding: 0 12px;
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

/* ── Empty state ── */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 80px 24px;
  text-align: center;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-lg);
  background: var(--el-bg-color);
}

.empty-icon {
  color: var(--el-text-color-placeholder);
  margin-bottom: 16px;
  opacity: 0.6;
}

.empty-title {
  margin: 0 0 8px;
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
}

.empty-hint {
  margin: 0;
  font-size: 14px;
  color: var(--el-text-color-placeholder);
  max-width: 320px;
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

/* ── Mobile card styles ── */
.mobile-file-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-md);
  margin-bottom: 8px;
  cursor: pointer;
  transition:
    background-color var(--transition-base),
    border-color var(--transition-base),
    transform var(--transition-base);
}

.mobile-file-card:last-child {
  margin-bottom: 0;
}

.mobile-file-card:hover {
  background: var(--el-fill-color-light);
  border-color: var(--el-border-color);
}

.mobile-file-card:active {
  transform: scale(0.99);
  background: var(--el-fill-color);
}

.mobile-file-card--dir {
  /* Subtle hint that folders are tappable cards, not links */
}

.mobile-file-icon {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--el-color-primary) 10%, transparent);
  color: var(--el-color-primary);
}

.mobile-file-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.mobile-file-name {
  font-size: 14px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  letter-spacing: -0.005em;
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
}

.mobile-file-actions {
  flex-shrink: 0;
}

/* ── Responsive: Tiny phone ── */
@media (max-width: 400px) {
  .mobile-file-card {
    padding: 10px 12px;
    gap: 10px;
  }

  .mobile-file-icon {
    width: 36px;
    height: 36px;
  }

  .mobile-file-name {
    font-size: 13px;
  }

  .mobile-file-meta {
    font-size: 11px;
  }
}
</style>
