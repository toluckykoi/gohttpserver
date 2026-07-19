<template>
  <div class="admin-shell">
    <div class="admin-card">
      <!-- Top bar: brand + close. Close returns to the file manager.
           We use a router-friendly replace so the back button doesn't
           re-enter the panel. -->
      <header class="admin-topbar">
        <div class="admin-brand">
          <span class="admin-logo" aria-hidden="true">
            <img src="/favicon.png" alt="logo" width="20" height="20" style="border-radius:4px">
          </span>
          <h1 class="admin-title">Admin Panel</h1>
        </div>
        <el-button text class="admin-close" aria-label="Close" @click="handleClose">
          <el-icon :size="20"><Close /></el-icon>
        </el-button>
      </header>

      <!-- Body: left nav + right content. On phones the nav collapses
           to a top bar of icons (el-tabs handles this naturally via
           the tab-position prop). -->
      <div class="admin-body">
        <el-tabs
          v-model="activeTab"
          :tab-position="isPhone ? 'top' : 'left'"
          class="admin-tabs"
        >
          <el-tab-pane name="profile">
            <template #label>
              <span class="admin-tab-label">
                <el-icon><UserFilled /></el-icon>
                <span class="admin-tab-text">Profile</span>
              </span>
            </template>
            <ProfileSection v-if="activeTab === 'profile'" @username-changed="onUsernameChanged" />
          </el-tab-pane>

          <el-tab-pane name="settings">
            <template #label>
              <span class="admin-tab-label">
                <el-icon><Setting /></el-icon>
                <span class="admin-tab-text">Settings</span>
              </span>
            </template>
            <SettingsSection v-if="activeTab === 'settings'" />
          </el-tab-pane>
        </el-tabs>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Close, UserFilled, Setting } from '@element-plus/icons-vue'
import ProfileSection from '@/components/admin/ProfileSection.vue'
import SettingsSection from '@/components/admin/SettingsSection.vue'

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'username-changed', value: string): void
}>()

const activeTab = ref<'profile' | 'settings'>('profile')

// Phone breakpoint — same threshold as App.vue's isPhone so the panel
// matches the rest of the app's responsive behaviour.
const isPhone = ref(window.innerWidth < 640)
function handleResize() {
  isPhone.value = window.innerWidth < 640
}

function handleClose() {
  emit('close')
}

function onUsernameChanged(newName: string) {
  emit('username-changed', newName)
}

onMounted(() => {
  window.addEventListener('resize', handleResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
})
</script>

<style scoped>
.admin-shell {
  position: fixed;
  inset: 0;
  z-index: 2000;
  background: linear-gradient(135deg, #f1f5f9 0%, #e2e8f0 100%);
  color: #1e293b;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  overflow: auto;
  display: flex;
  justify-content: center;
  padding: 24px 16px;
  box-sizing: border-box;
}

.admin-card {
  width: min(1400px, 100%);
  background: #ffffff;
  border: 1px solid rgba(148, 163, 184, 0.35);
  border-radius: 14px;
  box-shadow: 0 18px 40px rgba(15, 23, 42, 0.12);
  display: flex;
  flex-direction: column;
  max-height: 100%;
  overflow: hidden;
}

.admin-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid rgba(148, 163, 184, 0.25);
  flex-shrink: 0;
}

.admin-brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
}

.admin-logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  background: rgba(14, 165, 233, 0.12);
  border-radius: 8px;
}

.admin-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: #0f172a;
}

.admin-close {
  color: #64748b;
}

.admin-body {
  flex: 1 1 auto;
  overflow: hidden;
  min-height: 0;
}

.admin-tabs {
  height: 100%;
  display: flex;
  --el-tabs-header-height: 44px;
}

.admin-tabs :deep(.el-tabs__header) {
  margin: 0;
  flex-shrink: 0;
  background: rgba(248, 250, 252, 0.7);
  border-right: 1px solid rgba(148, 163, 184, 0.2);
}

.admin-tabs :deep(.el-tabs__nav-wrap)::after {
  background-color: transparent;
}

.admin-tabs :deep(.el-tabs__item) {
  padding: 0 18px;
  height: 44px;
  line-height: 44px;
  color: #475569;
  font-weight: 500;
  transition: background-color 0.15s ease, color 0.15s ease;
}

.admin-tabs :deep(.el-tabs__item:hover) {
  color: #0f172a;
  background-color: rgba(14, 165, 233, 0.06);
}

.admin-tabs :deep(.el-tabs__item.is-active) {
  color: #0284c7;
  font-weight: 700;
  background-color: rgba(14, 165, 233, 0.12);
}

/* Vertical (left) tabs: left-align each label so the icons and text
   line up along the left edge instead of being centered per-item. */
.admin-tabs :deep(.el-tabs__item.is-left) {
  display: flex;
  justify-content: flex-start;
}

.admin-tabs :deep(.el-tabs__active-bar) {
  background-color: #0284c7;
  height: 3px !important;
  border-radius: 2px;
}

.admin-tabs :deep(.el-tabs__content) {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  padding: 24px;
  box-sizing: border-box;
}

.admin-tab-label {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

/* Tablet: tighten things up a bit but keep the desktop layout. */
@media (max-width: 900px) {
  .admin-shell {
    padding: 16px 12px;
  }
  .admin-tabs :deep(.el-tabs__content) {
    padding: 18px;
  }
}

/* Phone: full-screen, no card chrome, safe-area aware. */
@media (max-width: 640px) {
  .admin-shell {
    padding: 0;
    /* Honor notched phones: keep the top bar below the status bar
       and the content above the home indicator. */
    padding-top: env(safe-area-inset-top);
    padding-bottom: env(safe-area-inset-bottom);
  }
  .admin-card {
    border-radius: 0;
    border: none;
    box-shadow: none;
    /* Use 100vh minus the safe-area insets so the card fills the
       viewport without being clipped by the notch / home bar. */
    max-height: calc(100vh - env(safe-area-inset-top) - env(safe-area-inset-bottom));
  }
  .admin-topbar {
    padding: 12px 14px;
  }
  .admin-title {
    font-size: 16px;
  }
  .admin-tabs {
    flex-direction: column;
  }
  .admin-tabs :deep(.el-tabs__header) {
    border-right: none;
    border-bottom: 1px solid rgba(148, 163, 184, 0.2);
  }
  .admin-tabs :deep(.el-tabs__content) {
    padding: 14px;
  }
}
</style>
