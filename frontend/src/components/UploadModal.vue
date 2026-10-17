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
        <div class="el-upload__tip">
          <template v-if="supportsFolderPick">
            Or
            <el-link
              class="folder-link"
              type="primary"
              :underline="false"
              @click.stop="triggerFolderPicker"
            >choose a folder</el-link>
            <span class="folder-hint">(preserves folder structure)</span>
          </template>
          <template v-else>
            <el-tooltip
              content="Your browser doesn't support folder selection. Use Chrome / Edge / Firefox."
              placement="top"
            >
              <el-link class="folder-link" type="primary" :underline="false" disabled>choose a folder</el-link>
            </el-tooltip>
          </template>
        </div>
      </template>
    </el-upload>

    <!-- 文件夹选择用原生 input（el-upload 不暴露 webkitdirectory），
         选中的文件自带相对路径。 -->
    <input
      ref="folderInput"
      type="file"
      webkitdirectory
      multiple
      class="folder-input-hidden"
      @change="handleFolderChange"
    />

    <div v-if="uploading" class="upload-progress">      <div class="progress-file-name">{{ displayFileName }}</div>
      <el-progress
        :percentage="displayPercent"
        :status="progressStatus"
        :stroke-width="20"
        :text-inside="true"
      />
      <div class="progress-stats">
        {{ displayStatsLine }}
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

// webkitdirectory 仅 Chromium / Firefox 支持，Safari 不支持。
const supportsFolderPick = 'webkitdirectory' in document.createElement('input')
const folderInput = ref<HTMLInputElement | null>(null)
const unzipAfterUpload = ref(false)

const currentFileIndex = ref(0)
const currentFileName = ref('')
const currentPercent = ref(0)
const uploadedBytes = ref(0)
const totalBytes = ref(0)

// 服务端以 NDJSON 逐行推送解压进度。
type UnzipPhase = 'idle' | 'uploading' | 'unzipping' | 'done' | 'error'
const unzipPhase = ref<UnzipPhase>('idle')
const unzipCurrent = ref(0)
const unzipTotal = ref(0)
const unzipFileName = ref('')

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const progressStatus = computed(() => {
  if (unzipPhase.value === 'error') return 'exception'
  if (unzipPhase.value === 'done') return 'success'
  return ''
})

// 解压阶段进度条走文件数（current/total），否则走字节上传百分比。
const displayPercent = computed(() => {
  if (unzipPhase.value === 'unzipping' || unzipPhase.value === 'done') {
    if (unzipTotal.value === 0) return 0
    return Math.round((unzipCurrent.value / unzipTotal.value) * 100)
  }
  return currentPercent.value
})

const displayFileName = computed(() => {
  if (unzipPhase.value === 'unzipping' || unzipPhase.value === 'done') {
    return unzipFileName.value || currentFileName.value
  }
  return currentFileName.value
})

// 分母用展开后的文件数而非 fileList.length：一个文件夹条目可能展开成上百个上传。
const uploadTotalCount = ref(0)
const displayStatsLine = computed(() => {
  if (unzipPhase.value === 'unzipping' || unzipPhase.value === 'done') {
    return `Extracting ${unzipCurrent.value} / ${unzipTotal.value}`
  }
  return `${currentFileIndex.value} / ${uploadTotalCount.value} files · ${formatSize(uploadedBytes.value)} / ${formatSize(totalBytes.value)}`
})

// 每个被选中的顶层文件夹一个条目，携带其原始 File[]。
type FolderEntryData = {
  isFolder: true
  folderName: string
  files: File[]
  totalBytes: number
}

function isFolderEntry(
  entry: UploadUserFile
): entry is UploadUserFile & FolderEntryData {
  return (entry as unknown as Partial<FolderEntryData>).isFolder === true
}

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

// Safari 会忽略 webkitdirectory 而退化成普通文件选择器，因此入口处先判定 supportsFolderPick。
function triggerFolderPicker() {
  if (!supportsFolderPick) return
  folderInput.value?.click()
}

