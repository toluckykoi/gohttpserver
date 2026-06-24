<template>
  <el-dialog
    v-model="visible"
    :title="title"
    width="780px"
    align-center
    :show-close="true"
    class="image-dialog"
    @opened="onOpened"
    @closed="resetState"
  >
    <template #header>
      <div class="image-header">
        <div class="image-header-main">
          <span class="image-title" :title="props.file?.name ?? ''">
            {{ props.file?.name }}
          </span>
          <el-tag v-if="props.file" size="small" round type="info">
            {{ formatBytes(props.file.size) }}
          </el-tag>
        </div>
        <div v-if="hasSiblings" class="image-nav-info">
          {{ siblingIndex + 1 }} / {{ siblings.length }}
        </div>
      </div>
    </template>

    <div ref="viewportRef" class="image-viewport" @wheel.prevent="handleWheel">
      <!-- Prev / Next buttons -->
      <button
        v-if="hasPrev"
        class="image-nav image-nav--prev"
        aria-label="Previous image"
        @click="navigate(-1)"
      >
        <el-icon :size="20"><ArrowLeft /></el-icon>
      </button>
      <button
        v-if="hasNext"
        class="image-nav image-nav--next"
        aria-label="Next image"
        @click="navigate(1)"
      >
        <el-icon :size="20"><ArrowRight /></el-icon>
      </button>

      <div
        ref="stageRef"
        class="image-stage"
        :class="{ 'image-stage--dragging': isDragging }"
        @mousedown="onDragStart"
        @mousemove="onDragMove"
        @mouseup="onDragEnd"
        @mouseleave="onDragEnd"
      >
        <img
          v-if="src"
          :src="src"
          :alt="props.file?.name ?? ''"
          class="image-canvas"
          :style="imageStyle"
          @load="onImageLoad"
          @error="onImageError"
          draggable="false"
        />
        <div v-else-if="loadError" class="image-error">
          <el-icon :size="40"><Picture /></el-icon>
          <p>Failed to load image</p>
        </div>
      </div>
    </div>

    <div class="image-toolbar">
      <div class="image-toolbar-group">
        <el-tooltip content="Zoom out" placement="top">
          <el-button :icon="ZoomOut" circle size="small" @click="zoom(-0.2)" />
        </el-tooltip>
        <span class="image-zoom-label">{{ Math.round(scale * 100) }}%</span>
        <el-tooltip content="Zoom in" placement="top">
          <el-button :icon="ZoomIn" circle size="small" @click="zoom(0.2)" />
        </el-tooltip>
        <el-tooltip content="Reset" placement="top">
          <el-button :icon="Refresh" circle size="small" @click="resetTransform" />
        </el-tooltip>
      </div>

      <div class="image-toolbar-group">
        <el-tooltip content="Rotate left" placement="top">
          <el-button :icon="RefreshLeft" circle size="small" @click="rotate(-90)" />
        </el-tooltip>
        <el-tooltip content="Rotate right" placement="top">
          <el-button :icon="RefreshRight" circle size="small" @click="rotate(90)" />
        </el-tooltip>
      </div>

      <div class="image-toolbar-group image-toolbar-group--right">
        <el-tooltip content="Download" placement="top">
          <el-button :icon="Download" size="small" @click="handleDownload">
            Download
          </el-button>
        </el-tooltip>
      </div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import type { FileItem } from '@/types'
import { getEncodePath } from '@/utils/path'
import { isImageFile } from '@/utils/fileIcon'
import { useFileApi } from '@/composables/useFileApi'
import { formatBytes } from '@/utils/formatBytes'
import {
  ArrowLeft,
  ArrowRight,
  ZoomIn,
  ZoomOut,
  Refresh,
  RefreshLeft,
  RefreshRight,
  Download,
  Picture
} from '@element-plus/icons-vue'

interface Props {
  visible: boolean
  file: FileItem | null
  currentPath: string
  /** Sibling image files in the same directory, for prev/next navigation. */
  siblings?: FileItem[]
}

const props = withDefaults(defineProps<Props>(), { siblings: () => [] })
const emit = defineEmits<{
  'update:visible': [value: boolean]
  navigate: [file: FileItem]
}>()

const fileApi = useFileApi()
const viewportRef = ref<HTMLElement>()
const stageRef = ref<HTMLElement>()

const scale = ref(1)
const rotation = ref(0)
const offsetX = ref(0)
const offsetY = ref(0)
const isDragging = ref(false)
const dragStart = ref({ x: 0, y: 0, ox: 0, oy: 0 })
const naturalSize = ref({ w: 0, h: 0 })
const loadError = ref(false)

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const title = computed(() => {
  if (!props.file) return 'Image Preview'
  return `Image: ${props.file.name}`
})

const src = computed(() => {
  if (!props.file) return ''
  return getEncodePath(props.file.name, props.currentPath)
})

const siblings = computed(() =>
  (props.siblings ?? []).filter((f) => f.type !== 'dir' && isImageFile(f.name))
)

const hasSiblings = computed(() => siblings.value.length > 1)

const siblingIndex = computed(() => {
  if (!props.file) return 0
  const i = siblings.value.findIndex((f) => f.name === props.file!.name)
  return i >= 0 ? i : 0
})

