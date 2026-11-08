<template>
  <el-dialog
    v-model="visible"
    :title="t('urlDownload.title')"
    width="500px"
    :close-on-click-modal="!downloading"
    :close-on-press-escape="!downloading"
    @closed="handleClosed"
  >
    <p class="url-modal-hint">
      {{ t('urlDownload.hint') }}
    </p>

    <el-form
      label-position="top"
      @submit.prevent="handleSubmit"
    >
      <el-form-item :label="t('urlDownload.urlLabel')" required>
        <el-input
          v-model="url"
          :placeholder="t('urlDownload.urlPlaceholder')"
          :disabled="downloading"
          clearable
        />
      </el-form-item>

      <el-form-item :label="t('urlDownload.saveAs')" required>
        <el-input
          v-model="filename"
          :placeholder="t('urlDownload.saveAsPlaceholder')"
          :disabled="downloading"
          clearable
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <div class="dialog-footer">
        <el-button @click="handleCancel" :disabled="downloading">
          {{ t('common.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="downloading"
          :disabled="!canSubmit"
          @click="handleSubmit"
        >
          {{ t('urlDownload.fetch') }}
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useFileApi } from '@/composables/useFileApi'

const { t } = useI18n()

interface Props {
  visible: boolean
  currentPath: string
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
  'fetched': []
}>()

const fileApi = useFileApi()

const url = ref('')
const filename = ref('')
const downloading = ref(false)

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const canSubmit = computed(() => {
  if (downloading.value) return false
  const u = url.value.trim()
  const f = filename.value.trim()
  if (!u || !f) return false
  return /^https?:\/\//i.test(u)
})

async function handleSubmit() {
  if (!canSubmit.value || downloading.value) return
  downloading.value = true
  try {
    const result = await fileApi.fetchFromUrl(
      props.currentPath,
      url.value.trim(),
      filename.value.trim()
    )
    if (result.success) {
      ElMessage.success(
        t('urlDownload.fetched', {
          size: result.size ?? '?',
          name: result.destination ?? filename.value
        })
      )
      emit('fetched')
      visible.value = false
    } else {
      ElMessage.error(t('urlDownload.fetchFailed'))
    }
  } catch (err: any) {
    ElMessage.error(
      t('urlDownload.fetchFailedDetail', {
        msg: err?.message ?? t('urlDownload.unknownError')
      })
    )
  } finally {
    downloading.value = false
  }
}

function handleCancel() {
  if (downloading.value) return
  visible.value = false
}

function handleClosed() {
  url.value = ''
  filename.value = ''
  downloading.value = false
}
</script>

<style scoped>
.url-modal-hint {
  margin: 0 0 16px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}
</style>