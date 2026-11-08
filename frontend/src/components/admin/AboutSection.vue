<template>
  <div class="about-section">
    <!-- Software info -->
    <section class="about-card">
      <div class="about-hero">
        <img src="/favicon.png" alt="GoHTTPServer" class="about-logo">
        <div class="about-hero-text">
          <h2 class="about-name">GoHTTPServer</h2>
          <p class="about-desc">{{ t('about.desc') }}</p>
        </div>
      </div>
      <dl class="info-grid">
        <div class="info-item">
          <dt>{{ t('about.currentVersion') }}</dt>
          <dd><code>{{ version || t('common.unknown') }}</code></dd>
        </div>
        <div class="info-item">
          <dt>{{ t('about.author') }}</dt>
          <dd>
            <a href="https://github.com/toluckykoi" target="_blank" rel="noopener">luckykoi</a>
          </dd>
        </div>
        <div class="info-item">
          <dt>{{ t('about.openSource') }}</dt>
          <dd class="about-links">
            <a href="https://github.com/toluckykoi/gohttpserver" target="_blank" rel="noopener">{{ t('about.github') }}</a>
            <span aria-hidden="true">·</span>
            <a href="https://gitee.com/toluckykoi/gohttpserver" target="_blank" rel="noopener">{{ t('about.gitee') }}</a>
          </dd>
        </div>
      </dl>
    </section>

    <!-- Software updates -->
    <section class="about-card">
      <h2 class="section-title">{{ t('about.softwareUpdates') }}</h2>
      <p class="section-hint">{{ t('about.updateHint') }}</p>
      <div class="update-row">
        <div class="update-state">
          <template v-if="!updateEnabled">
            <el-tag type="info" effect="plain" size="small">{{ t('common.disabled') }}</el-tag>
            <span class="update-state-text">{{ t('about.selfUpdateDisabled') }}</span>
          </template>
          <template v-else-if="pendingUpdate">
            <el-tag type="warning" effect="light" size="small">
              {{ t('about.updateAvailable', { version: updateStore.status?.release_version }) }}
            </el-tag>
            <span class="update-state-text">{{ t('about.updateContinueHint') }}</span>
          </template>
          <template v-else>
            <span class="update-state-text">
              {{ updateStore.status?.state === 'downloading' ? t('about.checking') : t('about.checkHint') }}
            </span>
          </template>
        </div>
        <el-button
          type="primary"
          :loading="checking"
          :disabled="checking || !updateEnabled"
          @click="handleCheckUpdate"
        >
          {{ checking ? t('about.checking') : t('update.checkForUpdates') }}
        </el-button>
      </div>
      <div v-if="lastCheckText" class="update-meta">
        {{ t('about.lastChecked', { date: lastCheckText }) }}
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import dayjs from 'dayjs'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useFileStore } from '@/stores/fileStore'
import { useUpdateStore } from '@/stores/updateStore'
import { UpdateUnavailableError } from '@/composables/useUpdateApi'
import { formatBytes } from '@/utils/formatBytes'

const { t } = useI18n()
const fileStore = useFileStore()
const updateStore = useUpdateStore()

const checking = ref(false)
const version = computed(() => fileStore.version)
const updateEnabled = computed(() => fileStore.updateEnabled)

const lastCheckText = computed(() => {
  const checkTime = updateStore.status?.last_check
  if (!checkTime) return ''
  const d = dayjs(checkTime)
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm:ss') : checkTime
})

// An update is waiting to be handled (available/ready/applied); show an entry hint.
const pendingUpdate = computed(() => {
  const st = updateStore.status
  return !!st && ['available', 'ready', 'applied'].includes(st.state)
})

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

/** reason 到词典 key 的映射。 */
const REASON_KEYS: Record<string, string> = {
  up_to_date: 'update.runningLatest',
  client_ahead: 'update.latestIsNewer',
  no_matching_artifact: 'update.noArtifact',
  no_published_release: 'update.noRelease'
}

