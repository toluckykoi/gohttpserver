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

    <!-- Hidden native file input for folder picking. Element Plus's
         el-upload is a thin wrapper around <input type="file"> and
         doesn't expose the webkitdirectory attribute, so we keep a
         parallel native input just for the folder-pick flow. The
         picked File objects carry their directory position via
         File.webkitRelativePath (e.g. "MyFolder/sub/foo.txt"), which
         we forward to the server as the `path` form field. -->
    <input
      ref="folderInput"
      type="file"
      webkitdirectory
      multiple
      class="folder-input-hidden"
      @change="handleFolderChange"
    />

    <!-- Upload progress -->
    <div v-if="uploading" class="upload-progress">
      <div class="progress-file-name">{{ displayFileName }}</div>
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

// `webkitdirectory` is supported by Chromium-based browsers and Firefox,
// but not by Safari. Detect once at component setup; the template uses
// this to either render the active picker link or a disabled link with
// a tooltip explaining the limitation.
const supportsFolderPick = 'webkitdirectory' in document.createElement('input')
const folderInput = ref<HTMLInputElement | null>(null)
const unzipAfterUpload = ref(false)

// Progress state
const currentFileIndex = ref(0)
const currentFileName = ref('')
const currentPercent = ref(0)
const uploadedBytes = ref(0)
const totalBytes = ref(0)

// Unzip phase state. Only populated when the user ticked "Unzip after
// upload". `unzipPhase` mirrors the server's progress stream:
//   'uploading' — bytes are flowing into the server
//   'unzipping' — server is extracting the zip; we update current/total/file
//   'done'      — terminal line received, extraction finished
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

// During the unzipping phase the progress bar shows file-count progress
// (current / total). Otherwise it shows byte-upload percent.
const displayPercent = computed(() => {
  if (unzipPhase.value === 'unzipping' || unzipPhase.value === 'done') {
    if (unzipTotal.value === 0) return 0
    return Math.round((unzipCurrent.value / unzipTotal.value) * 100)
  }
  return currentPercent.value
})

// File-name line: file being uploaded, or file currently being extracted.
const displayFileName = computed(() => {
  if (unzipPhase.value === 'unzipping' || unzipPhase.value === 'done') {
    return unzipFileName.value || currentFileName.value
  }
  return currentFileName.value
})

// Stats line: "1 / 1 files · 5.2 MB / 6.9 MB" while uploading, then
// "Extracting 5 / 12" while the server is unpacking. The denominator
// is the flattened file count, not fileList.length, so a folder entry
// showing "1 / 1 files" still ticks correctly while it expands to
// hundreds of underlying uploads.
const uploadTotalCount = ref(0)
const displayStatsLine = computed(() => {
  if (unzipPhase.value === 'unzipping' || unzipPhase.value === 'done') {
    return `Extracting ${unzipCurrent.value} / ${unzipTotal.value}`
  }
  return `${currentFileIndex.value} / ${uploadTotalCount.value} files · ${formatSize(uploadedBytes.value)} / ${formatSize(totalBytes.value)}`
})

// Folder entry extension on top of Element Plus's UploadUserFile.
// The picker produces one of these per top-level folder chosen,
// carrying the raw File[] so the upload loop can iterate them.
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

// Folder-picker plumbing. `webkitdirectory` is only set on the input
// when the browser supports it — Safari ignores the attribute and
// degrades to a regular file picker, so we gate entry on
// `supportsFolderPick` (see template + ref).
function triggerFolderPicker() {
  if (!supportsFolderPick) return
  folderInput.value?.click()
}

function handleFolderChange(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  if (files.length === 0) return

  // Group by top-level folder. webkitRelativePath looks like
  // "MyFolder/sub/foo.txt" — the first segment is the folder the
  // user picked. We push ONE list entry per top-level folder so a
  // folder with hundreds of files doesn't bloat the upload list
  // (the user only needs to see what they're uploading, not every
  // leaf path). The individual files stay attached to the entry
  // for the upload loop to iterate.
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
      // Negative + random so uids never collide with el-upload's own
      // positive monotonic counter.
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

  // Reset the input so picking the same folder again still fires
  // `change`. Without this, browsers treat the second pick as a
  // no-op because the file list reference hasn't changed.
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

  // Flatten fileList into a per-file upload sequence. Single-file
  // entries contribute one pair; folder entries (see handleFolderChange)
  // contribute one pair per underlying file, in the order the picker
  // returned them. The flattened length is what the progress UI shows
  // as the denominator — fileList.length would only count entries,
  // which is misleading when one folder entry hides hundreds of files.
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
      // Reset every per-file piece of state, including the phase.
      // Leaving `unzipPhase` at 'done' from the previous iteration
      // would make the progress display computeds read the stale
      // unzip counters (current/total) instead of currentPercent —
      // and with current/total both 0, the bar would pin at 0% for
      // the entire upload of this file, looking like a hang.
      unzipPhase.value = 'uploading'
      unzipFileName.value = ''
      unzipCurrent.value = 0
      unzipTotal.value = 0

      const fileSize = file.size
      // Only zips go through the unzip streaming path. The checkbox
      // label already says "for .zip files", but the code used to
      // route every file through it, which had two visible
      // consequences: (1) non-zip files hit a server-side unzip
      // failure that the user couldn't act on, and (2) the progress
      // UI mixed zip and non-zip state in confusing ways.
      const isZip = file.name.toLowerCase().endsWith('.zip')
      const useUnzip = unzipAfterUpload.value && isZip

      if (useUnzip) {
        // Streaming path: server returns NDJSON with one line per file
        // during extraction, then a terminal "done" line. The upload
        // bytes progress and the unzip progress share the same modal.
        const result = await fileApi.uploadFileWithUnzipProgress(
          fileStore.currentPath,
          file,
          {
            onUploadProgress: (percent: number) => {
              currentPercent.value = percent
              uploadedBytes.value = completedBytes + Math.round((percent / 100) * fileSize)
            },
            onUnzipProgress: (current, total, name) => {
              // First unzip event flips the phase so the display
              // computeds switch to file-count progress.
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
        // Legacy path: server returns a single JSON object when done.
        // No unzip progress is observable from the client.
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
/* The folder-pick input is hidden but still in the DOM so its
   ref + click() still work. Using display:none (the obvious choice)
   breaks <input type="file"> in some browsers; visibility:hidden
   + absolute positioning keeps it functional and out of layout. */
.folder-input-hidden {
  position: absolute;
  width: 0;
  height: 0;
  opacity: 0;
  pointer-events: none;
}

/* el-link ships with display:inline-flex + vertical-align:middle,
   which floats a few pixels above the baseline of the surrounding
   text. Force baseline alignment so "Or", "choose a folder", and
   "(preserves folder structure)" all sit on the same line. The
   link keeps its primary blue colour from type="primary" — the
   visual distinction is the point. */
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
