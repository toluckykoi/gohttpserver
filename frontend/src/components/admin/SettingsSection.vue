<template>
  <div class="settings-section">
    <!-- WebDAV 总开关：关闭后保留账号，但所有请求返回 503。 -->
    <section class="settings-card">
      <div class="card-header">
        <div>
          <h2 class="section-title">{{ t('webdav.service') }}</h2>
          <p class="section-hint">
            {{ t('webdav.serviceHint') }}
          </p>
        </div>
        <el-switch
          :model-value="store.webdavEnabled"
          :loading="togglingEnabled"
          @change="handleToggleEnabled"
        />
      </div>
      <div class="webdav-url-row" v-if="store.webdavEnabled">
        <span class="webdav-url-label">{{ t('webdav.url') }}:</span>
        <code class="webdav-url-value">{{ webdavFullUrl }}</code>
        <el-button text size="small" @click="copyUrl">
          <el-icon><CopyDocument /></el-icon>
          {{ t('common.copy') }}
        </el-button>
      </div>
    </section>

    <section class="settings-card" v-if="store.webdavEnabled">
      <div class="card-header">
        <h2 class="section-title">{{ t('webdav.accounts') }}</h2>
        <el-button class="new-account-btn" @click="showCreate = true">
          <el-icon class="new-account-icon"><Plus /></el-icon>
          <span>{{ t('webdav.newAccount') }}</span>
        </el-button>
      </div>
      <WebdavAccountList v-if="store.webdavLoaded" />
      <el-skeleton v-else :rows="4" animated />
    </section>

    <!-- 按需挂载，避免弹窗在未打开时就初始化。 -->
    <WebdavAccountCreate
      v-if="showCreate"
      v-model:visible="showCreate"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument, Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useAdminStore } from '@/stores/adminStore'
import { copyText } from '@/utils/clipboard'
import WebdavAccountList from './WebdavAccountList.vue'
import WebdavAccountCreate from './WebdavAccountCreate.vue'

const { t } = useI18n()
const store = useAdminStore()
const togglingEnabled = ref(false)
const showCreate = ref(false)

const webdavFullUrl = computed(() => {
  const origin = window.location.origin
  return `${origin}${store.webdavUrl}`
})

async function handleToggleEnabled(target: boolean) {
  togglingEnabled.value = true
  try {
    const res = await store.setWebdavEnabled(target)
    if (res.ok) {
      ElMessage.success(target ? t('webdav.enabled') : t('webdav.disabled'))
    } else {
      ElMessage.error(res.error || t('webdav.toggleFailed'))
    }
  } finally {
    togglingEnabled.value = false
  }
}

async function copyUrl() {
  if (await copyText(webdavFullUrl.value)) {
    ElMessage.success(t('webdav.copied'))
  } else {
    ElMessage.error(t('webdav.copyFailed'))
  }
}

onMounted(async () => {
  if (!store.webdavLoaded) {
    await store.loadWebdavStatus()
  }
  // used_bytes 是服务端缓存值，WebDAV 之外新增的文件不会自动体现；面板打开时强制重算。
  if (store.webdavEnabled) {
    store.recalculateWebdavUsage()
  }
})
</script>

<style scoped>
.settings-section {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.settings-card {
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

/* 覆盖 Element Plus 默认按钮样式，做成渐变胶囊按钮。 */
.new-account-btn {
  height: 28px;
  padding: 0 12px;
  border: none;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  color: #ffffff;
  background: linear-gradient(135deg, #0ea5e9 0%, #0284c7 100%);
  box-shadow: 0 2px 6px rgba(2, 132, 199, 0.24);
  transition: transform 0.15s ease, box-shadow 0.15s ease, filter 0.15s ease;
}

.new-account-btn:hover,
.new-account-btn:focus {
  color: #ffffff;
  background: linear-gradient(135deg, #0ea5e9 0%, #0284c7 100%);
  filter: brightness(1.05);
  box-shadow: 0 4px 10px rgba(2, 132, 199, 0.32);
  transform: translateY(-1px);
}

.new-account-btn:active {
  transform: translateY(0);
  box-shadow: 0 2px 5px rgba(2, 132, 199, 0.28);
}

.new-account-icon {
  margin-right: 5px;
  font-size: 13px;
}

.section-hint {
  margin: 0;
  font-size: 12px;
  color: #64748b;
  max-width: 520px;
}

.webdav-url-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  padding: 10px 12px;
  background: rgba(14, 165, 233, 0.08);
  border-radius: 8px;
  flex-wrap: wrap;
}

.webdav-url-label {
  font-size: 12px;
  color: #475569;
}

.webdav-url-value {
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
  .settings-section {
    gap: 14px;
  }
  .settings-card {
    padding: 14px 14px;
  }
  .webdav-url-row {
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
  }
}
</style>