/** Check for updates: the download only starts after the user confirms. */
async function handleCheckUpdate() {
  // Never send the request when the server has no update API registered
  // (it would fall into the file-upload handler and return a cryptic 400).
  if (!updateEnabled.value) {
    ElMessage.info(t('about.selfUpdateDisabled'))
    return
  }
  checking.value = true
  try {
    await updateStore.check()
    const st = updateStore.status
    if (!st) {
      ElMessage.info(t('about.selfUpdateDisabled'))
      return
    }
    switch (st.state) {
      case 'available': {
        const size = st.artifact_size ? ` (${formatBytes(st.artifact_size)})` : ''
        const required = st.required ? '\n\n' + t('update.requiredByPublisher') : ''
        let confirmed = false
        try {
          await ElMessageBox.confirm(
            t('update.availableConfirm', { version: st.release_version, size, required }),
            t('update.availableTitle'),
            { type: 'info', confirmButtonText: t('update.installUpdate'), cancelButtonText: t('common.cancel') }
          )
          confirmed = true
        } catch {
          return // User chose Not Now; do nothing.
        }
        if (!confirmed) return
        try {
          await updateStore.startDownload()
        } catch (err) {
          ElMessage.error(t('update.downloadFailed', { msg: errText(err) }))
        }
        // Open the update dialog to show progress and the install/restart steps.
        updateStore.dialogVisible = true
        break
      }
      case 'downloading':
      case 'ready':
      case 'applied':
        // An update is already in progress / waiting; open the dialog to continue.
        updateStore.dialogVisible = true
        break
      case 'error':
        ElMessage.error(st.error || t('update.checkFailed', { msg: errText(new Error('try again later')) }))
        break
      default: {
        const reasonKey = REASON_KEYS[st.reason ?? '']
        ElMessage.success(reasonKey ? t(reasonKey) : t('update.runningLatest'))
      }
    }
  } catch (err) {
    if (err instanceof UpdateUnavailableError) {
      ElMessage.info(t('about.selfUpdateDisabled'))
    } else {
      ElMessage.error(t('update.checkFailed', { msg: errText(err) }))
    }
  } finally {
    checking.value = false
  }
}
</script>

<style scoped>
.about-section {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.about-card {
  background: #f8fafc;
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 10px;
  padding: 18px 20px;
}

.section-title {
  margin: 0 0 8px;
  font-size: 15px;
  font-weight: 600;
  color: #0f172a;
}

.section-hint {
  margin: 0 0 14px;
  font-size: 12px;
  color: #64748b;
}

.about-hero {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 16px;
}

.about-logo {
  width: 44px;
  height: 44px;
  border-radius: 10px;
  flex-shrink: 0;
  box-shadow: 0 2px 8px rgba(15, 23, 42, 0.12);
}

.about-name {
  margin: 0 0 4px;
  font-size: 17px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: #0f172a;
}

.about-desc {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
  color: #475569;
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 14px;
  margin: 0;
}

.info-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.info-item dt {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: #64748b;
  font-weight: 500;
}

.info-item dd {
  margin: 0;
  font-size: 14px;
  color: #0f172a;
  font-weight: 500;
  word-break: break-word;
}

.info-item dd code {
  background: rgba(148, 163, 184, 0.2);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 12px;
}

.info-item dd a {
  color: #0284c7;
  text-decoration: none;
}

.info-item dd a:hover {
  text-decoration: underline;
}

.about-links {
  display: flex;
  align-items: center;
  gap: 6px;
}

.update-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
}

.update-state {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.update-state-text {
  font-size: 13px;
  color: #475569;
}

.update-meta {
  margin-top: 10px;
  font-size: 12px;
  color: #94a3b8;
}

@media (max-width: 640px) {
  .about-section {
    gap: 14px;
  }
  .about-card {
    padding: 14px 14px;
  }
  .info-grid {
    grid-template-columns: 1fr;
    gap: 10px;
  }
}
</style>
