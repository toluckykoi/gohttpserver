<template>
  <div class="frp-section">
    <!-- FRP master switch + status display -->
    <section class="frp-card">
      <div class="card-header">
        <div>
          <h2 class="section-title">FRP NAT Traversal</h2>
          <p class="section-hint">
            When enabled, local services can be exposed to the public internet via an FRP server.
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
        <span class="status-label">Status:</span>
        <el-tag :type="store.frpStatus?.running ? 'success' : 'danger'" size="small">
          {{ store.frpStatus?.running ? 'Running' : 'Stopped' }}
        </el-tag>
      </div>
    </section>

    <!-- Configuration form -->
    <section class="frp-card" v-if="store.frpStatus?.enabled">
      <div class="card-header">
        <h2 class="section-title">FRP Configuration</h2>
      </div>

      <el-form
        :model="form"
        label-width="120px"
        class="frp-form"
        @submit.prevent="handleSaveConfig"
      >
        <!-- Server settings -->
        <div class="form-group">
          <h3 class="group-title">Server Settings</h3>
          <el-form-item label="Server Address" required>
            <el-input
              v-model="form.serverAddr"
              placeholder="e.g., frp.example.com"
              clearable
            />
          </el-form-item>
          <el-form-item label="Server Port" required>
            <el-input-number
              v-model="form.serverPort"
              :min="1"
              :max="65535"
              controls-position="right"
            />
          </el-form-item>
          <el-form-item label="Auth Token">
            <el-input
              v-model="form.authToken"
              :type="passwordVisible ? 'text' : 'password'"
              :placeholder="authTokenPlaceholder"
              clearable
            >
              <template #suffix>
                <el-icon class="token-eye" @click="handleToggleTokenVisible">
                  <Loading v-if="revealingToken" />
                  <View v-else-if="passwordVisible" />
                  <Hide v-else />
                </el-icon>
              </template>
            </el-input>
          </el-form-item>
        </div>

        <!-- Proxy settings -->
        <div class="form-group">
          <h3 class="group-title">Proxy Settings</h3>
          <el-form-item label="Proxy Name">
            <el-input
              v-model="form.proxyName"
              placeholder="gohttpserver"
              clearable
            />
            <div class="form-item-hint" v-if="store.frpStatus?.config?.registeredProxyName">
              Actual name registered on the FRP server: <code>{{ store.frpStatus.config.registeredProxyName }}</code> (a 3-char random suffix is appended per process to keep proxy names unique across instances)
            </div>
          </el-form-item>
          <el-form-item label="Proxy Type">
            <el-select v-model="form.proxyType" style="width: 100%">
              <el-option label="TCP" value="tcp" />
              <el-option label="HTTP" value="http" />
              <el-option label="HTTPS" value="https" />
            </el-select>
          </el-form-item>
          <el-form-item label="Proxy Protocol">
            <el-select v-model="form.proxyProtocolVersion" style="width: 100%">
              <el-option label="Disabled" value="" />
              <el-option label="v1" value="v1" />
              <el-option label="v2" value="v2" />
            </el-select>
            <div class="form-item-hint">
              Enable Proxy Protocol to pass real client IP through FRP. Requires FRP server to support Proxy Protocol.
            </div>
          </el-form-item>
        </div>

        <!-- Local settings (auto-detected, read-only) -->
        <div class="form-group">
          <h3 class="group-title">Local Service (Auto-detected)</h3>
          <el-form-item label="Local IP">
            <el-input
              :model-value="form.localIP"
              readonly
              disabled
            >
              <template #append>
                <el-tooltip content="Automatically obtained from program startup configuration" placement="top">
                  <el-icon><InfoFilled /></el-icon>
                </el-tooltip>
              </template>
            </el-input>
          </el-form-item>
          <el-form-item label="Local Port">
            <el-input
              :model-value="form.localPort"
              readonly
              disabled
            >
              <template #append>
                <el-tooltip content="Automatically obtained from program startup configuration" placement="top">
                  <el-icon><InfoFilled /></el-icon>
                </el-tooltip>
              </template>
            </el-input>
          </el-form-item>
        </div>

        <!-- Remote settings -->
        <div class="form-group">
          <h3 class="group-title">Remote Port</h3>
          <el-form-item label="Remote Port" required>
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
            Save Configuration
          </el-button>
        </div>
      </el-form>
    </section>

    <!-- Connection info -->
    <section class="frp-card" v-if="store.frpStatus?.enabled && store.frpStatus?.running">
      <div class="card-header">
        <h2 class="section-title">Connection Info</h2>
      </div>
      <div class="connection-info">
        <div class="info-row">
          <span class="info-label">Public Access Address:</span>
          <code class="info-value">{{ form.serverAddr }}:{{ form.remotePort }}</code>
          <el-button text size="small" @click="copyAddress">
            <el-icon><CopyDocument /></el-icon>
            Copy
          </el-button>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument, InfoFilled, View, Hide, Loading } from '@element-plus/icons-vue'
