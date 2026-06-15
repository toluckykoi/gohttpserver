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

    <el-table
      :data="sortedFiles"
      v-loading="loading"
      style="width: 100%"
      stripe
      @row-click="handleRowClick"
    >
      <el-table-column label="Name" min-width="300">
        <template #default="{ row }">
          <FileItem :file="row" />
        </template>
      </el-table-column>

      <el-table-column label="Size" width="120" align="right">
        <template #default="{ row }">
          <span v-if="row.type === 'dir'" class="size-text">~</span>
          <span v-else class="size-text">{{ formatBytes(row.size) }}</span>
        </template>
      </el-table-column>

      <el-table-column label="Modified" width="200">
        <template #default="{ row }">
          <el-link type="primary" :underline="false" @click.stop="toggleMtimeType">
            {{ formatMtime(row.mtime) }}
          </el-link>
        </template>
      </el-table-column>

      <el-table-column label="Actions" width="340">
        <template #default="{ row }">
          <div class="action-buttons" @click.stop>
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
        </template>
      </el-table-column>
    </el-table>

    <UploadModal
      v-model:visible="showUploadModal"
      @upload="handleUpload"
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
  Download,
  InfoFilled,
  Delete,
  DocumentCopy,
  Camera,
  VideoPlay,
  Box
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

async function handleUpload(file: File, options?: { filename?: string; unzip?: boolean }) {
  await fileStore.uploadFile(file, options)
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
  padding: 20px 0;
}

.toolbar {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.action-buttons {
  display: flex;
  gap: 4px;
  flex-wrap: nowrap;
}

.size-text {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
</style>
