<template>
  <el-dialog
    v-model="updateStore.dialogVisible"
    title="软件更新"
    width="520px"
    align-center
    class="update-dialog"
    @closed="handleClosed"
  >
    <!-- 重启等待态：服务已退出，轮询 /healthz 恢复后整页刷新。 -->
    <div v-if="updateStore.restarting" class="update-restarting">
      <el-icon class="is-loading update-restarting-icon" :size="28"><Loading /></el-icon>
      <div class="update-restarting-title">服务正在重启…</div>
      <div class="update-restarting-sub">
        正在等待服务以新版本恢复，页面将自动刷新（已等待 {{ restartWaitSeconds }}s）。
      </div>
    </div>

    <template v-else>
      <!-- 功能不可用：未登录 / 后端未注册。 -->
      <div v-if="!updateStore.status" class="update-disabled">
        <el-empty description="更新功能未启用" :image-size="72" />
        <div class="update-disabled-hint">
          需要在服务端启用 --login 后使用。
        </div>
      </div>

      <div v-else class="update-body">
        <!-- 版本概览 -->
        <div class="update-versions">
          <div class="update-version-chip">
            <span class="update-version-label">当前</span>
            <span class="update-version-value">{{ updateStore.status.current_version || '未知' }}</span>
          </div>
          <el-icon class="update-version-arrow" :size="16"><Right /></el-icon>
          <div class="update-version-chip update-version-chip--new">
            <span class="update-version-label">最新</span>
            <span class="update-version-value">
              {{ updateStore.status.release_version || '—' }}
            </span>
          </div>
        </div>

        <el-alert
          v-if="updateStore.status.required && hasUpdate"
          type="warning"
          :closable="false"
          show-icon
          title="此更新被发布者标记为必须安装"
          class="update-alert"
        />

        <el-alert
          v-if="updateStore.status.state === 'error' && updateStore.status.error"
          type="error"
          :closable="false"
          show-icon
          :title="updateStore.status.error"
          class="update-alert"
        />

        <!-- 下载进度 -->
        <div v-if="updateStore.status.state === 'downloading'" class="update-progress">
          <el-progress
            :percentage="percent"
            :stroke-width="10"
            :show-text="true"
          />
          <div class="update-progress-meta">
            {{ formatSize(updateStore.status.downloaded) }} /
            {{ formatSize(updateStore.status.artifact_size) }}
            <span v-if="updateStore.status.artifact_name" class="update-artifact-name">
              {{ updateStore.status.artifact_name }}
            </span>
          </div>
        </div>

        <!-- 安装/重启就绪提示 -->
        <el-alert
          v-if="updateStore.status.state === 'ready'"
          type="success"
          :closable="false"
          show-icon
          title="安装包已下载并校验通过"
          description="安装会把磁盘上的程序替换为新版本（原文件保留为 .bak），替换后需重启服务生效。"
          class="update-alert"
        />
        <el-alert
          v-if="updateStore.status.state === 'applied'"
          type="success"
          :closable="false"
          show-icon
          title="新版本已写入磁盘"
          description="重启服务后生效。若服务由 systemd(Restart=always) 等守护进程管理，重启后会自动以新版本拉起。"
          class="update-alert"
        />

        <!-- 无更新时的原因说明 -->
        <div v-if="updateStore.status.state === 'idle' && reasonText" class="update-reason">
          <el-icon :size="14"><InfoFilled /></el-icon>
          <span>{{ reasonText }}</span>
        </div>

        <!-- 更新说明（纯文本展示，Vue 插值自动转义） -->
        <template v-if="hasUpdate && updateStore.status.state !== 'downloading'">
          <div class="update-notes-title">更新说明</div>
          <pre class="update-notes">{{ updateStore.status.notes?.trim() || '（无更新说明）' }}</pre>
          <div v-if="publishedText" class="update-published">
            发布于 {{ publishedText }}
            <span v-if="updateStore.status.sha256" class="update-sha" :title="updateStore.status.sha256">
              SHA-256 已随包校验
            </span>
          </div>
        </template>
      </div>
    </template>

    <template #footer>
      <div v-if="!updateStore.restarting && updateStore.status" class="update-footer">
        <el-button
          :loading="checking"
          :disabled="busy"
          @click="handleCheck"
        >
          检查更新
        </el-button>
        <el-button
          v-if="updateStore.status.state === 'available' || (updateStore.status.state === 'error' && hasUpdate)"
          type="primary"
          :disabled="busy"
          @click="handleDownload"
        >
          下载更新（{{ formatSize(updateStore.status.artifact_size) }}）
        </el-button>
        <el-button
          v-if="updateStore.status.state === 'ready'"
          type="primary"
          :loading="applying"
          @click="handleApply"
        >
          安装更新
        </el-button>
        <el-button
          v-if="updateStore.status.state === 'applied'"
          type="primary"
          @click="handleRestart"
        >
          立即重启
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, onBeforeUnmount } from 'vue'
import dayjs from 'dayjs'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Loading, Right, InfoFilled } from '@element-plus/icons-vue'
import { useUpdateStore, UPDATE_REASON_TEXTS } from '@/stores/updateStore'
import { formatBytes } from '@/utils/formatBytes'

