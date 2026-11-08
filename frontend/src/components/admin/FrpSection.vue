<template>
  <div class="frp-section">
    <section class="frp-card">
      <div class="card-header">
        <div>
          <h2 class="section-title">{{ t('frp.service') }}</h2>
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

      <div class="status-row" v-if="store.frpStatus?.enabled">
        <span class="status-label">{{ t('frp.status') }}</span>
        <el-tag :type="store.frpStatus?.running ? 'success' : 'danger'" size="small">
          {{ store.frpStatus?.running ? t('frp.running') : t('frp.stopped') }}
        </el-tag>
      </div>
    </section>

    <section class="frp-card" v-if="store.frpStatus?.enabled">
      <div class="card-header">
        <h2 class="section-title">{{ t('frp.configuration') }}</h2>
      </div>

      <el-form
        :model="form"
        label-width="120px"
        class="frp-form"
        @submit.prevent="handleSaveConfig"
      >
        <div class="form-group">
          <h3 class="group-title">{{ t('frp.serverSettings') }}</h3>
          <el-form-item :label="t('frp.serverAddress')" required>
            <el-input
              v-model="form.serverAddr"
              :placeholder="t('frp.serverAddressExample')"
              clearable
            />
          </el-form-item>
          <el-form-item :label="t('frp.serverPort')" required>
            <el-input-number
              v-model="form.serverPort"
              :min="1"
              :max="65535"
              controls-position="right"
            />
          </el-form-item>
          <el-form-item :label="t('frp.authToken')">
            <el-input
              v-model="form.authToken"
              :type="passwordVisible ? 'text' : 'password'"
              :placeholder="authTokenPlaceholder"
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

        <div class="form-group">
          <h3 class="group-title">{{ t('frp.proxySettings') }}</h3>
          <el-form-item :label="t('frp.proxyName')">
            <el-input
              v-model="form.proxyName"
              placeholder="gohttpserver"
              clearable
            />
            <div class="form-item-hint" v-if="store.frpStatus?.config?.registeredProxyName">
              {{ t('frp.registeredProxyHint', { name: store.frpStatus.config.registeredProxyName }) }}
            </div>
          </el-form-item>
          <el-form-item :label="t('frp.proxyType')">
            <el-select v-model="form.proxyType" style="width: 100%">
              <el-option label="TCP" value="tcp" />
              <el-option label="HTTP" value="http" />
              <el-option label="HTTPS" value="https" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('frp.proxyProtocol')">
            <el-select v-model="form.proxyProtocolVersion" style="width: 100%">
              <el-option label="Disabled" value="" />
              <el-option label="v1" value="v1" />
              <el-option label="v2" value="v2" />
            </el-select>
            <div class="form-item-hint">
              {{ t('frp.proxyProtocolHint') }}
            </div>
          </el-form-item>
        </div>

        <div class="form-group">
          <h3 class="group-title">{{ t('frp.localServiceTitle') }}</h3>
          <el-form-item :label="t('frp.localIP')">
            <el-input
              v-model="form.localIP"
              placeholder="127.0.0.1"
            >
              <template #append>
                <el-tooltip :content="t('frp.serverAddressHint')" placement="top">
                  <el-icon><InfoFilled /></el-icon>
                </el-tooltip>
              </template>
            </el-input>
          </el-form-item>
          <el-form-item :label="t('frp.localPort')">
            <el-input-number
              v-model="form.localPort"
              :min="1"
              :max="65535"
              controls-position="right"
            />
          </el-form-item>
        </div>

        <div class="form-group">
          <h3 class="group-title">{{ t('frp.remotePort') }}</h3>
          <el-form-item :label="t('frp.remotePort')" required>
            <el-input-number
              v-model="form.remotePort"
              :min="1"
              :max="65535"
              controls-position="right"
            />
          </el-form-item>
        </div>

        <div class="form-actions">
          <el-button type="primary" :loading="saving" @click="handleSaveConfig">
            {{ t('frp.save') }}
          </el-button>
        </div>
      </el-form>
    </section>

    <section class="frp-card" v-if="store.frpStatus?.enabled && store.frpStatus?.running">
      <div class="card-header">
        <h2 class="section-title">{{ t('frp.connectionInfo') }}</h2>
      </div>
      <div class="connection-info">
        <div class="info-row">
          <span class="info-label">{{ t('frp.publicAddress') }}</span>
          <code class="info-value">{{ form.serverAddr }}:{{ form.remotePort }}</code>
          <el-button text size="small" @click="copyAddress">
            <el-icon><CopyDocument /></el-icon>
            {{ t('common.copy') }}
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
import { useI18n } from 'vue-i18n'
import { useAdminStore } from '@/stores/adminStore'
import { copyText } from '@/utils/clipboard'

const { t } = useI18n()
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

// 服务端的 auth token 只写不回显：把掩码值回填会以 "********" 覆盖真实 token。留空表示保持原值。
const authTokenPlaceholder = computed(() =>
  store.frpStatus?.config?.authTokenSet
    ? t('frp.tokenKeepPlaceholder')
    : t('frp.tokenPlaceholder'),
)

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
      ElMessage.success(target ? t('frp.enabled') : t('frp.disabled'))
      await store.loadFRPStatus()
    } else {
      ElMessage.error(res.error || t('frp.toggleFailed'))
    }
  } finally {
    togglingEnabled.value = false
  }
}

async function handleSaveConfig() {
  if (!form.serverAddr) {
    ElMessage.error(t('frp.serverAddressRequired'))
    return
  }
  if (!form.serverPort || form.serverPort < 1 || form.serverPort > 65535) {
    ElMessage.error(t('frp.serverPortInvalid'))
    return
  }
  if (!form.localPort || form.localPort < 1 || form.localPort > 65535) {
    ElMessage.error(t('frp.localPortInvalid'))
    return
  }
  if (!form.remotePort || form.remotePort < 1 || form.remotePort > 65535) {
    ElMessage.error(t('frp.remotePortInvalid'))
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
      ElMessage.success(t('frp.saved'))
      await store.loadFRPStatus()
    } else {
      ElMessage.error(res.error || t('frp.saveFailed'))
    }
  } finally {
    saving.value = false
  }
}

/** 明文显示开关：字段为空但服务端已存 token 时，先拉取明文再显示。 */
async function handleToggleTokenVisible() {
  if (!passwordVisible.value && !form.authToken && store.frpStatus?.config?.authTokenSet) {
    revealingToken.value = true
    try {
      const res = await store.revealFRPAuthToken()
      if (res.ok) {
        form.authToken = res.data.authToken || ''
        if (!form.authToken) {
          ElMessage.info(t('frp.noToken'))
          return
        }
      } else {
        ElMessage.error(res.error || t('frp.tokenFetchFailed'))
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
    ElMessage.success(t('webdav.copied'))
  } else {
    ElMessage.error(t('frp.copyFailed'))
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
