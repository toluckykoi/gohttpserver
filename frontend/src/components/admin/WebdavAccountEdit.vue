<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="(v: boolean) => $emit('update:visible', v)"
    :title="t('webdav.editAccountTitle')"
    width="min(520px, 92vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!submitting"
    append-to-body
  >
    <el-form label-position="top" @submit.prevent="handleSubmit">
      <el-form-item :label="t('webdav.remark')" required>
        <el-input
          v-model="form.remark"
          :placeholder="t('webdav.remarkPlaceholder')"
          :disabled="submitting"
        />
      </el-form-item>

      <el-form-item :label="t('webdav.directory')" required>
        <el-input v-model="form.rootPath" placeholder="/" :disabled="submitting">
          <template #prepend>root</template>
          <template #append>
            <el-button @click="showPicker = true" :disabled="submitting">
              <el-icon><FolderOpened /></el-icon>
              {{ t('webdav.browse') }}
            </el-button>
          </template>
        </el-input>
        <div class="field-hint">{{ t('webdav.directoryHint') }}</div>
      </el-form-item>

      <el-form-item :label="t('webdav.username')">
        <el-input :model-value="account?.username" disabled />
        <div class="field-hint">{{ t('about.usernameBound') }}</div>
      </el-form-item>

      <el-form-item>
        <div class="switch-row">
          <div>
            <div class="switch-label">{{ t('about.readOnlyMode') }}</div>
            <div class="switch-hint">{{ t('webdav.readonlyDesc') }}</div>
          </div>
          <el-switch v-model="form.readonly" :disabled="submitting" />
        </div>
      </el-form-item>

      <el-form-item>
        <div class="switch-row">
          <div>
            <div class="switch-label">{{ t('webdav.systemProtection') }}</div>
            <div class="switch-hint">
              {{ t('webdav.systemProtectDesc') }}
            </div>
          </div>
          <el-switch v-model="form.protectSystemFiles" :disabled="submitting" />
        </div>
      </el-form-item>

      <el-form-item>
        <div class="switch-row">
          <div class="switch-text">
            <div class="switch-label">{{ t('webdav.quota') }}</div>
            <div class="switch-hint">
              {{ t('webdav.quotaHint') }}
            </div>
          </div>
          <div class="quota-controls">
            <el-input-number
              v-model="form.quotaValue"
              :min="0"
              :disabled="submitting"
              :placeholder="t('common.unlimited')"
              controls-position="right"
              class="quota-input"
            />
            <el-select
              v-model="quotaUnit"
              :disabled="submitting"
              class="quota-unit"
            >
              <el-option label="B" :value="1" />
              <el-option label="KB" :value="1024" />
              <el-option label="MB" :value="1024 ** 2" />
              <el-option label="GB" :value="1024 ** 3" />
              <el-option label="TB" :value="1024 ** 4" />
            </el-select>
          </div>
        </div>
      </el-form-item>

      <div v-if="errorMessage" class="form-error">{{ errorMessage }}</div>
    </el-form>

    <template #footer>
      <el-button :disabled="submitting" @click="$emit('update:visible', false)">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">{{ t('common.save') }}</el-button>
    </template>

    <DirectoryPicker v-model="form.rootPath" v-model:visible="showPicker" />
  </el-dialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { FolderOpened } from '@element-plus/icons-vue'
import { useAdminStore } from '@/stores/adminStore'
import type { WebdavAccount } from '@/types'
import { useI18n } from 'vue-i18n'
import DirectoryPicker from './DirectoryPicker.vue'

const { t } = useI18n()

interface Props {
  visible: boolean
  account: WebdavAccount | null
}
const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
}>()

const store = useAdminStore()

const form = reactive({
  remark: '',
  rootPath: '/',
  readonly: false,
  protectSystemFiles: true,
  // 配额以「数值 + 单位」编辑，提交时换算成字节。
  quotaValue: 0,
})
const quotaUnit = ref<number>(1024 ** 3)
const submitting = ref(false)
const errorMessage = ref('')
const showPicker = ref(false)

// 选能让数值 >= 1 的最大单位（例如显示 5 GB 而不是 5368709120 B）。
function splitQuota(bytes: number): { value: number; unit: number } {
  if (!bytes || bytes <= 0) return { value: 0, unit: 1024 ** 3 }
  const units = [1024 ** 4, 1024 ** 3, 1024 ** 2, 1024, 1]
  for (const u of units) {
    if (bytes >= u) return { value: Math.round((bytes / u) * 100) / 100, unit: u }
  }
  return { value: bytes, unit: 1 }
}

watch(
  () => [props.visible, props.account] as const,
  ([open]) => {
    if (open && props.account) {
      form.remark = props.account.remark
      form.rootPath = props.account.root_path || '/'
      form.readonly = props.account.readonly
      form.protectSystemFiles = props.account.protect_system_files
      const q = splitQuota(props.account.quota_bytes)
      form.quotaValue = q.value
      quotaUnit.value = q.unit
      errorMessage.value = ''
    }
  },
  { immediate: true },
)

async function handleSubmit() {
  errorMessage.value = ''
  if (!props.account) return
  if (!form.remark.trim()) {
    errorMessage.value = t('webdav.remarkRequired')
    return
  }
  submitting.value = true
  try {
    const res = await store.updateWebdavAccount(props.account.id, {
      remark: form.remark.trim(),
      root_path: form.rootPath.trim() || '/',
      readonly: form.readonly,
      protect_system_files: form.protectSystemFiles,
      quota_bytes: Math.round(form.quotaValue * quotaUnit.value),
    })
    if (res.ok) {
      ElMessage.success(t('webdav.accountUpdated'))
      store.recalculateWebdavUsage()
      emit('update:visible', false)
    } else {
      errorMessage.value = res.error || t('webdav.updateFailed')
    }
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.field-hint {
  font-size: 11px;
  color: #64748b;
  margin-top: 4px;
  line-height: 1.4;
}

.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  flex-wrap: wrap;
}

.switch-text {
  flex: 1 1 200px;
  min-width: 0;
}

.quota-controls {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.quota-input {
  width: 150px;
}

.quota-unit {
  width: 82px;
}

.switch-label {
  font-size: 13px;
  font-weight: 500;
  color: #0f172a;
}

.switch-hint {
  font-size: 11px;
  color: #64748b;
  margin-top: 2px;
  max-width: 100%;
  line-height: 1.4;
}

.switch-hint code {
  background: rgba(148, 163, 184, 0.2);
  padding: 0 4px;
  border-radius: 3px;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
}

.form-error {
  margin: 8px 0 0;
  padding: 8px 12px;
  background: rgba(239, 68, 68, 0.12);
  border: 1px solid rgba(239, 68, 68, 0.35);
  color: #b91c1c;
  border-radius: 8px;
  font-size: 13px;
}
</style>
