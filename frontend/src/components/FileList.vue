<template>
  <div class="file-list-container">
    <div class="panel-card">
      <div class="toolbar">
        <Breadcrumb class="toolbar-breadcrumb" />
        <div class="toolbar-controls">
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
                @click="handleCreateDirectory"
              >
                <el-icon><FolderAdd /></el-icon>
                <span class="toolbar-label">New Folder</span>
              </el-button>
            </el-tooltip>
          </div>
        </div>
      </div>

      <div v-if="!loading && sortedFiles.length > 0" class="panel-stats">
        <span>{{ sortedFiles.length }} {{ sortedFiles.length === 1 ? 'item' : 'items' }}</span>
        <span class="panel-stats-sep" aria-hidden="true">·</span>
        <span>{{ formatBytes(totalSize) }}</span>
        <span class="panel-stats-sep" aria-hidden="true">·</span>
        <span>Last modified {{ lastModified }}</span>
      </div>

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
      <div v-if="selectedCount > 0" class="selection-bar" role="status">
        <div class="selection-bar-left">
          <span class="selection-bar-badge" aria-hidden="true">
            <el-icon :size="13"><Check /></el-icon>
          </span>
          <span class="selection-bar-count">
            {{ selectedCount }} selected
            <template v-if="selectedTotalSize > 0">
              <span class="selection-bar-sep">·</span>
              <span class="selection-bar-size data-mono">{{ formatBytes(selectedTotalSize) }}</span>
            </template>
          </span>
        </div>
        <div class="selection-bar-right">
          <el-tooltip content="Download selected as archive" placement="bottom">
            <el-button
              class="selection-bar-action"
              :icon="Download"
              @click="handleDownloadSelected"
            >
              <span class="selection-bar-action-label">Download</span>
            </el-button>
          </el-tooltip>
          <el-tooltip v-if="auth.delete" content="Delete selected items" placement="bottom">
            <el-button
              class="selection-bar-action selection-bar-action--danger"
              :icon="Delete"
              :loading="deletingSelected"
              @click="handleDeleteSelected"
            >
              <span class="selection-bar-action-label">Delete</span>
            </el-button>
          </el-tooltip>
          <span class="selection-bar-divider" aria-hidden="true" />
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

    <el-table
      class="file-table"
      :data="visibleFiles"
      v-loading="loading"
      style="width: 100%"
      :default-sort="defaultSort"
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
          <el-tooltip
            :content="rowClickLabel(row)"
            placement="top"
            :show-after="350"
            :hide-after="0"
            :offset="7"
            :enterable="false"
            popper-class="row-hint-popper"
          >
            <div class="file-name-cell-wrap">
              <FileItem :file="row" />
            </div>
          </el-tooltip>
        </template>
      </el-table-column>

      <el-table-column label="Size" width="120" align="right" prop="size" sortable="custom">
        <template #default="{ row }">
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

      <el-table-column label="Actions" width="132" class-name="col-actions">
        <template #default="{ row }">
          <div class="action-buttons action-desktop" @click.stop>
            <template v-if="row.type === 'dir'">
              <el-tooltip content="Download as ZIP" placement="top">
                <el-button
                  class="row-action-btn"
                  :icon="Download"
                  aria-label="Download as ZIP"
                  @click="handleDownloadArchive(row)"
                />
              </el-tooltip>
            </template>
            <template v-else>
              <el-tooltip v-if="isImage(row)" content="Preview image" placement="top">
                <el-button
                  class="row-action-btn"
                  :icon="Picture"
                  aria-label="Preview image"
                  @click="handleShowImagePreview(row)"
                />
              </el-tooltip>
              <el-tooltip v-else-if="isVideo(row)" content="Play video" placement="top">
                <el-button
                  class="row-action-btn"
                  :icon="VideoPlay"
                  aria-label="Play video"
                  @click="handleVideoPlay(row)"
                />
              </el-tooltip>
              <el-tooltip v-else-if="canPreview(row)" content="Preview" placement="top">
                <el-button
                  class="row-action-btn"
                  :icon="Reading"
                  aria-label="Preview"
                  @click="handleShowPreview(row)"
                />
              </el-tooltip>
              <el-tooltip content="Download" placement="top">
                <el-button
                  class="row-action-btn"
                  :icon="Download"
                  aria-label="Download"
                  @click="handleDownload(row)"
                />
              </el-tooltip>
            </template>
            <el-dropdown trigger="click" placement="bottom-end" @command="(cmd: string) => handleMobileAction(cmd, row)">
              <el-button
                class="row-action-btn"
                :icon="MoreFilled"
                aria-label="More actions"
              />
              <template #dropdown>
                <el-dropdown-menu>
                  <template v-if="row.type === 'dir'">
                    <el-dropdown-item command="zip" :icon="Download">Download as ZIP</el-dropdown-item>
                    <el-dropdown-item command="qrcode" :icon="Camera">QR Code</el-dropdown-item>
                    <el-dropdown-item command="info" :icon="InfoFilled">Info</el-dropdown-item>
                    <el-dropdown-item v-if="auth.delete" command="delete" :icon="Delete" divided>
                      Delete
                    </el-dropdown-item>
                  </template>
                  <template v-else>
                    <el-dropdown-item command="download" :icon="Download">Download</el-dropdown-item>
                    <el-dropdown-item command="copy" :icon="DocumentCopy">Copy Link</el-dropdown-item>
                    <el-dropdown-item command="qrcode" :icon="Camera">QR Code</el-dropdown-item>
                    <el-dropdown-item v-if="isImage(row)" command="image-preview" :icon="Picture">Preview image</el-dropdown-item>
                    <el-dropdown-item v-if="canPreview(row)" command="preview" :icon="Reading">Preview</el-dropdown-item>
                    <el-dropdown-item v-if="isVideo(row)" command="video" :icon="VideoPlay">Play Video</el-dropdown-item>
                    <el-dropdown-item v-if="hasQrCode(row)" command="install" :icon="Box">Install</el-dropdown-item>
                    <el-dropdown-item command="info" :icon="InfoFilled">Info</el-dropdown-item>
                    <el-dropdown-item v-if="auth.delete" command="delete" :icon="Delete" divided>
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

    <div class="mobile-file-list" :class="{ 'is-loading': loading }">
      <div
        v-for="row in visibleFiles"
        :key="row.name"
        class="mobile-file-card"
        :class="{
          'mobile-file-card--dir': row.type === 'dir',
          'mobile-file-card--selected': fileStore.selectedFiles.has(row.path)
        }"
        @click="handleRowClick(row)"
      >
        <el-checkbox
          v-if="fileStore.selectionMode || fileStore.selectedFiles.has(row.path)"
          class="mobile-file-checkbox"
          :model-value="fileStore.selectedFiles.has(row.path)"
          @change="fileStore.toggleSelect(row.path)"
          @click.stop
        />
        <div class="mobile-file-icon" :style="iconTileStyle(row)">
          <svg class="mobile-file-glyph" viewBox="0 0 24 24" fill="currentColor">
            <path v-for="(d, i) in getFileIconPaths(row.name, row.type)" :key="i" :d="d" />
          </svg>
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
            <span>{{ formatMtimeMobile(row.mtime) }}</span>
          </div>
        </div>
        <div v-if="!fileStore.selectionMode" class="mobile-file-actions" @click.stop>
          <!-- 高频操作直达：文件直接下载，文件夹 ZIP 下载 -->
          <el-tooltip
            :content="row.type === 'dir' ? 'Download as ZIP' : 'Download'"
            placement="top"
            :show-after="350"
          >
            <el-button
              class="mobile-quick-btn"
              :icon="Download"
              circle
              :aria-label="row.type === 'dir' ? 'Download as ZIP' : 'Download'"
              @click="row.type === 'dir' ? handleDownloadArchive(row) : handleDownload(row)"
            />
          </el-tooltip>
          <el-dropdown
            trigger="click"
            @command="(cmd: string) => handleMobileAction(cmd, row)"
          >
            <el-button class="mobile-quick-btn" :icon="MoreFilled" circle aria-label="More actions" />
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

    <!-- 增量渲染哨兵：进入视口即扩大渲染窗口。 -->
    <div v-if="sortedFiles.length > 0" ref="sentinelRef" class="load-more-sentinel">
      <el-button
        v-if="hasMoreFiles"
        class="load-more-btn"
        text
        type="primary"
        @click="displayCount += PAGE_SIZE"
      >
        Load more — {{ sortedFiles.length - displayCount }} of {{ sortedFiles.length }} remaining
      </el-button>
    </div>
    </template>
    </div>

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
import { ref, computed, onMounted, onBeforeUnmount, defineAsyncComponent, nextTick, watch } from 'vue'
import { useFileStore } from '@/stores/fileStore'
import { useFileApi } from '@/composables/useFileApi'
import type { FileItem as FileItemType } from '@/types'
import { formatBytes } from '@/utils/formatBytes'
import { getEncodePath, parentDirectory } from '@/utils/path'
import { shouldHaveQrcode, isVideoFile, isPreviewableImage, getFileIconPaths, getFileIconColor } from '@/utils/fileIcon'
import { isPreviewable } from '@/utils/previewable'
import { copyText } from '@/utils/clipboard'
import FileItem from './FileItem.vue'
import Breadcrumb from './Breadcrumb.vue'

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
  Picture,
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
const mtimeTypeFromNow = ref(false)
const refreshing = ref(false)
const elTableRef = ref<TableInstance>()

