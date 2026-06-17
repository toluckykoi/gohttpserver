<template>
  <el-dialog
    v-model="visible"
    :title="title"
    width="400px"
  >
    <div class="qrcode-container">
      <div ref="qrcodeRef" class="qrcode"></div>
      <div class="qrcode-info">
        <el-link :href="url" target="_blank" type="primary">
          {{ url }}
        </el-link>
      </div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import type { FileItem } from '@/types'
import { getEncodePath } from '@/utils/path'
import QRCode from 'qrcode'

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

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

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
      margin: 2,
    })
    qrcodeRef.value.innerHTML = svg
  } catch (error) {
    console.error('Failed to render QR code:', error)
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
.qrcode-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 20px;
}

.qrcode {
  display: flex;
  justify-content: center;
  align-items: center;
}

.qrcode-info {
  text-align: center;
  word-break: break-all;
}
</style>