<template>
  <el-dialog
    v-model="visible"
    :title="t('newFolder.title')"
    width="400px"
    :close-on-click-modal="!creating"
    :close-on-press-escape="!creating"
    @opened="handleOpened"
    @closed="handleClosed"
  >
    <el-form
      label-position="top"
      @submit.prevent="handleCreate"
    >
      <el-form-item :label="t('newFolder.nameLabel')" required>
        <el-input
          ref="inputRef"
          v-model="name"
          :placeholder="t('newFolder.namePlaceholder')"
          :disabled="creating"
          clearable
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <div class="dialog-footer">
        <el-button @click="handleCancel" :disabled="creating">
          {{ t('common.cancel') }}
        </el-button>
        <el-button
          type="primary"
          :loading="creating"
          :disabled="!name"
          @click="handleCreate"
        >
          {{ t('newFolder.create') }}
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useFileStore } from '@/stores/fileStore'
import { checkPathNameLegal } from '@/utils/path'

const { t } = useI18n()

const props = defineProps<{
  visible: boolean
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  created: []
}>()

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const fileStore = useFileStore()
const inputRef = ref<HTMLInputElement>()

const name = ref('')
const creating = ref(false)

function handleOpened() {
  name.value = ''
  creating.value = false
  nextTick(() => {
    inputRef.value?.focus?.()
  })
}

function handleClosed() {
  name.value = ''
  creating.value = false
}

async function handleCreate() {
  const inputName = name.value.trim()
  if (!inputName) return

  if (!checkPathNameLegal(inputName)) {
    ElMessage.warning(t('newFolder.illegalName'))
    return
  }

  creating.value = true
  try {
    await fileStore.createDirectory(inputName)
    visible.value = false
    emit('created')
  } catch {
    creating.value = false
  }
}

function handleCancel() {
  if (creating.value) return
  visible.value = false
}
</script>
