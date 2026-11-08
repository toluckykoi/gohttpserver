<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="(v: boolean) => $emit('update:visible', v)"
    :title="t('webdav.newAccountTitle')"
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
          required
        />
      </el-form-item>

      <!-- 根目录：服务端会校验并拒绝 ".." 等逃逸路径。 -->
      <el-form-item :label="t('webdav.relativeRoot')" required>
        <el-input
          v-model="form.rootPath"
          placeholder="/"
          :disabled="submitting"
        >
          <template #prepend>root</template>
          <template #append>
            <el-button @click="showPicker = true" :disabled="submitting">
              <el-icon><FolderOpened /></el-icon>
              {{ t('webdav.browse') }}
            </el-button>
          </template>
        </el-input>
        <div class="field-hint">{{ t('webdav.relativeRootHint') }}</div>
      </el-form-item>

      <el-collapse v-model="advancedOpen" class="advanced">
        <el-collapse-item :title="t('common.advanced')" name="adv">
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
                <div class="switch-hint">{{ t('webdav.quotaHintLong') }}</div>
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
        </el-collapse-item>
      </el-collapse>

      <div v-if="errorMessage" class="form-error">{{ errorMessage }}</div>
    </el-form>

    <template #footer>
      <el-button :disabled="submitting" @click="$emit('update:visible', false)">
        {{ t('common.cancel') }}
      </el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">
        {{ t('webdav.createAccount') }}
      </el-button>
    </template>
  </el-dialog>

  <DirectoryPicker
    v-model="form.rootPath"
    v-model:visible="showPicker"
  />
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { FolderOpened } from '@element-plus/icons-vue'
import { useAdminStore } from '@/stores/adminStore'
import { useI18n } from 'vue-i18n'
import DirectoryPicker from './DirectoryPicker.vue'

const { t } = useI18n()

interface Props {
  visible: boolean
}
const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
}>()

const adminStore = useAdminStore()

const form = reactive({
  remark: '',
  rootPath: '/',
  readonly: false,
  protectSystemFiles: true,
  quotaValue: 0,
})

const submitting = ref(false)
const errorMessage = ref('')
const advancedOpen = ref<string[]>([])
const showPicker = ref(false)

const quotaUnit = ref<number>(1024 ** 3)

async function handleSubmit() {
  errorMessage.value = ''
  if (!form.remark.trim()) {
    errorMessage.value = t('webdav.remarkRequired')
    return
  }
  submitting.value = true
  try {
    // 密码由服务端生成，store 会缓存以便稍后查看。
    const res = await adminStore.createWebdavAccount({
      remark: form.remark.trim(),
      root_path: form.rootPath.trim() || '/',
      readonly: form.readonly,
      protect_system_files: form.protectSystemFiles,
      quota_bytes: Math.round(form.quotaValue * quotaUnit.value),
    })
    if (res.ok) {
      ElMessage.success(t('webdav.accountCreated'))
      adminStore.recalculateWebdavUsage()
      emit('update:visible', false)
    } else if (res.error === 'unauthorized') {
      errorMessage.value = t('login.sessionExpired')
    } else {
      errorMessage.value = res.error || t('webdav.createFailed')
    }
  } finally {
    submitting.value = false
  }
}

watch(
  () => props.visible,
  (open) => {
    if (open) {
      form.remark = ''
      form.rootPath = '/'
      form.readonly = false
      form.protectSystemFiles = true
      form.quotaValue = 0
      quotaUnit.value = 1024 ** 3
      errorMessage.value = ''
      advancedOpen.value = []
    }
  },
)
</script>

<style scoped>
.field-hint {
  font-size: 11px;
  color: #64748b;
  margin-top: 4px;
  line-height: 1.4;
}

.advanced {
  margin-top: 8px;
  border-top: 1px solid rgba(148, 163, 184, 0.25);
}

.advanced :deep(.el-collapse-item__header) {
  font-size: 13px;
  font-weight: 500;
  color: #475569;
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
