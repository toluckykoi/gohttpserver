<template>
  <el-dialog
    v-model="visible"
    :title="title"
    width="400px"
    align-center
    :show-close="true"
    class="qrcode-dialog"
  >
    <template #header>
      <span class="qrcode-dialog-title" :title="title">{{ title }}</span>
    </template>
    <div class="qrcode-card">
      <div class="qrcode-header">
        <div class="qrcode-icon">
          <el-icon :size="22"><Iphone /></el-icon>
        </div>
        <div class="qrcode-header-text">
          <div class="qrcode-title">Scan with your phone</div>
          <div class="qrcode-subtitle">
            {{ isFileMode ? 'Open this file on your mobile device' : 'Continue browsing on your phone' }}
          </div>
        </div>
      </div>

      <div class="qrcode-stage">
        <div class="qrcode-corners">
          <span class="corner corner-tl" />
          <span class="corner corner-tr" />
          <span class="corner corner-bl" />
          <span class="corner corner-br" />
        </div>
        <div class="qrcode-frame">
          <div ref="qrcodeRef" class="qrcode-canvas" />
          <div class="qrcode-logo">
            <img src="/favicon.png" alt="logo" />
          </div>
        </div>
      </div>

      <div class="qrcode-url-card">
        <el-link
          :href="url"
          target="_blank"
          type="primary"
          class="qrcode-url"
          :underline="false"
        >
          <el-icon :size="14"><Link /></el-icon>
          <span class="qrcode-url-text" :title="url">{{ url }}</span>
        </el-link>
        <el-button
          :icon="copied ? CircleCheck : CopyDocument"
          class="copy-btn"
          :class="{ 'copy-btn--done': copied }"
          @click="handleCopy"
        >
          {{ copied ? 'Copied' : 'Copy' }}
        </el-button>
      </div>

      <div class="qrcode-tip">
        <el-icon :size="13"><InfoFilled /></el-icon>
        <span>Point your camera at the QR code to open the link</span>
      </div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import type { FileItem } from '@/types'
import { getEncodePath } from '@/utils/path'
import QRCode from 'qrcode'
import { ElMessage } from 'element-plus'
import { copyText } from '@/utils/clipboard'
import {
  Iphone,
  Link,
  CopyDocument,
  CircleCheck,
  InfoFilled
} from '@element-plus/icons-vue'

interface Props {
  visible: boolean
  file: FileItem | null
  currentPath: string
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

const qrcodeRef = ref<HTMLElement>()
const copied = ref(false)

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const isFileMode = computed(() => Boolean(props.file))

const url = computed(() => {
  if (props.file) {
    const encodePath = getEncodePath(props.file.name, props.currentPath)
    return window.location.origin + encodePath
  }
  // View in Phone: QR code for current page
  return window.location.href
})

const title = computed(() => {
  if (props.file) return props.file.name
  return 'View in Phone'
})

async function renderQrcode() {
  if (!qrcodeRef.value || !url.value) return

  await nextTick()

  try {
    const svg = await QRCode.toString(url.value, {
      type: 'svg',
      width: 256,
      margin: 1,
      errorCorrectionLevel: 'H',
      color: {
        dark: '#0f172a',
        light: '#ffffff'
      }
    })
    qrcodeRef.value.innerHTML = svg
  } catch (error) {
    console.error('Failed to render QR code:', error)
  }
}

async function handleCopy() {
  const ok = await copyText(url.value)
  if (ok) {
    copied.value = true
    ElMessage.success('Link copied to clipboard')
    setTimeout(() => {
      copied.value = false
    }, 1800)
  } else {
    ElMessage.error('Failed to copy link — please copy manually')
  }
}

watch(
  () => props.visible,
  async (newVal) => {
    if (newVal) {
      await nextTick()
      await renderQrcode()
    }
  }
)
</script>

<style scoped>
.qrcode-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 18px;
  padding: 4px 4px 2px;
}

/* ── Header ── */
.qrcode-header {
  display: flex;
  align-items: center;
  gap: 12px;
  align-self: stretch;
  padding: 14px 16px;
  border-radius: var(--radius-lg);
  background: linear-gradient(
    135deg,
    color-mix(in srgb, var(--el-color-primary) 12%, transparent),
    color-mix(in srgb, var(--el-color-primary) 4%, transparent)
  );
  border: 1px solid color-mix(in srgb, var(--el-color-primary) 18%, transparent);
}

.qrcode-icon {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border-radius: var(--radius-md);
  background: var(--el-color-primary);
  color: #fff;
  box-shadow: 0 4px 12px color-mix(in srgb, var(--el-color-primary) 35%, transparent);
}

.qrcode-header-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.qrcode-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  letter-spacing: -0.01em;
}

.qrcode-subtitle {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.4;
}

