<template>
  <el-dialog
    v-model="visible"
    :title="title"
    width="80%"
    :close-on-click-modal="false"
    destroy-on-close
    @closed="handleClosed"
  >
    <div class="video-container">
      <video
        ref="videoRef"
        class="video-player"
        controls
        autoplay
        :src="videoUrl"
      >
        Your browser does not support the video tag.
      </video>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import type { FileItem } from '@/types'
import { getEncodePath } from '@/utils/path'

interface Props {
  visible: boolean
  file: FileItem | null
  currentPath: string
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

const videoRef = ref<HTMLVideoElement>()

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const title = computed(() => {
  if (!props.file) return 'Video Player'
  return props.file.name
})

const videoUrl = computed(() => {
  if (!props.file) return ''
  return getEncodePath(props.file.name, props.currentPath)
})

function handleClosed() {
  if (videoRef.value) {
    videoRef.value.pause()
    videoRef.value.src = ''
  }
}

watch(
  () => props.visible,
  async (newVal) => {
    if (newVal && videoRef.value) {
      await nextTick()
      videoRef.value.load()
      videoRef.value.focus()
    }
  }
)
</script>

<style scoped>
.video-container {
  display: flex;
  justify-content: center;
  align-items: center;
  background-color: #000;
  border-radius: 4px;
  min-height: 300px;
}

.video-player {
  max-width: 100%;
  max-height: 70vh;
  outline: none;
}
</style>
