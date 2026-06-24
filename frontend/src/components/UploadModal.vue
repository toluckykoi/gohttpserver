<template>
  <el-dialog
    v-model="visible"
    title="File Upload"
    width="500px"
    :close-on-click-modal="!uploading"
    :close-on-press-escape="!uploading"
    :show-close="!uploading"
    @closed="handleClosed"
  >
    <el-upload
      ref="uploadRef"
      class="upload-demo"
      drag
      :auto-upload="false"
      :on-change="handleFileChange"
      :on-remove="handleFileRemove"
      :file-list="fileList"
      :disabled="uploading"
      multiple
    >
      <el-icon class="el-icon--upload"><upload-filled /></el-icon>
      <div class="el-upload__text">
        Drop file here or <em>click to upload</em>
      </div>
      <template #tip>
        <div class="el-upload__tip">
          File size should not exceed 10GB
        </div>
      </template>
    </el-upload>

    <!-- Upload progress -->
    <div v-if="uploading" class="upload-progress">
      <div class="progress-file-name">{{ currentFileName }}</div>
      <el-progress
        :percentage="currentPercent"
        :status="progressStatus"
        :stroke-width="20"
        :text-inside="true"
      />
      <div class="progress-stats">
        {{ currentFileIndex }} / {{ fileList.length }} files
        &middot;
        {{ formatSize(uploadedBytes) }} / {{ formatSize(totalBytes) }}
      </div>
    </div>

    <div class="options" v-if="fileList.length > 0 && !uploading">
      <el-checkbox v-model="unzipAfterUpload">
        Unzip after upload (for .zip files)
      </el-checkbox>
    </div>

    <template #footer>
      <div class="dialog-footer">
        <el-button @click="handleCancel" :disabled="uploading">
          {{ uploading ? 'Uploading...' : 'Cancel' }}
        </el-button>
        <el-button
          type="primary"
          @click="handleUpload"
          :loading="uploading"
          :disabled="fileList.length === 0"
        >
          Upload
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import type { UploadUserFile } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useFileApi } from '@/composables/useFileApi'
import { useFileStore } from '@/stores/fileStore'

interface Props {
  visible: boolean
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

const fileApi = useFileApi()
const fileStore = useFileStore()

const uploadRef = ref()
const fileList = ref<UploadUserFile[]>([])
const uploading = ref(false)
const unzipAfterUpload = ref(false)

// Progress state
const currentFileIndex = ref(0)
const currentFileName = ref('')
const currentPercent = ref(0)
const uploadedBytes = ref(0)
const totalBytes = ref(0)

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const progressStatus = computed(() => {
  if (currentPercent.value >= 100) return 'success'
  return ''
})

function handleFileChange(file: UploadUserFile) {
  if (file.raw) {
    fileList.value.push(file)
  }
}

function handleFileRemove(file: UploadUserFile) {
  const index = fileList.value.findIndex(f => f.uid === file.uid)
  if (index > -1) {
    fileList.value.splice(index, 1)
  }
}

function formatSize(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i]
}

async function handleUpload() {
  if (fileList.value.length === 0) {
    ElMessage.warning('Please select files to upload')
    return
  }

  uploading.value = true
  currentFileIndex.value = 0
  uploadedBytes.value = 0

  // Calculate total size
  totalBytes.value = fileList.value.reduce((sum, f) => sum + (f.raw?.size ?? 0), 0)

  let completedBytes = 0

  try {
    for (let i = 0; i < fileList.value.length; i++) {
      const file = fileList.value[i]
      if (!file.raw) continue

      currentFileIndex.value = i + 1
      currentFileName.value = file.name
      currentPercent.value = 0

      const fileSize = file.raw.size

      await fileApi.uploadFileWithProgress(
        fileStore.currentPath,
        file.raw,
        (percent: number) => {
          currentPercent.value = percent
          uploadedBytes.value = completedBytes + Math.round((percent / 100) * fileSize)
        },
        { unzip: unzipAfterUpload.value }
      )

      completedBytes += fileSize
      currentPercent.value = 100
    }

    uploadedBytes.value = totalBytes.value
    ElMessage.success('All files uploaded successfully')
    visible.value = false
    await fileStore.loadFiles()
  } catch (error: any) {
    if (error.message !== 'Upload cancelled') {
      ElMessage.error(`Upload failed: ${error.message}`)
    }
    console.error(error)
  } finally {
    uploading.value = false
  }
}

function handleCancel() {
  if (uploading.value) return
  visible.value = false
}

function handleClosed() {
  fileList.value = []
  unzipAfterUpload.value = false
  uploading.value = false
  currentPercent.value = 0
  currentFileIndex.value = 0
  currentFileName.value = ''
}
</script>

<style scoped>
.options {
  margin-top: 16px;
}

.upload-progress {
  margin-top: 20px;
  padding: 16px;
  background: var(--el-fill-color-light);
  border-radius: var(--radius-lg);
}

.progress-file-name {
  margin-bottom: 10px;
  font-size: 14px;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.progress-stats {
  margin-top: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  text-align: center;
}
</style>
