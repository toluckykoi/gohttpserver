<template>
  <div class="file-list-container">
    <div class="toolbar">
      <el-button type="default" @click="goBack">
        <el-icon><ArrowLeft /></el-icon>
        Back
      </el-button>
      <el-button type="default" @click="toggleShowHidden">
        <el-icon><View /></el-icon>
        {{ showHidden ? 'Hide' : 'Show' }} Hidden
      </el-button>
      <el-button
        v-if="auth.upload"
        type="primary"
        @click="showUploadModal = true"
      >
        <el-icon><Upload /></el-icon>
        Upload
      </el-button>
      <el-button
        v-if="auth.delete"
        type="success"
        @click="handleCreateDirectory"
      >
        <el-icon><FolderAdd /></el-icon>
        New Folder
      </el-button>
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

    <el-table
      v-else
      :data="sortedFiles"
      v-loading="loading"
      style="width: 100%"
      :default-sort="{ prop: 'mtime', order: 'descending' }"
      @sort-change="handleSortChange"
      @row-click="handleRowClick"
    >
      <el-table-column label="Name" min-width="300" prop="name" sortable="custom">
        <template #default="{ row }">
          <FileItem :file="row" />
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
                v-if="hasQrCode(row)"
                content="QR Code"
                placement="top"
              >
                <el-button
                  type="warning"
                  :icon="Camera"
                  circle
                  size="small"
                  @click="handleShowQrCode(row)"
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
                    <el-dropdown-item v-if="auth.delete" command="delete">Delete</el-dropdown-item>
                  </template>
                  <template v-else>
                    <el-dropdown-item command="download">Download</el-dropdown-item>
                    <el-dropdown-item command="copy">Copy Link</el-dropdown-item>
                    <el-dropdown-item command="info">Info</el-dropdown-item>
                    <el-dropdown-item v-if="hasQrCode(row)" command="qrcode">QR Code</el-dropdown-item>
                    <el-dropdown-item v-if="isVideo(row)" command="video">Play Video</el-dropdown-item>
                    <el-dropdown-item v-if="hasQrCode(row)" command="install">Install</el-dropdown-item>
                    <el-dropdown-item v-if="auth.delete" command="delete">Delete</el-dropdown-item>
                  </template>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </template>
      </el-table-column>
    </el-table>

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
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useFileStore } from '@/stores/fileStore'
import { useFileApi } from '@/composables/useFileApi'
import type { FileItem as FileItemType } from '@/types'
import { formatBytes } from '@/utils/formatBytes'
import { getEncodePath, parentDirectory } from '@/utils/path'
import { shouldHaveQrcode, isVideoFile } from '@/utils/fileIcon'
import FileItem from './FileItem.vue'
import UploadModal from './UploadModal.vue'
import QrCodeModal from './QrCodeModal.vue'
import FileInfoModal from './FileInfoModal.vue'
import VideoPlayer from './VideoPlayer.vue'
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
  MoreFilled
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
const currentQrFile = ref<FileItemType | null>(null)
const currentInfoFile = ref<FileItemType | null>(null)
const currentVideoFile = ref<FileItemType | null>(null)
const mtimeTypeFromNow = ref(true)

// Computed
const currentPath = computed(() => fileStore.currentPath)
const sortedFiles = computed(() => fileStore.sortedFiles)
const auth = computed(() => fileStore.auth)
const loading = computed(() => fileStore.loading)
const showHidden = computed(() => fileStore.showHidden)

function goBack() {
  const parentPath = parentDirectory(currentPath.value)
  fileStore.loadFiles(parentPath || '/')
}

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

function handleRowClick(row: FileItemType) {
  if (row.type === 'dir') {
    const newPath = getEncodePath(row.name, currentPath.value)
    fileStore.loadFiles(newPath)
  } else {
    const encodePath = getEncodePath(row.name, currentPath.value)
    window.location.href = encodePath
  }
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
    case 'delete': handleDeleteFile(file); break
  }
}

async function handleCopyLink(file: FileItemType) {
  const encodePath = getEncodePath(file.name, currentPath.value)
  const url = window.location.origin + encodePath
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success('Link copied to clipboard')
  } catch {
    ElMessage.error('Failed to copy link')
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
  gap: 8px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.toolbar :deep(.el-button) {
  font-size: 13px;
  font-weight: 500;
  transition: all var(--transition-base);
}

.toolbar :deep(.el-button:active) {
  scale: 0.97;
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

/* ── Responsive: Phone / Small tablet ── */
@media (max-width: 640px) {
  .file-list-container {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }

  .toolbar .el-button {
    font-size: 12px;
    padding: 6px 10px;
  }

  /* Hide desktop action buttons, show dropdown */
  .action-desktop {
    display: none;
  }

  .action-mobile {
    display: flex;
    justify-content: center;
  }

  /* Hide Modified column */
  .file-list-container :deep(.col-modified) {
    display: none !important;
  }

  /* Shrink table columns */
  .file-list-container :deep(.el-table__header colgroup col:nth-child(1)),
  .file-list-container :deep(.el-table__body colgroup col:nth-child(1)) {
    width: auto !important;
  }

  /* Reduce actions column width */
  .file-list-container :deep(.col-actions) {
    width: 52px !important;
    min-width: 52px !important;
  }

  /* Table cell padding reduction */
  .file-list-container :deep(.el-table__header th .cell),
  .file-list-container :deep(.el-table__body td .cell) {
    padding: 0 8px;
  }

  .data-mono {
    font-size: 12px;
  }
}

/* ── Responsive: Tiny phone ── */
@media (max-width: 400px) {
  .toolbar {
    gap: 4px;
  }

  .toolbar .el-button {
    font-size: 11px;
    padding: 4px 8px;
  }

  /* Hide toolbar button labels, icon-only */
  .toolbar .el-button .el-icon + * {
    display: none;
  }

  /* Shrink name column */
  .file-list-container :deep(.el-table__body td .cell) {
    padding: 0 6px;
  }
}
</style>
