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
      <p>{{ t('fileInfo.loading') }}</p>
    </div>

    <div v-else class="file-info">
      <el-descriptions :column="1" border>
        <el-descriptions-item :label="t('fileInfo.name')">
          {{ fileInfo?.name || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('fileInfo.type')">
          {{ fileInfo?.type || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('fileInfo.size')">
          {{ formatSize }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('fileInfo.path')">
          {{ fileInfo?.path || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('fileInfo.modified')">
          {{ formatMtime }}
        </el-descriptions-item>
        <el-descriptions-item v-if="fileInfo?.md5" label="MD5">
          <span class="hash-value" :title="fileInfo.md5">{{ fileInfo.md5 }}</span>
          <el-button
            class="hash-copy"
            text
            type="primary"
            :icon="CopyDocument"
            @click="copyToClipboard(fileInfo.md5, 'MD5')"
          />
        </el-descriptions-item>
        <el-descriptions-item v-if="fileInfo?.sha256" label="SHA-256">
          <span class="hash-value" :title="fileInfo.sha256">{{ fileInfo.sha256 }}</span>
          <el-button
            class="hash-copy"
            text
            type="primary"
            :icon="CopyDocument"
            @click="copyToClipboard(fileInfo.sha256, 'SHA-256')"
          />
        </el-descriptions-item>
      </el-descriptions>

      <template v-if="fileInfo?.extra">
        <el-divider content-position="left">{{ t('fileInfo.extraInfo') }}</el-divider>
        <el-card class="extra-info">
          <pre>{{ JSON.stringify(fileInfo.extra, null, 2) }}</pre>
        </el-card>
      </template>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { FileItem, FileInfo as FileInfoType } from '@/types'
import { useFileApi } from '@/composables/useFileApi'
import { formatBytes } from '@/utils/formatBytes'
import { Loading, CopyDocument } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { copyText } from '@/utils/clipboard'
import dayjs from 'dayjs'

const { t } = useI18n()

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
  if (!props.file) return t('fileInfo.title')
  return t('fileInfo.info', { name: props.file.name })
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

async function copyToClipboard(text: string, label: string) {
  try {
    await copyText(text)
    ElMessage.success(t('messages.copiedToClipboard', { label }))
  } catch (error) {
    ElMessage.error(t('messages.copyFailed', { label }))
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

.hash-value {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
  margin-right: 4px;
}
.hash-copy {
  vertical-align: middle;
}
</style>