const currentPath = computed(() => fileStore.currentPath)
const sortedFiles = computed(() => fileStore.sortedFiles)
const auth = computed(() => fileStore.auth)
const loading = computed(() => fileStore.loading)
const showHidden = computed(() => fileStore.showHidden)
const selectedCount = computed(() => fileStore.selectedCount)
const isAllSelected = computed(() => fileStore.isAllSelected)
// 选中项合计大小（目录计 0）。
const selectedTotalSize = computed(() =>
  fileStore.files.reduce(
    (sum, f) => (fileStore.selectedFiles.has(f.path) ? sum + (f.size || 0) : sum),
    0
  )
)
const defaultSort = computed(() => ({ prop: fileStore.sortProp, order: fileStore.sortOrder }))

// 增量渲染，避免超大目录卡顿。
const PAGE_SIZE = 500
const displayCount = ref(PAGE_SIZE)
const visibleFiles = computed(() => sortedFiles.value.slice(0, displayCount.value))
const hasMoreFiles = computed(() => sortedFiles.value.length > displayCount.value)
const sentinelRef = ref<HTMLElement>()
let sentinelObserver: IntersectionObserver | null = null

const totalSize = computed(() =>
  sortedFiles.value.reduce((sum, f) => sum + (f.size || 0), 0)
)
const lastModified = computed(() => {
  if (sortedFiles.value.length === 0) return ''
  const latest = Math.max(...sortedFiles.value.map(f => f.mtime || 0))
  return dayjs(latest).fromNow()
})

