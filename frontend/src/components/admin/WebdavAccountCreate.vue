<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="(v: boolean) => $emit('update:visible', v)"
    title="新建 WebDAV 账号"
    width="min(520px, 92vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!submitting"
    append-to-body
  >
    <el-form label-position="top" @submit.prevent="handleSubmit">
      <!-- 备注名:必填,仅用于展示,不参与鉴权。 -->
      <el-form-item label="备注名" required>
        <el-input
          v-model="form.remark"
          placeholder="如：备份用 / 手机同步"
          :disabled="submitting"
          required
        />
      </el-form-item>

      <!-- 根目录:可手动输入或点击「浏览」从文件树中选择。
           服务端会校验并拒绝 ".." 等逃逸路径。 -->
      <el-form-item label="相对根目录" required>
        <el-input
          v-model="form.rootPath"
          placeholder="/"
          :disabled="submitting"
        >
          <template #prepend>root</template>
          <template #append>
            <el-button @click="showPicker = true" :disabled="submitting">
              <el-icon><FolderOpened /></el-icon>
              浏览
            </el-button>
          </template>
        </el-input>
        <div class="field-hint">选择共享的目录。账号只能访问此目录及其子目录，留空或 "/" 表示根目录。</div>
      </el-form-item>

      <!-- 高级选项:默认折叠,大多数用户用默认值(读写 + 系统文件保护)即可。 -->
      <el-collapse v-model="advancedOpen" class="advanced">
        <el-collapse-item title="高级选项" name="adv">
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
                  限制该账号可写入的总字节数。挂载客户端（Windows 资源管理器 / Finder /
                  RaiDrive / Cyberduck）会显示「已用 / 可用」空间。留空或 0 表示不限。
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
        </el-collapse-item>
      </el-collapse>

      <div v-if="errorMessage" class="form-error">{{ errorMessage }}</div>
    </el-form>

    <template #footer>
      <el-button :disabled="submitting" @click="$emit('update:visible', false)">
        取消
      </el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">
        创建账号
      </el-button>
    </template>
  </el-dialog>

  <!-- 目录选择器:从文件树中选目录,选好后回填到访问目录输入框。 -->
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
import DirectoryPicker from './DirectoryPicker.vue'

interface Props {
  visible: boolean
}
const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
}>()

const adminStore = useAdminStore()

// 表单状态。用 reactive 而不是分散的 ref,方便一次性重置。
const form = reactive({
  remark: '',
  rootPath: '/',
  readonly: false,
  protectSystemFiles: true,
  // 配额以「数值 + 单位」录入:quotaValue 是显示值(默认单位 GB),
  // 提交时乘以 quotaUnit 换算成字节。0 表示不限。
  quotaValue: 0,
})

const submitting = ref(false)
const errorMessage = ref('')
const advancedOpen = ref<string[]>([])
const showPicker = ref(false)

// 配额单位选择器 (B / KB / MB / GB / TB),默认 GB。
const quotaUnit = ref<number>(1024 ** 3)

async function handleSubmit() {
  errorMessage.value = ''
  if (!form.remark.trim()) {
    errorMessage.value = '请填写备注名。'
    return
  }
  submitting.value = true
  try {
    // 密码由服务端随机生成,并由 store 缓存到本地,创建后可在账号
    // 的「基本信息」里查看。这里不再弹出凭据对话框 —— 直接关闭。
    const res = await adminStore.createWebdavAccount({
      remark: form.remark.trim(),
      root_path: form.rootPath.trim() || '/',
      readonly: form.readonly,
      protect_system_files: form.protectSystemFiles,
      quota_bytes: Math.round(form.quotaValue * quotaUnit.value),
    })
    if (res.ok) {
      ElMessage.success('账号已创建')
      // The new account may point at a folder that already has files;
      // recalc so its quota bar shows real usage instead of 0.
      adminStore.recalculateWebdavUsage()
      emit('update:visible', false)
    } else if (res.error === 'unauthorized') {
      errorMessage.value = '会话已过期，请重新登录。'
    } else {
      errorMessage.value = res.error || '创建失败。'
    }
  } finally {
    submitting.value = false
  }
}

// 每次打开对话框时重置表单。监听 props.visible(不是 ref)才能
// 响应父组件驱动的变化。
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
  /* Let the quota controls drop below the label on narrow dialogs
     instead of getting clipped. */
  flex-wrap: wrap;
}

/* Text block (label + hint) shrinks so the controls always keep room. */
.switch-text {
  flex: 1 1 200px;
  min-width: 0;
}

/* Quota value + unit stay together and never shrink so both are fully
   visible. */
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