/* ── QR Stage ── */
.qrcode-stage {
  position: relative;
  width: 240px;
  height: 240px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.qrcode-frame {
  position: relative;
  width: 220px;
  height: 220px;
  border-radius: var(--radius-xl);
  background: #ffffff;
  padding: 12px;
  box-shadow:
    0 1px 2px rgba(15, 23, 42, 0.04),
    0 12px 32px -8px rgba(15, 23, 42, 0.12),
    0 24px 48px -12px rgba(15, 23, 42, 0.08);
  box-sizing: border-box;
  transition: transform 0.3s cubic-bezier(0.2, 0.8, 0.2, 1);
}

.qrcode-frame:hover {
  transform: translateY(-2px);
}

.qrcode-canvas {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
}

.qrcode-canvas :deep(svg) {
  display: block;
  width: 100%;
  height: 100%;
  border-radius: var(--radius-sm);
}

.qrcode-logo {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 44px;
  height: 44px;
  background: #ffffff;
  border-radius: var(--radius-md);
  padding: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow:
    0 2px 6px rgba(15, 23, 42, 0.08),
    0 0 0 4px #ffffff;
}

.qrcode-logo img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  border-radius: var(--radius-sm);
}

/* Corner brackets — visual "scan frame" */
.qrcode-corners {
  position: absolute;
  inset: 0;
  pointer-events: none;
}

.corner {
  position: absolute;
  width: 18px;
  height: 18px;
  border-color: var(--el-color-primary);
  border-style: solid;
  border-width: 0;
}

.corner-tl {
  top: 0;
  left: 0;
  border-top-width: 2px;
  border-left-width: 2px;
  border-top-left-radius: var(--radius-sm);
}

.corner-tr {
  top: 0;
  right: 0;
  border-top-width: 2px;
  border-right-width: 2px;
  border-top-right-radius: var(--radius-sm);
}

.corner-bl {
  bottom: 0;
  left: 0;
  border-bottom-width: 2px;
  border-left-width: 2px;
  border-bottom-left-radius: var(--radius-sm);
}

.corner-br {
  bottom: 0;
  right: 0;
  border-bottom-width: 2px;
  border-right-width: 2px;
  border-bottom-right-radius: var(--radius-sm);
}

/* ── URL card ── */
.qrcode-url-card {
  display: flex;
  align-items: center;
  gap: 8px;
  align-self: stretch;
  padding: 6px 6px 6px 14px;
  border-radius: var(--radius-md);
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-lighter);
  transition: border-color 0.2s, box-shadow 0.2s;
}

.qrcode-url-card:hover {
  border-color: var(--el-border-color);
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.04);
}

.qrcode-url {
  flex: 1;
  min-width: 0;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.qrcode-url-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.copy-btn {
  flex-shrink: 0;
  --el-button-size: 28px;
  font-size: 12px;
  padding: 0 10px;
  height: 28px;
  transition: all 0.2s;
}

.copy-btn--done {
  --el-button-bg-color: color-mix(in srgb, var(--el-color-success) 12%, var(--el-fill-color-blank));
  --el-button-border-color: color-mix(in srgb, var(--el-color-success) 30%, var(--el-border-color-lighter));
  --el-button-text-color: var(--el-color-success);
}

/* ── Tip ── */
.qrcode-tip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  color: var(--el-text-color-placeholder);
}

.qrcode-tip .el-icon {
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
}

/* ── Dialog polish ── */
:deep(.qrcode-dialog .el-dialog__header) {
  margin-right: 0;
  padding-bottom: 12px;
  padding-right: 32px; /* leave room for the close button */
}

:deep(.qrcode-dialog .el-dialog__body) {
  padding: 8px 20px 22px;
}

/* Dialog title: long file names truncate with ellipsis and reveal the full
   name on hover via the native title attribute. Padding-right reserves space
   for the close button so the ellipsis never crashes into it. */
.qrcode-dialog-title,
:deep(.qrcode-dialog .el-dialog__title) {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

/* Subtitle: never break the layout, even on the narrowest phones. */
.qrcode-subtitle {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

/* URL link: el-link renders an <a> that needs min-width:0 to let the inner
   span actually shrink and ellipsize. */
.qrcode-url {
  min-width: 0;
  max-width: 100%;
}

.qrcode-url :deep(.el-link__inner),
.qrcode-url :deep(a) {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
}

.qrcode-url-text {
  flex: 1;
  min-width: 0;
  display: inline-block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  /* The native title attribute on this span is the only way for the user to
     see the part that was truncated — el-link's own tooltip wouldn't fire
     because the inner text is wrapped in <a><span>. */
}

@media (max-width: 480px) {
  .qrcode-stage {
    width: 220px;
    height: 220px;
  }

  .qrcode-frame {
    width: 200px;
    height: 200px;
  }

  :deep(.qrcode-dialog .el-dialog__body) {
    padding: 6px 14px 18px;
  }

  .qrcode-url-card {
    padding: 5px 5px 5px 12px;
  }

  .copy-btn {
    font-size: 11px;
    padding: 0 8px;
  }
}
</style>