/** 移动端卡片图标按类型着色（与 FileItem 保持一致）。 */
function iconTileStyle(file: FileItemType) {
  const color = getFileIconColor(file.name, file.type)
  return {
    color,
    background: `linear-gradient(145deg,
      color-mix(in srgb, ${color} 18%, transparent),
      color-mix(in srgb, ${color} 7%, transparent))`,
    boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${color} 14%, transparent)`
  }
}

/** 图片预览弹窗内上一张/下一张轮播的同级图片。 */
const imageSiblings = computed(() =>
  sortedFiles.value.filter((f) => f.type !== 'dir' && isPreviewableImage(f.name))
)

function goBack() {
  const parentPath = parentDirectory(currentPath.value)
  fileStore.loadFiles(parentPath || '/')
}

async function handleRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  // 图标至少转 400ms，保证刷新反馈可见。
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
  // F5 改为刷新目录而非整页重载。
  if (e.key === 'F5' && !e.ctrlKey && !e.metaKey && !e.shiftKey && !e.altKey) {
    e.preventDefault()
    handleRefresh()
  }
}

// 在 window 级跟踪 Alt 状态，桌面点击与移动端合成下拉事件都要用到。
const isAltPressed = ref(false)

function onAltChange(e: KeyboardEvent) {
  isAltPressed.value = e.altKey
}

// 窗口失焦时 Alt 标记不会收到 keyup，需手动清除。
function onWindowBlur() {
  isAltPressed.value = false
}

onMounted(() => {
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keydown', onAltChange)
  window.addEventListener('keyup', onAltChange)
  window.addEventListener('blur', onWindowBlur)
  sentinelObserver = new IntersectionObserver((entries) => {
    if (entries.some(e => e.isIntersecting) && hasMoreFiles.value) {
      displayCount.value += PAGE_SIZE
    }
  }, { rootMargin: '800px' })
  if (sentinelRef.value) {
    sentinelObserver.observe(sentinelRef.value)
  }
  // 挂载后再套用已保存的排序。
  nextTick(() => {
    if (elTableRef.value && fileStore.files.length > 0) {
      elTableRef.value.sort(fileStore.sortProp, fileStore.sortOrder)
    }
  })
})

// sentinel 元素会随导航被替换，需重新挂载 observer。
watch(sentinelRef, (el, oldEl) => {
  if (oldEl) sentinelObserver?.unobserve(oldEl)
  if (el) sentinelObserver?.observe(el)
}, { flush: 'post' })

watch(() => fileStore.files, (newFiles) => {
  // 新目录：渲染窗口重置回顶部。
  displayCount.value = PAGE_SIZE
  if (newFiles.length > 0 && elTableRef.value) {
    nextTick(() => {
      elTableRef.value?.sort(fileStore.sortProp, fileStore.sortOrder)
    })
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keydown', onAltChange)
  window.removeEventListener('keyup', onAltChange)
  window.removeEventListener('blur', onWindowBlur)
  sentinelObserver?.disconnect()
  sentinelObserver = null
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

/** 移动端卡片专用：固定相对时间，省空间且适合触屏扫读。 */
function formatMtimeMobile(timestamp: number): string {
  return dayjs(timestamp).fromNow()
}

function hasQrCode(file: FileItemType): boolean {
  return shouldHaveQrcode(file.name)
}

function isVideo(file: FileItemType): boolean {
  return isVideoFile(file.name)
}

function isImage(file: FileItemType): boolean {
  return isPreviewableImage(file.name)
}

function canPreview(file: FileItemType): boolean {
  return file.type !== 'dir' && isPreviewable(file.name)
}

function selectOnly(row: FileItemType) {
  const table = elTableRef.value
  if (table) {
    table.clearSelection()
    table.toggleRowSelection(row, true)
  } else {
    fileStore.setSelection([row.path])
  }
}

function handleRowClick(row: FileItemType) {
  if (row.type === 'dir') {
    const newPath = getEncodePath(row.name, currentPath.value)
    fileStore.loadFiles(newPath)
    return
  }

  if (fileStore.selectionMode) {
    fileStore.toggleSelect(row.path)
    return
  }

  selectOnly(row)
}

function rowClickLabel(row: FileItemType): string {
  if (row.type === 'dir') return 'Open folder'
  if (fileStore.selectionMode) {
    return fileStore.selectedFiles.has(row.path) ? 'Tap to deselect' : 'Tap to select'
  }
  return fileStore.selectedFiles.has(row.path) ? 'Selected file' : 'Select file'
}

function handleNavigateImage(target: FileItemType) {
  currentImageFile.value = target
}

/** Element Plus 的 row-class-name 钩子：设置光标样式并同步勾选列的选中态。 */
function rowClassName({ row }: { row: FileItemType }): string {
  // file-row 是行级悬停效果（FileItem 图标缩放）的锚点类。
  const classes = ['file-row', row.type === 'dir' ? 'row-action-navigate' : 'row-action-select']
  if (fileStore.selectedFiles.has(row.path)) {
    classes.push('file-row-selected')
  }
  return classes.join(' ')
}

function handleDownload(file: FileItemType) {
  fileApi.downloadFile(currentPath.value, file.name)
}

function handleDownloadArchive(file: FileItemType) {
  fileApi.downloadArchive(currentPath.value, file.name)
}

/** 选择栏批量下载：按选中数量走最省事的路径。 */
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

function handleShowImagePreview(file: FileItemType) {
  currentImageFile.value = file
  showImagePreviewModal.value = true
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
    case 'image-preview': handleShowImagePreview(file); break
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
  // 按住 Alt 点击可跳过确认。
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
    // 用户取消
  }
}

function handleCreateDirectory() {
  showNewFolderDialog.value = true
}

function handleUrlDownloaded() {
  fileStore.loadFiles(undefined, undefined, { silent: true }).catch(err =>
    console.error('Refresh after URL fetch failed:', err)
  )
}

/** 批量删除：直接调 API，避免每个文件弹一次提示。 */
const deletingSelected = ref(false)
async function handleDeleteSelected() {
  const selected = fileStore.files.filter((f) =>
    fileStore.selectedFiles.has(f.path)
  )
  if (selected.length === 0 || deletingSelected.value) return

  try {
    await ElMessageBox.confirm(
      `Delete ${selected.length} selected item${selected.length > 1 ? 's' : ''}? This cannot be undone.`,
      'Confirm batch delete',
      {
        confirmButtonText: 'Delete',
        cancelButtonText: 'Cancel',
        type: 'warning',
        confirmButtonClass: 'el-button--danger'
      }
    )
  } catch {
    return
  }

  deletingSelected.value = true
  try {
    const results = await Promise.allSettled(
      selected.map((f) => fileApi.deleteFile(currentPath.value, f.name))
    )
    const okPaths = new Set(
      selected
        .filter((_, i) => results[i].status === 'fulfilled')
        .map((f) => f.path)
    )
    // 先本地乐观更新，再静默与服务端重新同步。
    fileStore.files = fileStore.files.filter((f) => !okPaths.has(f.path))
    const remaining = new Set(fileStore.selectedFiles)
    okPaths.forEach((p) => remaining.delete(p))
    fileStore.selectedFiles = remaining

    const failed = results.length - okPaths.size
    if (failed === 0) {
      ElMessage.success(`Deleted ${okPaths.size} item${okPaths.size > 1 ? 's' : ''}`)
    } else {
      ElMessage.warning(`Deleted ${okPaths.size} item${okPaths.size > 1 ? 's' : ''}, ${failed} failed`)
    }
    fileStore.loadFiles(undefined, undefined, { silent: true }).catch(err =>
      console.error('Background refresh after batch delete failed:', err)
    )
  } finally {
    deletingSelected.value = false
  }
}

/** 把 Element Plus 的 selection-change 同步进 store。 */
function handleSelectionChange(selection: FileItemType[]) {
  fileStore.setSelection(selection.map(r => r.path))
}

function handleSelectAll() {
  const table = elTableRef.value
  if (table) {
    // 先 clear 再 toggleAll 才能落到“全选”状态。
    table.clearSelection()
    table.toggleAllSelection()
  } else {
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

.panel-card {
  background: color-mix(in srgb, var(--el-bg-color) 80%, transparent);
  border: 1px solid color-mix(in srgb, var(--el-border-color-lighter) 80%, transparent);
  border-radius: var(--radius-lg);
  box-shadow:
    var(--shadow-sm),
    0 12px 40px -18px color-mix(in srgb, var(--el-color-primary) 10%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  overflow: hidden;
  transition: background-color var(--transition-base),
              border-color var(--transition-base),
              box-shadow var(--transition-base);
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  flex-wrap: wrap;
}

.toolbar .toolbar-breadcrumb {
  flex: 1;
  min-width: 0;
  padding: 0;
}

.toolbar-breadcrumb :deep(.breadcrumb-inner) {
  flex-wrap: nowrap;
  overflow-x: auto;
  scrollbar-width: none;
}

.toolbar-breadcrumb :deep(.breadcrumb-inner)::-webkit-scrollbar {
  display: none;
}

.toolbar-controls {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.panel-stats {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 16px;
  font-size: 11.5px;
  color: var(--el-text-color-placeholder);
  font-variant-numeric: tabular-nums;
  border-bottom: 1px solid color-mix(in srgb, var(--el-border-color-lighter) 70%, transparent);
}

.panel-stats-sep {
  opacity: 0.5;
  user-select: none;
}

.toolbar-group {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  background: color-mix(in srgb, var(--el-fill-color) 50%, transparent);
  border-radius: var(--radius-md);
}

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

.toolbar-group :deep(.el-button:not(.is-circle)) {
  padding: 6px 12px;
}

.toolbar-group :deep(.el-button:hover) {
  background: var(--el-bg-color);
}

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

.selection-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 10px 12px;
  padding: 8px 10px 8px 12px;
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--el-color-primary) 8%, var(--el-bg-color));
  border: 1px solid color-mix(in srgb, var(--el-color-primary) 20%, transparent);
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--el-color-primary) 5%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  animation: fade-up 240ms var(--ease-out) both;
  transition:
    background-color var(--transition-base),
    border-color var(--transition-base);
}

.selection-bar-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  flex-shrink: 0;
  border-radius: 7px;
  color: #fff;
  background: var(--el-color-primary);
  box-shadow: 0 2px 6px color-mix(in srgb, var(--el-color-primary) 35%, transparent);
}

.selection-bar-count {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}

.selection-bar-sep {
  color: var(--el-text-color-secondary);
  font-weight: 400;
}

.selection-bar-size {
  font-weight: 500;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.selection-bar-left {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.selection-bar-right {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.selection-bar-action.el-button {
  height: 28px;
  padding: 0 12px;
  border-radius: 8px;
  font-size: 12.5px;
  font-weight: 600;
  margin-left: 0;
  color: var(--el-color-primary);
  background: color-mix(in srgb, var(--el-color-primary) 12%, var(--el-bg-color));
  border: 1px solid color-mix(in srgb, var(--el-color-primary) 26%, transparent);
  transition: all var(--transition-base);
}

.selection-bar-action.el-button:hover {
  background: color-mix(in srgb, var(--el-color-primary) 18%, var(--el-bg-color));
  border-color: color-mix(in srgb, var(--el-color-primary) 40%, transparent);
  transform: translateY(-1px);
}

.selection-bar-action.el-button:active {
  transform: translateY(0) scale(0.97);
}

.selection-bar-action--danger.el-button {
  color: var(--el-color-danger);
  background: color-mix(in srgb, var(--el-color-danger) 9%, var(--el-bg-color));
  border-color: color-mix(in srgb, var(--el-color-danger) 24%, transparent);
}

.selection-bar-action--danger.el-button:hover {
  background: color-mix(in srgb, var(--el-color-danger) 15%, var(--el-bg-color));
  border-color: color-mix(in srgb, var(--el-color-danger) 38%, transparent);
}

.selection-bar-action :deep(.el-icon) {
  font-size: 14px;
}

.selection-bar-divider {
  width: 1px;
  height: 18px;
  margin: 0 4px;
  background: color-mix(in srgb, var(--el-color-primary) 18%, transparent);
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

@media (max-width: 640px) {
  .selection-bar {
    padding: 6px 8px 6px 10px;
  }

  .selection-bar-sep,
  .selection-bar-size {
    display: none;
  }

  .selection-bar-action.el-button {
    width: 30px;
    padding: 0;
  }

  .selection-bar-action-label {
    display: none;
  }

  .selection-bar-action :deep(.el-icon) {
    margin-right: 0;
  }

  .selection-bar-btn {
    font-size: 12px;
    padding-left: 6px;
    padding-right: 6px;
  }
}

.toolbar-select-toggle {
  display: none;
}

@media (max-width: 640px) {
  .toolbar {
    gap: 8px;
  }

  .toolbar-label {
    display: none;
  }

  /* 触屏用统一的 40×40 图标按钮。 */
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

  .toolbar-group--actions {
    padding-left: 0;
    margin-left: 0;
    border-left: none;
  }

  .toolbar-group {
    flex-wrap: nowrap;
  }

  .toolbar-select-toggle {
    display: inline-flex;
  }
}

/* 超小屏缩小触摸目标，尽量单行容纳更多图标。 */
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

.file-list-container :deep(.el-table) {
  --el-table-border-color: transparent;
  --el-table-border: none;
  --el-table-row-hover-bg-color: color-mix(
    in srgb,
    var(--el-color-primary) 5%,
    transparent
  );
  --el-table-selected-row-bg-color: color-mix(
    in srgb,
    var(--el-color-primary) 12%,
    transparent
  );
  --el-table-header-bg-color: color-mix(in srgb, var(--el-fill-color) 50%, transparent);
  background: transparent;
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
  padding: 7px 0;
  border-bottom: 1px solid color-mix(in srgb, var(--el-border-color-lighter) 55%, transparent);
  background: transparent;
  transition: background-color var(--transition-base),
              box-shadow var(--transition-base);
  /* 锁定 42px：紧凑行距，内容（28px 芯片/按钮）上下各留 7px。 */
  height: 42px !important;
  max-height: 42px !important;
}

.file-list-container :deep(.el-table__body tr) {
  cursor: pointer;
  transition: background-color var(--transition-base);
  height: 42px !important;
  max-height: 42px !important;
}

/* 紧凑行专用 line-height。 */
.file-list-container :deep(.el-table .cell) {
  line-height: 28px;
  max-height: 28px;
  overflow: hidden;
}

.file-list-container :deep(.el-table-column--selection > .cell),
.file-list-container :deep(.col-actions .cell) {
  height: 28px;
  max-height: 28px;
}

.file-list-container :deep(.el-table__body td .cell) {
  padding: 0 16px;
}

/* 收窄复选框与文件名之间的间距。 */
.file-list-container :deep(.el-table__body td.el-table-column--selection .cell),
.file-list-container :deep(.el-table__header th.el-table-column--selection .cell) {
  padding: 0 2px 0 12px;
}

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

/* 行悬停：干净的统一色调（EP 悬停变量负责 background-color）+
   左侧主题色强调条。不叠加渐变——渐变压在 EP 底色上会形成浑浊的两段色。 */
.file-list-container :deep(.el-table__body tr:hover > td.el-table__cell:first-child) {
  box-shadow: inset 3px 0 0 0 var(--el-color-primary);
}

/* 选中行常驻强调条，与悬停的视觉语言保持一致。 */
.file-list-container :deep(.el-table__body tr.file-row-selected > td.el-table__cell:first-child) {
  box-shadow: inset 3px 0 0 0 var(--el-color-primary);
}

.file-list-container :deep(.el-table__body tr:last-child td) {
  border-bottom: none;
}

.file-list-container :deep(.el-table__body tr:active) {
  background: var(--el-fill-color);
}

.data-mono {
  font-family: var(--font-mono);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
  color: var(--el-text-color-secondary);
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

.action-buttons {
  display: flex;
  gap: 6px;
  flex-wrap: nowrap;
}

.file-name-cell-wrap {
  display: flex;
  align-items: center;
  width: 100%;
  min-width: 0;
}

.file-list-container :deep(tr.row-action-navigate),
.file-list-container :deep(tr.row-action-select) {
  cursor: pointer;
}

.file-list-container :deep(.el-table__body tr.file-row-selected > td.el-table__cell) {
  background: var(--el-table-selected-row-bg-color);
}

.file-list-container :deep(.el-table__body tr.file-row-selected:hover > td.el-table__cell) {
  background: color-mix(in srgb, var(--el-color-primary) 16%, transparent);
}

.action-buttons {
  justify-content: flex-end;
}

.action-buttons :deep(.el-button + .el-button) {
  margin-left: 0;
}

.action-buttons :deep(.el-dropdown) {
  display: inline-flex;
}

/* 操作按钮：常显芯片式——默认即有柔和底色与描边，一眼可见；
   行悬停时浮起，按钮自身悬停染上主题色。 */
.action-buttons :deep(.row-action-btn.el-button) {
  width: 26px;
  height: 26px;
  min-height: 26px;
  padding: 0;
  margin-left: 0;
  border-radius: 7px;
  color: var(--el-text-color-regular);
  background: color-mix(in srgb, var(--el-fill-color) 75%, transparent);
  border: 1px solid color-mix(in srgb, var(--el-border-color-lighter) 90%, transparent);
  box-shadow: none;
  transition: color var(--transition-base),
              border-color var(--transition-base),
              background var(--transition-base),
              box-shadow var(--transition-base),
              transform var(--transition-fast);
}

/* 行悬停/选中：按钮浮起，更贴近可点击的实体感。 */
.file-list-container :deep(.el-table__body tr:hover .row-action-btn.el-button),
.file-list-container :deep(.el-table__body tr.file-row-selected .row-action-btn.el-button) {
  color: var(--el-text-color-primary);
  background: var(--el-bg-color);
  border-color: var(--el-border-color);
  box-shadow: var(--shadow-sm);
}

.action-buttons :deep(.row-action-btn.el-button:hover) {
  color: var(--el-color-primary);
  border-color: color-mix(in srgb, var(--el-color-primary) 45%, transparent);
  background: color-mix(in srgb, var(--el-color-primary) 9%, var(--el-bg-color));
  transform: translateY(-1px);
  box-shadow: 0 2px 8px color-mix(in srgb, var(--el-color-primary) 16%, transparent);
}

.action-buttons :deep(.row-action-btn.el-button:active) {
  transform: scale(0.92) translateY(0);
}

.action-buttons :deep(.row-action-btn .el-icon) {
  font-size: 15px;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  margin: 12px;
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

.file-list-container :deep(.el-loading-mask) {
  border-radius: var(--radius-lg);
}

.load-more-sentinel {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 4px;
  padding: 4px 0;
}

.load-more-btn {
  font-size: 13px;
}

.mobile-file-list {
  display: none;
}

@media (max-width: 640px) {
  .file-list-container {
    overflow-x: visible;
  }

  .toolbar .el-button {
    font-size: 12px;
    padding: 6px 10px;
  }

  /* 手机隐藏表格，改用卡片列表。 */
  .file-list-container :deep(.file-table) {
    display: none !important;
  }

  .mobile-file-list {
    display: block;
    padding: 10px 12px 12px;
  }

  .toolbar-breadcrumb {
    flex: 1 1 100%;
  }

  /* 隐藏 Modified 列（表格若意外出现也生效）。 */
  .file-list-container :deep(.col-modified) {
    display: none !important;
  }

  .data-mono {
    font-size: 12px;
  }
}

.mobile-file-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 11px 14px;
  background: color-mix(in srgb, var(--el-bg-color) 80%, transparent);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-lg);
  margin-bottom: 7px;
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
  width: 40px;
  height: 40px;
  border-radius: var(--radius-md);
  /* color 与 background 由 iconTileStyle 按文件类型注入 */
  transition: background var(--transition-base),
              transform var(--transition-base);
}

.mobile-file-card:hover .mobile-file-icon {
  transform: scale(1.04);
}

.mobile-file-glyph {
  width: 20px;
  height: 20px;
  display: block;
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
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

/* 快捷按钮（下载 / 更多）：36px 触摸目标，芯片式与桌面行按钮同一视觉语言。 */
.mobile-quick-btn {
  width: 36px;
  height: 36px;
  min-height: 36px;
  padding: 0;
  border-radius: var(--radius-pill);
  color: var(--el-text-color-regular);
  background: color-mix(in srgb, var(--el-fill-color) 75%, transparent);
  border: 1px solid color-mix(in srgb, var(--el-border-color-lighter) 90%, transparent);
  transition: color var(--transition-base),
              border-color var(--transition-base),
              background var(--transition-base),
              transform var(--transition-fast);
}

.mobile-quick-btn:active {
  transform: scale(0.92);
  color: var(--el-color-primary);
  border-color: color-mix(in srgb, var(--el-color-primary) 45%, transparent);
  background: color-mix(in srgb, var(--el-color-primary) 9%, var(--el-bg-color));
}

@media (pointer: fine) {
  .mobile-quick-btn:hover {
    color: var(--el-color-primary);
    border-color: color-mix(in srgb, var(--el-color-primary) 45%, transparent);
    background: color-mix(in srgb, var(--el-color-primary) 9%, var(--el-bg-color));
  }
}

@media (max-width: 400px) {
  .mobile-file-card {
    padding: 10px 12px;
    gap: 10px;
  }

  .mobile-file-icon {
    width: 36px;
    height: 36px;
  }

  .mobile-file-glyph {
    width: 18px;
    height: 18px;
  }

  .mobile-file-name {
    font-size: 13px;
  }

  .mobile-file-meta {
    font-size: 11px;
  }

  .mobile-quick-btn {
    width: 34px;
    height: 34px;
    min-height: 34px;
  }
}
</style>
