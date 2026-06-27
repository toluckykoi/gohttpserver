<template>
  <el-dialog
    v-model="visible"
    title="Download from URL"
    width="500px"
    :close-on-click-modal="!downloading"
    :close-on-press-escape="!downloading"
    @closed="handleClosed"
  >
    <p class="url-modal-hint">
      Fetch a remote file to the current directory. The server blocks
      private/loopback URLs (SSRF protection).
    </p>

    <el-form
      label-position="top"
      @submit.prevent="handleSubmit"
    >
      <el-form-item label="URL" required>
        <el-input
          v-model="url"
          placeholder="https://example.com/file.zip"
          :disabled="downloading"
          clearable
        />
      </el-form-item>

      <el-form-item label="Save as" required>
        <el-input
          v-model="filename"
          placeholder="file.zip"
          :disabled="downloading"
          clearable
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <div class="dialog-footer">
        <el-button @click="handleCancel" :disabled="downloading">
          Cancel
        </el-button>
        <el-button
          type="primary"
          :loading="downloading"
          :disabled="!canSubmit"
          @click="handleSubmit"
        >
          Fetch
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { useFileApi } from '@/composables/useFileApi'

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

// Disable submit until both fields look plausible. We do the strict
// URL/filename validation server-side; this is just a "don't let the
// user click Fetch on an empty form" guard.
const canSubmit = computed(() => {
  if (downloading.value) return false
  const u = url.value.trim()
  const f = filename.value.trim()
  if (!u || !f) return false
  // Same scheme allowlist the server enforces, so the user gets
  // immediate feedback for typos like "example.com/file" instead
  // of waiting for a server-side 400.
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
        `Fetched ${result.size ?? '?'} bytes → ${result.destination ?? filename.value}`
      )
      emit('fetched')
      visible.value = false
    } else {
      ElMessage.error('Fetch failed')
    }
  } catch (err: any) {
    ElMessage.error(`Fetch failed: ${err?.message ?? 'unknown error'}`)
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