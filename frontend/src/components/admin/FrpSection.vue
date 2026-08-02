<template>
  <div class="frp-section">
    <!-- FRP master switch + status display -->
    <section class="frp-card">
      <div class="card-header">
        <div>
          <h2 class="section-title">FRP 内网穿透</h2>
          <p class="section-hint">
            启用后，可通过 FRP 服务器将本地服务暴露到公网。
          </p>
        </div>
        <el-switch
          :model-value="store.frpStatus?.enabled ?? false"
          :loading="togglingEnabled"
          @change="handleToggleEnabled"
        />
      </div>

      <!-- Status indicator -->
      <div class="status-row" v-if="store.frpStatus?.enabled">
        <span class="status-label">状态:</span>
        <el-tag :type="store.frpStatus?.running ? 'success' : 'danger'" size="small">
          {{ store.frpStatus?.running ? '运行中' : '已停止' }}
        </el-tag>
      </div>
    </section>

    <!-- Configuration form -->
    <section class="frp-card" v-if="store.frpStatus?.enabled">
      <div class="card-header">
        <h2 class="section-title">FRP 配置</h2>
      </div>

      <el-form
        :model="form"
        label-width="120px"
        class="frp-form"
        @submit.prevent="handleSaveConfig"
      >
        <!-- Server settings -->
        <div class="form-group">
          <h3 class="group-title">服务器设置</h3>
          <el-form-item label="服务器地址" required>
            <el-input
              v-model="form.serverAddr"
              placeholder="例如: frp.example.com"
              clearable
            />
          </el-form-item>
          <el-form-item label="服务器端口" required>
            <el-input-number
              v-model="form.serverPort"
              :min="1"
              :max="65535"
              controls-position="right"
            />
          </el-form-item>
          <el-form-item label="认证令牌">
            <el-input
              v-model="form.authToken"
              type="password"
              show-password
              placeholder="FRP 服务器认证令牌"
              clearable
            />
          </el-form-item>
        </div>

        <!-- Proxy settings -->
        <div class="form-group">
          <h3 class="group-title">代理设置</h3>
          <el-form-item label="代理名称">
            <el-input
              v-model="form.proxyName"
              placeholder="gohttpserver"
              clearable
            />
          </el-form-item>
          <el-form-item label="代理类型">
            <el-select v-model="form.proxyType" style="width: 100%">
              <el-option label="TCP" value="tcp" />
              <el-option label="HTTP" value="http" />
              <el-option label="HTTPS" value="https" />
            </el-select>
          </el-form-item>
        </div>

        <!-- Local settings (auto-detected, read-only) -->
        <div class="form-group">
          <h3 class="group-title">本地服务（自动检测）</h3>
          <el-form-item label="本地 IP">
            <el-input
              :model-value="form.localIP"
              readonly
              disabled
            >
              <template #append>
                <el-tooltip content="自动从程序启动配置获取" placement="top">
                  <el-icon><InfoFilled /></el-icon>
                </el-tooltip>
              </template>
            </el-input>
          </el-form-item>
          <el-form-item label="本地端口">
            <el-input
              :model-value="form.localPort"
              readonly
              disabled
            >
              <template #append>
                <el-tooltip content="自动从程序启动配置获取" placement="top">
                  <el-icon><InfoFilled /></el-icon>
                </el-tooltip>
              </template>
            </el-input>
          </el-form-item>
        </div>

        <!-- Remote settings -->
        <div class="form-group">
          <h3 class="group-title">远程端口</h3>
          <el-form-item label="远程端口" required>
            <el-input-number
              v-model="form.remotePort"
              :min="1"
              :max="65535"
              controls-position="right"
            />
          </el-form-item>
        </div>

        <!-- Save button -->
        <div class="form-actions">
          <el-button type="primary" :loading="saving" @click="handleSaveConfig">
            保存配置
          </el-button>
        </div>
      </el-form>
    </section>

    <!-- Connection info -->
    <section class="frp-card" v-if="store.frpStatus?.enabled && store.frpStatus?.running">
      <div class="card-header">
        <h2 class="section-title">连接信息</h2>
      </div>
      <div class="connection-info">
        <div class="info-row">
          <span class="info-label">公网访问地址:</span>
          <code class="info-value">{{ form.serverAddr }}:{{ form.remotePort }}</code>
          <el-button text size="small" @click="copyAddress">
            <el-icon><CopyDocument /></el-icon>
            复制
          </el-button>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument, InfoFilled } from '@element-plus/icons-vue'
