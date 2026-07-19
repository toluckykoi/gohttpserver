<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="(v: boolean) => $emit('update:visible', v)"
    title="编辑 WebDAV 账号"
    width="min(520px, 92vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!submitting"
    append-to-body
  >
    <el-form label-position="top" @submit.prevent="handleSubmit">
      <el-form-item label="备注名" required>
        <el-input
          v-model="form.remark"
          placeholder="如：备份用 / 手机同步"
          :disabled="submitting"
        />
      </el-form-item>

      <el-form-item label="访问目录" required>
        <el-input v-model="form.rootPath" placeholder="/" :disabled="submitting">
          <template #prepend>root</template>
          <template #append>
            <el-button @click="showPicker = true" :disabled="submitting">
              <el-icon><FolderOpened /></el-icon>
              浏览
            </el-button>
          </template>
        </el-input>
        <div class="field-hint">账号只能访问此目录及其子目录。留空或 "/" 表示根目录。</div>
      </el-form-item>

      <el-form-item label="用户名">
        <el-input :model-value="account?.username" disabled />
        <div class="field-hint">用户名与登录用户名绑定，不可修改。</div>
      </el-form-item>

      <el-form-item>
        <div class="switch-row">
          <div>
            <div class="switch-label">只读模式</div>
            <div class="switch-hint">用户只能通过此账号读取文件；</div>
          </div>
          <el-switch v-model="form.readonly" :disabled="submitting" />
        </div>
      </el-form-item>

      <el-form-item>
        <div class="switch-row">
          <div>
            <div class="switch-label">阻止删除/上传系统文件</div>
            <div class="switch-hint">
              开启后，以 <code>.</code> 开头的文件会被阻止上传；
            </div>
          </div>
          <el-switch v-model="form.protectSystemFiles" :disabled="submitting" />
        </div>
      </el-form-item>

      <el-form-item>
        <div class="switch-row">
          <div class="switch-text">
            <div class="switch-label">配额 (Quota)</div>
            <div class="switch-hint">
              限制该账号可写入的总字节数。留空或 0 表示不限。
            </div>
          </div>
          <div class="quota-controls">
            <el-input-number
              v-model="form.quotaValue"
              :min="0"
              :disabled="submitting"
              placeholder="不限"
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
      <el-button :disabled="submitting" @click="$emit('update:visible', false)">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">保存</el-button>
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
import DirectoryPicker from './DirectoryPicker.vue'

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
  // Quota is edited as a value + unit pair; combined into bytes on submit.
  quotaValue: 0,
})
const quotaUnit = ref<number>(1024 ** 3)
const submitting = ref(false)
const errorMessage = ref('')
const showPicker = ref(false)

// Pick the largest unit that yields a value >= 1 so the operator sees a
// readable number (e.g. 5 GB instead of 5368709120 B). 0 stays 0 (unlimited).
function splitQuota(bytes: number): { value: number; unit: number } {
  if (!bytes || bytes <= 0) return { value: 0, unit: 1024 ** 3 }
  const units = [1024 ** 4, 1024 ** 3, 1024 ** 2, 1024, 1]
  for (const u of units) {
    if (bytes >= u) return { value: Math.round((bytes / u) * 100) / 100, unit: u }
  }
  return { value: bytes, unit: 1 }
}

// Re-seed the form each time the dialog opens for a given account.
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
    errorMessage.value = '请填写备注名。'
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
      ElMessage.success('账号已更新')
      // root_path may have changed, which changes what counts toward the
      // quota — recalc so the usage bar reflects the new scope.
      store.recalculateWebdavUsage()
      emit('update:visible', false)
    } else {
      errorMessage.value = res.error || '更新失败。'
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
