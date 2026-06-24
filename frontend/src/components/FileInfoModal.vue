<template>
  <el-dialog
    v-model="visible"
    :title="title"
    width="500px"
  >
    <div v-if="loading" class="loading-container">
      <el-icon class="is-loading" :size="40">
        <Loading />
      </el-icon>
      <p>Loading...</p>
    </div>

    <div v-else class="file-info">
      <el-descriptions :column="1" border>
        <el-descriptions-item label="Name">
          {{ fileInfo?.name || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="Type">
          {{ fileInfo?.type || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="Size">
          {{ formatSize }}
        </el-descriptions-item>
        <el-descriptions-item label="Path">
          {{ fileInfo?.path || '-' }}
        </el-descriptions-item>
        <el-descriptions-item label="Modified">
          {{ formatMtime }}
        </el-descriptions-item>
      </el-descriptions>

      <template v-if="fileInfo?.extra">
        <el-divider content-position="left">Extra Info</el-divider>
        <el-card class="extra-info">
          <pre>{{ JSON.stringify(fileInfo.extra, null, 2) }}</pre>
        </el-card>
      </template>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { FileItem, FileInfo as FileInfoType } from '@/types'
import { useFileApi } from '@/composables/useFileApi'
import { formatBytes } from '@/utils/formatBytes'
import { Loading } from '@element-plus/icons-vue'
import dayjs from 'dayjs'

interface Props {
  visible: boolean
  file: FileItem | null
  currentPath: string
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

const fileApi = useFileApi()

const fileInfo = ref<FileInfoType | null>(null)
const loading = ref(false)

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const title = computed(() => {
  if (!props.file) return 'File Info'
  return `Info: ${props.file.name}`
})

const formatSize = computed(() => {
  if (!fileInfo.value) return '-'
  if (fileInfo.value.type === 'dir') return '~'
  return formatBytes(fileInfo.value.size)
})

const formatMtime = computed(() => {
  if (!fileInfo.value) return '-'
  return dayjs(fileInfo.value.mtime).format('YYYY-MM-DD HH:mm:ss')
})

async function loadFileInfo() {
  if (!props.file) return

  loading.value = true
  try {
    fileInfo.value = await fileApi.fetchFileInfo(props.currentPath, props.file.name)
  } catch (error) {
    console.error('Failed to load file info:', error)
  } finally {
    loading.value = false
  }
}

watch(
  () => props.visible,
  (newVal) => {
    if (newVal) {
      loadFileInfo()
    } else {
      fileInfo.value = null
    }
  }
)
</script>

<style scoped>
.loading-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px;
  gap: 16px;
}

.extra-info {
  margin-top: 16px;
}

.extra-info pre {
  margin: 0;
  overflow-x: auto;
  background: var(--el-fill-color-lighter);
  padding: 12px;
  border-radius: var(--radius-md);
}
</style>