import { useAdminStore } from '@/stores/adminStore'
import { copyText } from '@/utils/clipboard'

const store = useAdminStore()
const togglingEnabled = ref(false)
const saving = ref(false)

const form = reactive({
  serverAddr: '',
  serverPort: 7000,
  authToken: '',
  proxyName: 'gohttpserver',
  proxyType: 'tcp',
  localIP: '127.0.0.1',
  localPort: 8000,
  remotePort: 30000,
})

// Load config from store when status changes
watch(
  () => store.frpStatus?.config,
  (config) => {
    if (config) {
      form.serverAddr = config.serverAddr || ''
      form.serverPort = config.serverPort || 7000
      form.authToken = config.authToken || ''
      form.proxyName = config.proxyName || 'gohttpserver'
      form.proxyType = config.proxyType || 'tcp'
      form.localIP = config.localIP || '127.0.0.1'
      form.localPort = config.localPort || 8000
      form.remotePort = config.remotePort || 30000
    }
  },
  { immediate: true, deep: true }
)

async function handleToggleEnabled(target: boolean) {
  togglingEnabled.value = true
  try {
    const res = await store.setFRPEnabled(target)
    if (res.ok) {
      ElMessage.success(target ? 'FRP 已启用' : 'FRP 已禁用')
      // Refresh status to update UI
      await store.loadFRPStatus()
    } else {
      ElMessage.error(res.error || '切换失败')
    }
  } finally {
    togglingEnabled.value = false
  }
}

async function handleSaveConfig() {
  // Validation
  if (!form.serverAddr) {
    ElMessage.error('请输入服务器地址')
    return
  }
  if (!form.serverPort || form.serverPort < 1 || form.serverPort > 65535) {
    ElMessage.error('请输入有效的服务器端口')
    return
  }
  if (!form.localPort || form.localPort < 1 || form.localPort > 65535) {
    ElMessage.error('请输入有效的本地端口')
    return
  }
  if (!form.remotePort || form.remotePort < 1 || form.remotePort > 65535) {
    ElMessage.error('请输入有效的远程端口')
    return
  }

  saving.value = true
  try {
    const res = await store.updateFRPConfig({
      serverAddr: form.serverAddr,
      serverPort: form.serverPort,
      authToken: form.authToken,
      proxyName: form.proxyName,
      proxyType: form.proxyType,
      localIP: form.localIP,
      localPort: form.localPort,
      remotePort: form.remotePort,
    })
    if (res.ok) {
      ElMessage.success('配置已保存')
      // Refresh status to update UI
      await store.loadFRPStatus()
    } else {
      ElMessage.error(res.error || '保存失败')
    }
  } finally {
    saving.value = false
  }
}

async function copyAddress() {
  const address = `${form.serverAddr}:${form.remotePort}`
  if (await copyText(address)) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动选择复制')
  }
}

onMounted(async () => {
  if (!store.frpLoaded) {
    await store.loadFRPStatus()
  }
})
</script>

<style scoped>
.frp-section {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.frp-card {
  background: #f8fafc;
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 10px;
  padding: 18px 20px;
}

.card-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.section-title {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 600;
  color: #0f172a;
}

.section-hint {
  margin: 0;
  font-size: 12px;
  color: #64748b;
  max-width: 520px;
}

.status-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  padding: 10px 12px;
  background: rgba(14, 165, 233, 0.08);
  border-radius: 8px;
}

.status-label {
  font-size: 12px;
  color: #475569;
}

.frp-form {
  margin-top: 16px;
}

.form-group {
  margin-bottom: 24px;
}

.group-title {
  font-size: 13px;
  font-weight: 600;
  color: #334155;
  margin: 0 0 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid rgba(148, 163, 184, 0.2);
}

.form-actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid rgba(148, 163, 184, 0.2);
}

.connection-info {
  margin-top: 12px;
}

.info-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  background: rgba(34, 197, 94, 0.08);
  border-radius: 8px;
  flex-wrap: wrap;
}

.info-label {
  font-size: 12px;
  color: #475569;
}

.info-value {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 13px;
  color: #0f172a;
  background: transparent;
  padding: 0;
  border-radius: 0;
  word-break: break-all;
  min-width: 0;
  flex: 1 1 auto;
}

/* Phone: tighter spacing */
@media (max-width: 640px) {
  .frp-section {
    gap: 14px;
  }
  .frp-card {
    padding: 14px 14px;
  }
  .frp-form :deep(.el-form-item__label) {
    width: 100px !important;
  }
  .info-row {
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
  }
}
</style>