const updateStore = useUpdateStore()

const checking = ref(false)
const applying = ref(false)
const restartWaitSeconds = ref(0)
let healthTimer: number | null = null

const st = computed(() => updateStore.status)
const busy = computed(
  () => checking.value || applying.value || st.value?.state === 'downloading'
)

const hasUpdate = computed(() => !!st.value?.release_version)

const percent = computed(() => {
  const s = st.value
  if (!s || !s.artifact_size) return 0
  return Math.min(100, Math.round(((s.downloaded || 0) / s.artifact_size) * 100))
})

const reasonText = computed(() => {
  const r = st.value?.reason
  if (!r) return ''
  return UPDATE_REASON_TEXTS[r] ?? r
})

const publishedText = computed(() => {
  const p = st.value?.published_at
  if (!p) return ''
  const d = dayjs(p)
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm') : p
})

function formatSize(n?: number): string {
  if (!n || n <= 0) return ''
  return formatBytes(n)
}

function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

async function handleCheck() {
  checking.value = true
  try {
    await updateStore.check()
  } catch (err) {
    ElMessage.error(`检查更新失败: ${errText(err)}`)
  } finally {
    checking.value = false
  }
}

async function handleDownload() {
  try {
    await updateStore.startDownload()
  } catch (err) {
    ElMessage.error(`无法开始下载: ${errText(err)}`)
  }
}

async function handleApply() {
  try {
    await ElMessageBox.confirm(
      '将把磁盘上的程序替换为新版本（原文件保留为 .bak），替换完成后需重启服务生效。继续安装吗？',
      '安装更新',
      { type: 'warning', confirmButtonText: '安装', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  applying.value = true
  try {
    await updateStore.apply()
    ElMessage.success('安装完成，请重启服务')
  } catch (err) {
    ElMessage.error(`安装失败: ${errText(err)}`)
  } finally {
    applying.value = false
  }
}

async function handleRestart() {
  try {
    await ElMessageBox.confirm(
      '服务将优雅退出并等待守护进程拉起新版本；若未配置 systemd(Restart=always) 等进程守护，需要手动重新启动。确定现在重启吗？',
      '重启服务',
      { type: 'warning', confirmButtonText: '立即重启', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    await updateStore.restart()
  } catch (err) {
    ElMessage.error(`重启失败: ${errText(err)}`)
    return
  }
  startHealthPolling()
}

/** 重启后轮询未认证的 /healthz，恢复即整页刷新加载新版本前端。 */
function startHealthPolling() {
  stopHealthPolling()
  restartWaitSeconds.value = 0
  healthTimer = window.setInterval(async () => {
    restartWaitSeconds.value++
    if (restartWaitSeconds.value > 90) {
      stopHealthPolling()
      ElMessage.error('等待服务恢复超时，请确认服务是否已重新启动')
      return
    }
    try {
      const resp = await fetch('/healthz', { cache: 'no-store' })
      if (resp.ok) {
        stopHealthPolling()
        window.location.reload()
      }
    } catch {
      /* 服务尚未恢复，继续等待 */
    }
  }, 1000)
}

function stopHealthPolling() {
  if (healthTimer !== null) {
    window.clearInterval(healthTimer)
    healthTimer = null
  }
}

function handleClosed() {
  // 对话框关闭只停本地等待动画；若重启已在进行，轮询继续直到恢复或超时。
  if (!updateStore.restarting) {
    stopHealthPolling()
  }
}

onBeforeUnmount(stopHealthPolling)
</script>

<style scoped>
.update-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.update-versions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 14px 0 6px;
}

.update-version-chip {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  min-width: 120px;
  padding: 10px 16px;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-lg);
}

.update-version-chip--new {
  background: color-mix(in srgb, var(--el-color-primary) 8%, transparent);
  border-color: color-mix(in srgb, var(--el-color-primary) 30%, transparent);
}

.update-version-label {
  font-size: 11px;
  letter-spacing: 0.05em;
  color: var(--el-text-color-placeholder);
}

.update-version-value {
  font-family: var(--font-mono);
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}

.update-version-arrow {
  color: var(--el-text-color-placeholder);
}

.update-alert {
  width: 100%;
}

.update-progress {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.update-progress-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}

.update-artifact-name {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--el-text-color-placeholder);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.update-reason {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.update-notes-title {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.03em;
  color: var(--el-text-color-secondary);
}

.update-notes {
  margin: 0;
  padding: 10px 12px;
  max-height: 200px;
  overflow: auto;
  font-family: inherit;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-regular);
  white-space: pre-wrap;
  word-break: break-word;
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-md);
}

.update-published {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.update-sha {
  margin-left: auto;
  font-size: 11px;
}

.update-footer {
  display: flex;
  justify-content: flex-end;
  gap: 0;
}

.update-reason .el-icon {
  flex-shrink: 0;
}

.update-disabled {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 8px 0;
}

.update-disabled-hint {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  padding-bottom: 12px;
}

.update-restarting {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 28px 0 20px;
}

.update-restarting-icon {
  color: var(--el-color-primary);
}

.update-restarting-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.update-restarting-sub {
  font-size: 12.5px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}
</style>