import { useAdminStore } from '@/stores/adminStore'
import { copyText } from '@/utils/clipboard'

const store = useAdminStore()
const togglingEnabled = ref(false)
const saving = ref(false)
const revealingToken = ref(false)
const passwordVisible = ref(false)

const form = reactive({
  serverAddr: '',
  serverPort: 7000,
  authToken: '',
  proxyName: 'gohttpserver',
  proxyType: 'tcp',
  localIP: '127.0.0.1',
  localPort: 8000,
  remotePort: 30000,
  proxyProtocolVersion: '',
})

// The status API treats the auth token as write-only: it returns an
// empty authToken plus an authTokenSet flag. Never echo a value back
// into the password field — that's what caused a masked "********" to
// be re-saved as the real token and break FRP authentication after a
// restart. The placeholder tells the operator whether a token is
// already stored so they know to leave the field blank to keep it.
const authTokenPlaceholder = computed(() =>
  store.frpStatus?.config?.authTokenSet
    ? 'Token configured — leave blank to keep, or enter a new one'
    : 'FRP server authentication token',
)

// Load config from store when status changes
watch(
  () => store.frpStatus?.config,
  (config) => {
    if (config) {
      form.serverAddr = config.serverAddr || ''
      form.serverPort = config.serverPort || 7000
      form.authToken = ''
      form.proxyName = config.proxyName || 'gohttpserver'
      form.proxyType = config.proxyType || 'tcp'
      form.localIP = config.localIP || '127.0.0.1'
      form.localPort = config.localPort || 8000
      form.remotePort = config.remotePort || 30000
      form.proxyProtocolVersion = config.proxyProtocolVersion || ''
    }
  },
  { immediate: true, deep: true }
)

async function handleToggleEnabled(target: boolean) {
  togglingEnabled.value = true
  try {
    const res = await store.setFRPEnabled(target)
    if (res.ok) {
      ElMessage.success(target ? 'FRP enabled' : 'FRP disabled')
      // Refresh status to update UI
      await store.loadFRPStatus()
    } else {
      ElMessage.error(res.error || 'Toggle failed')
    }
  } finally {
    togglingEnabled.value = false
  }
}

async function handleSaveConfig() {
  // Validation
  if (!form.serverAddr) {
    ElMessage.error('Please enter server address')
    return
  }
  if (!form.serverPort || form.serverPort < 1 || form.serverPort > 65535) {
    ElMessage.error('Please enter a valid server port')
    return
  }
  if (!form.localPort || form.localPort < 1 || form.localPort > 65535) {
    ElMessage.error('Please enter a valid local port')
    return
  }
  if (!form.remotePort || form.remotePort < 1 || form.remotePort > 65535) {
    ElMessage.error('Please enter a valid remote port')
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
      proxyProtocolVersion: form.proxyProtocolVersion,
    })
    if (res.ok) {
      ElMessage.success('Configuration saved')
      // Refresh status to update UI
      await store.loadFRPStatus()
    } else {
      ElMessage.error(res.error || 'Save failed')
    }
  } finally {
    saving.value = false
  }
}

/** Eye-toggle for the auth token. When revealing an empty field that
 *  has a token stored server-side, pull the plaintext down first so
 *  the eye actually shows the saved secret — not a blank box. If the
 *  operator is typing a new token, the eye just toggles visibility. */
async function handleToggleTokenVisible() {
  // About to reveal + field empty + a token is stored -> fetch it.
  if (!passwordVisible.value && !form.authToken && store.frpStatus?.config?.authTokenSet) {
    revealingToken.value = true
    try {
      const res = await store.revealFRPAuthToken()
      if (res.ok) {
        form.authToken = res.data.authToken || ''
        if (!form.authToken) {
          ElMessage.info('当前未配置 token')
          return
        }
      } else {
        ElMessage.error(res.error || '获取 token 失败')
        return
      }
    } finally {
      revealingToken.value = false
    }
  }
  passwordVisible.value = !passwordVisible.value
}

async function copyAddress() {
  const address = `${form.serverAddr}:${form.remotePort}`
  if (await copyText(address)) {
    ElMessage.success('Copied to clipboard')
  } else {
    ElMessage.error('Copy failed, please select and copy manually')
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

.token-eye {
  cursor: pointer;
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
