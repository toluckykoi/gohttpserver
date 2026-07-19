<template>
  <div class="settings-section">
    <!-- WebDAV master switch + URL display. The switch is the
         single source of truth for whether /dav/ accepts connections;
         turning it off keeps existing accounts but refuses all
         requests with 503. -->
    <section class="settings-card">
      <div class="card-header">
        <div>
          <h2 class="section-title">WebDAV Service</h2>
          <p class="section-hint">
            Once enabled, files can be accessed via WebDAV clients (Cyberduck, rclone, Windows Explorer, etc.).
          </p>
        </div>
        <el-switch
          :model-value="store.webdavEnabled"
          :loading="togglingEnabled"
          @change="handleToggleEnabled"
        />
      </div>
      <div class="webdav-url-row" v-if="store.webdavEnabled">
        <span class="webdav-url-label">URL:</span>
        <code class="webdav-url-value">{{ webdavFullUrl }}</code>
        <el-button text size="small" @click="copyUrl">
          <el-icon><CopyDocument /></el-icon>
          Copy
        </el-button>
      </div>
    </section>

    <!-- Account list. Delegated to its own component because the table
         + actions are large enough to warrant isolation. -->
    <section class="settings-card" v-if="store.webdavEnabled">
      <div class="card-header">
        <h2 class="section-title">WebDAV Accounts</h2>
        <el-button class="new-account-btn" @click="showCreate = true">
          <el-icon class="new-account-icon"><Plus /></el-icon>
          <span>New Account</span>
        </el-button>
      </div>
      <WebdavAccountList v-if="store.webdavLoaded" />
      <el-skeleton v-else :rows="4" animated />
    </section>

    <!-- Create dialog. Mounted lazily so the password preview
         generator doesn't run until the user opens it. -->
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
import { useAdminStore } from '@/stores/adminStore'
import { copyText } from '@/utils/clipboard'
import WebdavAccountList from './WebdavAccountList.vue'
import WebdavAccountCreate from './WebdavAccountCreate.vue'

const store = useAdminStore()
const togglingEnabled = ref(false)
const showCreate = ref(false)

// Build the full WebDAV URL from the current location + the relative
// path returned by the API. Using window.location.origin keeps it
// correct behind reverse proxies that rewrite the host header.
const webdavFullUrl = computed(() => {
  const origin = window.location.origin
  return `${origin}${store.webdavUrl}`
})

async function handleToggleEnabled(target: boolean) {
  togglingEnabled.value = true
  try {
    const res = await store.setWebdavEnabled(target)
    if (res.ok) {
      ElMessage.success(target ? 'WebDAV enabled' : 'WebDAV disabled')
    } else {
      ElMessage.error(res.error || 'Failed to toggle')
    }
  } finally {
    togglingEnabled.value = false
  }
}

async function copyUrl() {
  if (await copyText(webdavFullUrl.value)) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动选择复制')
  }
}

onMounted(async () => {
  if (!store.webdavLoaded) {
    await store.loadWebdavStatus()
  }
  // used_bytes is a server-side cached counter (maintained incrementally
  // by WebDAV writes). Files added outside WebDAV — or an account pointed
  // at a folder that already has files — won't be reflected until a walk.
  // Force one when the panel opens so the quota bars show real numbers.
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
  /* On narrow screens the title block and the switch/​button can't
     sit side-by-side without overlapping. Wrap them so the action
     drops below the title instead of clipping. */
  flex-wrap: wrap;
}

.section-title {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 600;
  color: #0f172a;
}

/* Polished "New Account" pill: gradient fill, rounded, with a subtle
   shadow and a hover lift so it reads as the primary action. Overrides
   Element Plus's default button chrome. */
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
  /* On phones the URL can be very long; let it shrink and the copy
     button stay clickable rather than the URL eating all the space. */
  min-width: 0;
  flex: 1 1 auto;
}

/* Phone: tighter spacing, same as ProfileSection. */
@media (max-width: 640px) {
  .settings-section {
    gap: 14px;
  }
  .settings-card {
    padding: 14px 14px;
  }
  .webdav-url-row {
    /* Stack the label / URL / copy button vertically so a long URL
       gets a full row to display. */
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
  }
}
</style>