const hasPrev = computed(() => siblingIndex.value > 0)
const hasNext = computed(() => siblingIndex.value < siblings.value.length - 1)

const imageStyle = computed(() => ({
  transform:
    `translate(${offsetX.value}px, ${offsetY.value}px) ` +
    `scale(${scale.value}) rotate(${rotation.value}deg)`,
  cursor: isDragging.value ? 'grabbing' : 'grab'
}))

function resetTransform() {
  scale.value = 1
  rotation.value = 0
  offsetX.value = 0
  offsetY.value = 0
}

function resetState() {
  resetTransform()
  loadError.value = false
  naturalSize.value = { w: 0, h: 0 }
}

function zoom(delta: number) {
  const next = Math.max(0.1, Math.min(8, scale.value + delta))
  scale.value = next
  if (next === 1) {
    offsetX.value = 0
    offsetY.value = 0
  }
}

function rotate(deg: number) {
  rotation.value = (rotation.value + deg) % 360
}

function handleWheel(e: WheelEvent) {
  const delta = e.deltaY < 0 ? 0.1 : -0.1
  zoom(delta)
}

function onDragStart(e: MouseEvent) {
  if (scale.value <= 1) return
  isDragging.value = true
  dragStart.value = { x: e.clientX, y: e.clientY, ox: offsetX.value, oy: offsetY.value }
}

function onDragMove(e: MouseEvent) {
  if (!isDragging.value) return
  offsetX.value = dragStart.value.ox + (e.clientX - dragStart.value.x)
  offsetY.value = dragStart.value.oy + (e.clientY - dragStart.value.y)
}

function onDragEnd() {
  isDragging.value = false
}

function onImageLoad(e: Event) {
  const img = e.target as HTMLImageElement
  naturalSize.value = { w: img.naturalWidth, h: img.naturalHeight }
  loadError.value = false
}

function onImageError() {
  loadError.value = true
}

function onOpened() {
  resetState()
}

function handleDownload() {
  if (!props.file) return
  fileApi.downloadFile(props.currentPath, props.file.name)
}

function navigate(delta: number) {
  const list = siblings.value
  if (list.length === 0) return
  const next = siblingIndex.value + delta
  if (next < 0 || next >= list.length) return
  // The parent owns the file; emit and let it update.
  const target = list[next]
  emit('navigate', target)
}

// Keyboard navigation: left/right arrows when modal is open.
function onKeyDown(e: KeyboardEvent) {
  if (!visible.value) return
  if (e.key === 'ArrowLeft' && hasPrev.value) {
    e.preventDefault()
    navigate(-1)
  } else if (e.key === 'ArrowRight' && hasNext.value) {
    e.preventDefault()
    navigate(1)
  } else if (e.key === 'Escape' && scale.value !== 1) {
    e.preventDefault()
    resetTransform()
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKeyDown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
})

watch(
  () => props.file,
  () => {
    loadError.value = false
    if (visible.value) resetTransform()
  }
)
</script>

<style scoped>
/* ── Header ── */
.image-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  width: 100%;
  padding-right: 32px;
  box-sizing: border-box;
}

.image-header-main {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.image-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 360px;
}

.image-nav-info {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  background: var(--el-fill-color);
}

/* ── Viewport ── */
.image-viewport {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 60vh;
  min-height: 360px;
  background:
    repeating-conic-gradient(
      var(--el-fill-color-lighter) 0% 25%,
      var(--el-fill-color-light) 0% 50%
    )
    50% / 16px 16px;
  border-radius: var(--radius-md);
  overflow: hidden;
  user-select: none;
}

.image-stage {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: grab;
}

.image-stage--dragging {
  cursor: grabbing;
}

.image-canvas {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  transition: transform 0.12s ease-out;
  user-select: none;
  -webkit-user-drag: none;
}

.image-error {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  color: var(--el-text-color-placeholder);
}

/* ── Prev / Next buttons ── */
.image-nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  z-index: 2;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: none;
  background: color-mix(in srgb, var(--el-bg-color) 80%, transparent);
  color: var(--el-text-color-regular);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.12);
  transition: all 0.2s;
  backdrop-filter: blur(4px);
}

.image-nav:hover {
  background: var(--el-bg-color);
  color: var(--el-color-primary);
  transform: translateY(-50%) scale(1.08);
}

.image-nav--prev { left: 12px; }
.image-nav--next { right: 12px; }

/* ── Toolbar ── */
.image-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-top: 14px;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: var(--radius-md);
  flex-wrap: wrap;
}

.image-toolbar-group {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.image-toolbar-group--right {
  margin-left: auto;
}

.image-zoom-label {
  min-width: 48px;
  text-align: center;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  color: var(--el-text-color-secondary);
}

/* ── Dialog body padding ── */
:deep(.image-dialog .el-dialog__body) {
  padding: 4px 20px 20px;
}

/* ── Responsive ── */
@media (max-width: 768px) {
  :deep(.image-dialog) {
    width: calc(100vw - 32px) !important;
    max-width: none !important;
  }

  .image-title {
    max-width: 180px;
    font-size: 14px;
  }

  .image-viewport {
    height: 50vh;
    min-height: 280px;
  }

  .image-nav {
    width: 32px;
    height: 32px;
  }
}
</style>
