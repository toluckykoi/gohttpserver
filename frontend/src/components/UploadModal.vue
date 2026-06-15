<template>
  <el-dialog
    v-model="visible"
    title="File Upload"
    width="500px"
    :close-on-click-modal="false"
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

    <div class="options" v-if="fileList.length > 0">
      <el-checkbox v-model="unzipAfterUpload">
        Unzip after upload (for .zip files)
      </el-checkbox>
    </div>

    <template #footer>
      <div class="dialog-footer">
        <el-button @click="visible = false">Cancel</el-button>
        <el-button type="primary" @click="handleUpload" :loading="uploading">
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

interface Props {
  visible: boolean
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
  upload: [file: File, options?: { filename?: string; unzip?: boolean }]
}>()

const uploadRef = ref()
const fileList = ref<UploadUserFile[]>([])
const uploading = ref(false)
const unzipAfterUpload = ref(false)

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
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

async function handleUpload() {
  if (fileList.value.length === 0) {
    ElMessage.warning('Please select files to upload')
    return
  }

  uploading.value = true
  try {
    for (const file of fileList.value) {
      if (file.raw) {
        await emit('upload', file.raw, { unzip: unzipAfterUpload.value })
      }
    }
    ElMessage.success('All files uploaded successfully')
    visible.value = false
  } catch (error) {
    console.error(error)
  } finally {
    uploading.value = false
  }
}

function handleClosed() {
  fileList.value = []
  unzipAfterUpload.value = false
}
</script>

<style scoped>
.options {
  margin-top: 16px;
}
</style>