function handleFolderChange(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  if (files.length === 0) return

  // 每个顶层文件夹只列一个条目，避免上百个文件撑爆列表；文件仍挂在条目上供遍历。
  const groups = new Map<string, File[]>()
  for (const f of files) {
    const rel = (f as unknown as { webkitRelativePath?: string }).webkitRelativePath
    if (!rel) continue
    const top = rel.split('/')[0]
    let bucket = groups.get(top)
    if (!bucket) {
      bucket = []
      groups.set(top, bucket)
    }
    bucket.push(f)
  }

  for (const [folderName, group] of groups) {
    const totalBytes = group.reduce((sum, f) => sum + f.size, 0)
    const entry = {
      // 取负并加随机，避免与 el-upload 自己递增的正数 uid 冲突。
      uid: -Date.now() - Math.floor(Math.random() * 1_000_000),
      name:
        `${folderName}  ` +
        `(${group.length} ${group.length === 1 ? 'file' : 'files'} · ${formatSize(totalBytes)})`,
      status: 'ready'
    } as UploadUserFile & FolderEntryData
    entry.isFolder = true
    entry.folderName = folderName
    entry.files = group
    entry.totalBytes = totalBytes
    fileList.value.push(entry)
  }

  // 清空 input，否则再次选同一文件夹不会触发 change。
  input.value = ''
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

  // 把 fileList 展开成逐文件上传序列；进度 UI 用展开后的长度作分母（见 uploadTotalCount）。
  const uploadList: Array<{ file: File; relativePath?: string }> = []
  for (const entry of fileList.value) {
    if (isFolderEntry(entry)) {
      for (const f of entry.files) {
        const rel = (f as unknown as { webkitRelativePath?: string }).webkitRelativePath
        uploadList.push({ file: f, relativePath: rel })
      }
    } else if (entry.raw) {
      uploadList.push({ file: entry.raw })
    }
  }

  if (uploadList.length === 0) {
    ElMessage.warning('Please select files to upload')
    return
  }

  uploading.value = true
  currentFileIndex.value = 0
  uploadedBytes.value = 0
  unzipPhase.value = 'uploading'
  uploadTotalCount.value = uploadList.length
  totalBytes.value = uploadList.reduce((sum, it) => sum + it.file.size, 0)

  let completedBytes = 0

  try {
    for (let i = 0; i < uploadList.length; i++) {
      const it = uploadList[i]
      const file = it.file

      currentFileIndex.value = i + 1
      currentFileName.value = it.relativePath || file.name
      currentPercent.value = 0
      // 必须重置 unzipPhase：残留的 'done' 会让进度条读旧解压计数（0/0）而卡在 0%，像卡死。
      unzipPhase.value = 'uploading'
      unzipFileName.value = ''
      unzipCurrent.value = 0
      unzipTotal.value = 0

      const fileSize = file.size
      // 仅 .zip 走解压流：非 zip 会在服务端解压失败，且进度状态会互相干扰。
      const isZip = file.name.toLowerCase().endsWith('.zip')
      const useUnzip = unzipAfterUpload.value && isZip

      if (useUnzip) {
        // 流式路径：上传字节进度与解压进度共用同一进度条。
        const result = await fileApi.uploadFileWithUnzipProgress(
          fileStore.currentPath,
          file,
          {
            onUploadProgress: (percent: number) => {
              currentPercent.value = percent
              uploadedBytes.value = completedBytes + Math.round((percent / 100) * fileSize)
            },
            onUnzipProgress: (current, total, name) => {
              // 首个解压事件翻转阶段，让显示切换为文件数进度。
              if (unzipPhase.value === 'uploading') {
                unzipPhase.value = 'unzipping'
                currentPercent.value = 100
              }
              unzipCurrent.value = current
              unzipTotal.value = total
              unzipFileName.value = name
            }
          },
          { relativePath: it.relativePath }
        )
        unzipPhase.value = result.success ? 'done' : 'error'
      } else {
        // 传统路径：服务端完成后返回单个 JSON，客户端观测不到解压进度。
        await fileApi.uploadFileWithProgress(
          fileStore.currentPath,
          file,
          (percent: number) => {
            currentPercent.value = percent
            uploadedBytes.value = completedBytes + Math.round((percent / 100) * fileSize)
          },
          { relativePath: it.relativePath }
        )
      }

      completedBytes += fileSize
      currentPercent.value = 100
    }

    uploadedBytes.value = totalBytes.value
    ElMessage.success('All files uploaded successfully')
    visible.value = false
    await fileStore.loadFiles()
  } catch (error: any) {
    unzipPhase.value = 'error'
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
  unzipPhase.value = 'idle'
  unzipCurrent.value = 0
  unzipTotal.value = 0
  unzipFileName.value = ''
  uploadTotalCount.value = 0
}
</script>

<style scoped>
/* 隐藏但仍留在 DOM，ref + click() 才有效；display:none 会让部分浏览器的 file input 失效。 */
.folder-input-hidden {
  position: absolute;
  width: 0;
  height: 0;
  opacity: 0;
  pointer-events: none;
}

/* el-link 自带 inline-flex + vertical-align:middle，会浮在基线之上；强制 baseline 使其与周围文字对齐。 */
:deep(.folder-link) {
  vertical-align: baseline;
}
.folder-hint {
  color: var(--el-text-color-secondary);
  margin-left: 4px;
}

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
