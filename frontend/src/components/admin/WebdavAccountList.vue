<template>
  <div class="webdav-list">
    <!-- Empty state: shown when the operator has just enabled WebDAV
         but hasn't created any accounts yet. Encourages them to make
         one, since /dav/ returns 401 for every request without an
         account. -->
    <div v-if="store.webdavAccounts.length === 0" class="empty-state">
      <el-icon :size="32" class="empty-icon"><FolderOpened /></el-icon>
      <p class="empty-text">No WebDAV accounts yet. Click "New Account" in the top right to get started.</p>
    </div>

    <!-- Desktop: full table. Narrow screens: card list (the table has
         7 columns and becomes unreadable below ~900px, so we switch to
         vertical cards on tablets and phones).

         Clicking anywhere on a row toggles an expanded detail panel
         showing the connect link, username and password. -->
    <el-table
      v-else-if="!isNarrow"
      ref="tableRef"
      :data="store.webdavAccounts"
      stripe
      size="small"
      row-key="id"
      class="account-table"
      @row-click="onRowClick"
    >
      <!-- Expand column: the trigger arrow is hidden via CSS; the whole
           row is the click target. The panel shows the "basic info". -->
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="account-detail" @click.stop>
            <div class="detail-row">
              <span class="detail-label">链接地址</span>
              <code class="detail-value">{{ accountUrl }}</code>
              <el-button text size="small" @click="copyText(accountUrl)">
                <el-icon><CopyDocument /></el-icon>
              </el-button>
            </div>
            <div class="detail-row">
              <span class="detail-label">用户名</span>
              <code class="detail-value">{{ row.username }}</code>
              <el-button text size="small" @click="copyText(row.username)">
                <el-icon><CopyDocument /></el-icon>
              </el-button>
            </div>
            <div class="detail-row">
              <span class="detail-label">密码</span>
              <template v-if="store.getWebdavPassword(row.id)">
                <code class="detail-value">{{ revealed.has(row.id) ? store.getWebdavPassword(row.id) : '••••••••' }}</code>
                <el-button text size="small" @click="toggleReveal(row.id)">
                  <el-icon><View v-if="!revealed.has(row.id)" /><Hide v-else /></el-icon>
                </el-button>
                <el-button text size="small" @click="copyText(store.getWebdavPassword(row.id))">
                  <el-icon><CopyDocument /></el-icon>
                </el-button>
              </template>
              <span v-else class="detail-note">密码未缓存，请使用「重置密码」生成新密码后查看。</span>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="Remark" prop="remark" min-width="120" />
      <el-table-column label="Username" prop="username" min-width="100" />
      <el-table-column label="Root Path" prop="root_path" min-width="100">
        <template #default="{ row }">
          <code class="path-code">{{ row.root_path }}</code>
        </template>
      </el-table-column>
      <el-table-column label="Permission" width="100">
        <template #default="{ row }">
          <el-tag size="small" :type="row.readonly ? 'warning' : 'success'">
            {{ row.readonly ? 'Read-only' : 'Read/Write' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="System Protection" width="120">
        <template #default="{ row }">
          <el-tag size="small" :type="row.protect_system_files ? 'info' : 'danger'">
            {{ row.protect_system_files ? 'On' : 'Off' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="Quota" min-width="170">
        <template #default="{ row }">
          <div class="quota-cell">
            <el-progress
              :percentage="quotaPercent(row)"
              :color="quotaColor(row)"
              :stroke-width="6"
              :show-text="false"
              style="margin-bottom: 4px"
            />
            <div class="quota-text">
              {{ formatBytes(row.used_bytes) }} /
              <span v-if="row.quota_bytes > 0">{{ formatBytes(row.quota_bytes) }}</span>
              <span v-else class="muted">不限</span>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="Created" width="140">
        <template #default="{ row }">
          {{ formatTime(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column label="Actions" width="80" fixed="right">
        <template #default="{ row }">
          <el-dropdown trigger="click" @command="(cmd: string) => onCommand(cmd, row)">
            <el-button text class="action-trigger" @click.stop>
              <el-icon :size="18"><MoreFilled /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="edit">
                  <el-icon><Edit /></el-icon> 编辑
                </el-dropdown-item>
                <el-dropdown-item command="reset">
                  <el-icon><Key /></el-icon> 重置密码
                </el-dropdown-item>
                <el-dropdown-item command="delete" divided class="danger-item">
                  <el-icon><Delete /></el-icon> 删除
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </template>
      </el-table-column>
    </el-table>

    <!-- Narrow layout: vertical cards. Tapping the card header toggles
         the same detail panel; actions live in a three-dot menu. -->
    <div v-else class="mobile-cards">
      <div v-for="acc in store.webdavAccounts" :key="acc.id" class="mobile-card">
        <div class="mobile-card-header" @click="toggleMobile(acc.id)">
          <span class="mobile-remark">{{ acc.remark }}</span>
          <div class="mobile-header-right">
            <el-tag size="small" :type="acc.readonly ? 'warning' : 'success'">
              {{ acc.readonly ? 'Read-only' : 'Read/Write' }}
            </el-tag>
            <el-dropdown trigger="click" @command="(cmd: string) => onCommand(cmd, acc)">
              <el-button text class="action-trigger" @click.stop>
                <el-icon :size="18"><MoreFilled /></el-icon>
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="edit">
                    <el-icon><Edit /></el-icon> 编辑
                  </el-dropdown-item>
                  <el-dropdown-item command="reset">
                    <el-icon><Key /></el-icon> 重置密码
                  </el-dropdown-item>
                  <el-dropdown-item command="delete" divided class="danger-item">
                    <el-icon><Delete /></el-icon> 删除
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>
        <div class="mobile-row"><span>Root Path</span><code>{{ acc.root_path }}</code></div>
        <div class="mobile-row">
          <span>System Protection</span>
          <el-tag size="small" :type="acc.protect_system_files ? 'info' : 'danger'">
            {{ acc.protect_system_files ? 'On' : 'Off' }}
          </el-tag>
        </div>
        <div class="mobile-row">
          <span>Quota</span>
          <span>
            {{ formatBytes(acc.used_bytes) }} /
            <span v-if="acc.quota_bytes > 0">{{ formatBytes(acc.quota_bytes) }}</span>
            <span v-else class="muted">不限</span>
          </span>
        </div>
        <div class="mobile-row"><span>Created</span><span>{{ formatTime(acc.created_at) }}</span></div>
        <!-- Expanded "basic info" panel, mirrors the desktop detail row. -->
        <div v-if="expandedMobile.has(acc.id)" class="account-detail account-detail--mobile">
          <div class="detail-row">
            <span class="detail-label">链接地址</span>
            <code class="detail-value">{{ accountUrl }}</code>
            <el-button text size="small" @click="copyText(accountUrl)">
              <el-icon><CopyDocument /></el-icon>
            </el-button>
          </div>
          <div class="detail-row">
            <span class="detail-label">用户名</span>
            <code class="detail-value">{{ acc.username }}</code>
            <el-button text size="small" @click="copyText(acc.username)">
              <el-icon><CopyDocument /></el-icon>
            </el-button>
          </div>
          <div class="detail-row">
            <span class="detail-label">密码</span>
            <template v-if="store.getWebdavPassword(acc.id)">
              <code class="detail-value">{{ revealed.has(acc.id) ? store.getWebdavPassword(acc.id) : '••••••••' }}</code>
              <el-button text size="small" @click="toggleReveal(acc.id)">
                <el-icon><View v-if="!revealed.has(acc.id)" /><Hide v-else /></el-icon>
              </el-button>
              <el-button text size="small" @click="copyText(store.getWebdavPassword(acc.id))">
                <el-icon><CopyDocument /></el-icon>
              </el-button>
            </template>
            <span v-else class="detail-note">密码未缓存，请使用「重置密码」生成新密码后查看。</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Edit dialog. Mounted lazily so it only initialises when an
         account is actually being edited. -->
    <WebdavAccountEdit
      v-if="showEdit"
      v-model:visible="showEdit"
      :account="editAccount"
    />


    <!-- Reset password result dialog. Plaintext password is shown here
         once, with a copy button. Closing the dialog discards the
         password forever — there's no way to recover it. -->
    <el-dialog
      v-model="showPasswordDialog"
      title="New Password"
      width="min(440px, 92vw)"
      :close-on-click-modal="false"
      append-to-body
    >
      <div class="password-display">
        <code class="password-value">{{ resetPasswordValue }}</code>
        <el-button text @click="copyPassword">
          <el-icon><CopyDocument /></el-icon>
        </el-button>
      </div>
      <p class="password-warning">
        This password is shown only once. Copy it now — it cannot be viewed again after closing.
      </p>
      <!-- Connect URL with embedded credentials. Many WebDAV clients
           (rclone, Cyberduck, command-line curl, macOS Finder "Connect
           to Server") accept a URL of the form
           http://user:password@host:port/path and pre-fill the
           auth dialog. Showing this string makes the "which
           username? which password?" guesswork go away, which is the
           single most common source of "Linux shows Unauthorized"
           reports from operators who hand-copied the password. -->
      <div v-if="resetConnectUrl" class="connect-url-row">
        <span class="connect-url-label">Connect URL</span>
        <code class="connect-url-value">{{ resetConnectUrl }}</code>
        <el-button text size="small" @click="copyConnectUrl">
          <el-icon><CopyDocument /></el-icon>
          Copy
        </el-button>
      </div>
      <p class="connect-url-hint">
        Paste into your client's "Connect to Server" / URL field. Username and password are embedded so the client pre-fills them.
      </p>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  CopyDocument, FolderOpened, MoreFilled, Edit, Delete, Key, View, Hide,
} from '@element-plus/icons-vue'
import { useAdminStore } from '@/stores/adminStore'
import { copyText as copyToClipboard } from '@/utils/clipboard'
import type { WebdavAccount } from '@/types'
import WebdavAccountEdit from './WebdavAccountEdit.vue'

const store = useAdminStore()

// Ref to the el-table so onRowClick can toggle a row's expansion. The
// expand arrow itself is hidden (CSS) — the whole row is the trigger.
const tableRef = ref()

// Edit dialog state. editAccount is the row being edited; showEdit
// mounts the dialog lazily.
const showEdit = ref(false)
const editAccount = ref<WebdavAccount | null>(null)

// Mobile: which cards have their detail panel expanded. Desktop uses
// el-table's own expansion state instead.
const expandedMobile = ref<Set<string>>(new Set())

// Which accounts currently have their password revealed (eye toggle).
const revealed = ref<Set<string>>(new Set())
function toggleReveal(id: string) {
  const next = new Set(revealed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  revealed.value = next
}

// The connect link is the same for every account (origin + the WebDAV
// base path); root_path is a server-side scope, not part of the URL.
const accountUrl = computed(() => `${window.location.origin}${store.webdavUrl || '/dav/'}`)

// The table has 7 columns (Remark/Username/Root Path/Permission/System
// Protection/Created/Actions). On anything below ~900px it gets too
// cramped to read, so we switch to the vertical card layout. The
// threshold is higher than the App's 640px phone breakpoint because
// the table genuinely needs more room than a simple list — a tablet
// in portrait is still too narrow.
const isNarrow = ref(window.innerWidth < 900)
function handleResize() {
  isNarrow.value = window.innerWidth < 900
}

// Toggle the desktop expanded detail panel by clicking anywhere on the
// row. Clicks on the actions dropdown call @click.stop so they don't
// bubble here and accidentally toggle the panel.
function onRowClick(row: WebdavAccount) {
  tableRef.value?.toggleRowExpansion(row)
}

// Toggle a mobile card's detail panel.
function toggleMobile(id: string) {
  const next = new Set(expandedMobile.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expandedMobile.value = next
}

// Dispatch the three-dot menu commands to the matching handler.
function onCommand(cmd: string, acc: WebdavAccount) {
  if (cmd === 'edit') {
    editAccount.value = acc
    showEdit.value = true
  } else if (cmd === 'reset') {
    handleResetPassword(acc)
  } else if (cmd === 'delete') {
    handleDelete(acc)
  }
}

async function copyText(text: string) {
  if (await copyToClipboard(text)) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动选择复制')
  }
}

const showPasswordDialog = ref(false)
const resetPasswordValue = ref('')
// The webdav account whose password was just reset. We need its
// username to build the connect URL — store the whole account so
// this dialog can be reused for future per-account helpers without
// re-plumbing the username from the table.
const resetAccount = ref<WebdavAccount | null>(null)

function formatTime(unix: number): string {
  if (!unix) return '—'
  const d = new Date(unix * 1000)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// formatBytes converts raw bytes to a human-readable string with the
// largest unit that keeps the leading number >= 1. Mirrors what
// Windows / macOS Finder show in their drive-info panels so users get
// consistent numbers across admin UI and OS-level quota display.
function formatBytes(n: number): string {
  if (!n || n < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  // Two decimals for < 10 in the chosen unit (matches Finder), else
  // zero decimals. Always show at least one decimal for non-byte.
  if (i === 0) return `${v} ${units[i]}`
  if (v < 10) return `${v.toFixed(2)} ${units[i]}`
  if (v < 100) return `${v.toFixed(1)} ${units[i]}`
  return `${v.toFixed(0)} ${units[i]}`
}

// quotaPercent returns 0..100 (clamped). Unlimited accounts (quota=0)
// always show 0 to avoid a misleading "empty bar" — the column already
// displays "不限" as a textual label.
function quotaPercent(row: WebdavAccount): number {
  if (!row.quota_bytes || row.quota_bytes <= 0) return 0
  return Math.min(100, Math.round((row.used_bytes / row.quota_bytes) * 100))
}

// quotaColor returns the el-progress bar color based on fill. We use
// blue for healthy (<80%), warning yellow for near-full (80-100%),
// and red for over-quota (>100% — which can happen if the operator
// shrunk quota below current usage; the system doesn't delete files
// retroactively).
function quotaColor(row: WebdavAccount): string {
  const pct = quotaPercent(row)
  if (pct > 100) return '#ef4444'
  if (pct >= 80) return '#f59e0b'
  return '#3b82f6'
}

// Build the connect URL with embedded credentials. We pull username
// from the account that was just reset (resetAccount) and password
// from the freshly-generated value (resetPasswordValue). Empty
// until both are populated. The userinfo portion is percent-encoded
// per RFC 3986 — our generated passwords only use an unambiguous
// alphabet so no escaping is needed today, but encoding defensively
// avoids surprises if the alphabet changes later.
const resetConnectUrl = computed(() => {
  if (!resetAccount.value || !resetPasswordValue.value) return ''
  const u = encodeURIComponent(resetAccount.value.username)
  const p = encodeURIComponent(resetPasswordValue.value)
  const origin = window.location.origin
  const path = store.webdavUrl || '/dav/'
  // origin already includes scheme://host:port; append path with
  // embedded userinfo between scheme:// and host.
  const m = origin.match(/^([a-z]+:\/\/)(.+)$/i)
  if (!m) return ''
  return `${m[1]}${u}:${p}@${m[2]}${path.replace(/^\//, '')}`
})

async function handleResetPassword(acc: WebdavAccount) {
  try {
    await ElMessageBox.confirm(
      `Reset the password for "${acc.remark}"? The old password will be invalidated immediately.`,
      'Reset Password',
      { type: 'warning', confirmButtonText: 'Reset', cancelButtonText: 'Cancel' },
    )
  } catch {
    return
  }
  const res = await store.resetWebdavPassword(acc.id)
  if (res.ok) {
    resetAccount.value = acc
    resetPasswordValue.value = res.data.password
    showPasswordDialog.value = true
  } else {
    ElMessage.error(res.error || 'Reset failed')
  }
}

async function handleDelete(acc: WebdavAccount) {
  try {
    await ElMessageBox.confirm(
      `Delete account "${acc.remark}"? This action cannot be undone.`,
      'Delete Account',
      { type: 'warning', confirmButtonText: 'Delete', cancelButtonText: 'Cancel' },
    )
  } catch {
    return
  }
  const res = await store.deleteWebdavAccount(acc.id)
  if (res.ok) {
    ElMessage.success('Account deleted')
  } else {
    ElMessage.error(res.error || 'Delete failed')
  }
}

async function copyPassword() {
  if (await copyToClipboard(resetPasswordValue.value)) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动选择复制')
  }
}

async function copyConnectUrl() {
  if (!resetConnectUrl.value) return
  if (await copyToClipboard(resetConnectUrl.value)) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动选择复制')
  }
}

onMounted(() => {
  window.addEventListener('resize', handleResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
})
</script>

<style scoped>
.webdav-list {
  margin-top: 8px;
}

.empty-state {
  text-align: center;
  padding: 32px 16px;
  color: #64748b;
}

.empty-icon {
  color: #94a3b8;
  margin-bottom: 8px;
}

.empty-text {
  margin: 0;
  font-size: 13px;
}

.account-table :deep(.path-code) {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 12px;
  background: rgba(148, 163, 184, 0.2);
  padding: 1px 6px;
  border-radius: 4px;
}

/* Rows are clickable to expand the detail panel. */
.account-table :deep(.el-table__row) {
  cursor: pointer;
}

/* Hide the default expand arrow — the whole row toggles the panel — and
   collapse the expand column to a hairline so it doesn't leave a gap. */
.account-table :deep(.el-table__expand-column .cell) {
  display: none;
}
.account-table :deep(.el-table__expand-column) {
  width: 0;
  padding: 0;
}

.action-trigger {
  padding: 4px;
  color: #64748b;
}
.action-trigger:hover {
  color: #0f172a;
}

.danger-item {
  color: var(--el-color-danger);
}

/* Basic-info panel (link / username / password), shown on row expand. */
.account-detail {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 16px;
  background: rgba(14, 165, 233, 0.05);
}
.account-detail--mobile {
  margin-top: 8px;
  border-top: 1px solid rgba(148, 163, 184, 0.2);
  padding: 10px 0 0;
  background: transparent;
}
.detail-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
/* Element Plus adds margin-left:12px between adjacent buttons; on top of
   the flex gap that pushes the reveal / copy icons too far apart. Rely
   on the row's gap only. */
.detail-row :deep(.el-button + .el-button) {
  margin-left: 0;
}
.detail-label {
  font-size: 12px;
  color: #475569;
  width: 64px;
  flex-shrink: 0;
}
.detail-value {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 12px;
  color: #0f172a;
  background: rgba(148, 163, 184, 0.18);
  padding: 2px 8px;
  border-radius: 4px;
  word-break: break-all;
  user-select: all;
  min-width: 0;
}
.detail-note {
  font-size: 11px;
  color: #94a3b8;
}

.mobile-cards {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.mobile-card {
  background: #ffffff;
  border: 1px solid rgba(148, 163, 184, 0.3);
  border-radius: 8px;
  padding: 12px;
}

.mobile-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
  cursor: pointer;
  /* A very long remark name could push the tag off-screen. Wrap
     instead of clipping. */
  flex-wrap: wrap;
}

.mobile-header-right {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.mobile-remark {
  font-weight: 600;
  font-size: 14px;
  color: #0f172a;
  /* Long remarks shouldn't overflow; break and wrap to a new line. */
  word-break: break-word;
  min-width: 0;
}

.mobile-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 12px;
  color: #475569;
}

.quota-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 130px;
}

.quota-text {
  font-size: 11px;
  color: #475569;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  white-space: nowrap;
}

.muted {
  color: #94a3b8;
  font-style: italic;
}

.mobile-row code {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 11px;
  background: rgba(148, 163, 184, 0.2);
  padding: 1px 5px;
  border-radius: 4px;
  /* Long root_path values (e.g. /very/deep/nested/path) should wrap
     rather than push the label off the right edge. */
  word-break: break-all;
  text-align: right;
  min-width: 0;
}

.mobile-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
  border-top: 1px solid rgba(148, 163, 184, 0.2);
  padding-top: 8px;
}

.password-display {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px;
  background: rgba(14, 165, 233, 0.08);
  border-radius: 8px;
}

.password-value {
  flex: 1;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 16px;
  font-weight: 600;
  color: #0f172a;
  letter-spacing: 0.04em;
  user-select: all;
  min-width: 0;
}

.password-warning {
  margin: 12px 0 0;
  font-size: 12px;
  color: #b45309;
  background: rgba(245, 158, 11, 0.12);
  padding: 8px 12px;
  border-radius: 6px;
}

/* Connect URL row — same shape as the password display, but with a
   label on the left and a copy button on the right. The URL has
   user:password@host in the middle so a wide layout would stretch
   it; we wrap aggressively to keep the dialog narrow. */
.connect-url-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
  padding: 10px 12px;
  background: rgba(14, 165, 233, 0.08);
  border-radius: 8px;
  flex-wrap: wrap;
}
.connect-url-label {
  font-size: 11px;
  color: #475569;
  flex-shrink: 0;
}
.connect-url-value {
  flex: 1 1 auto;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 12px;
  color: #0f172a;
  background: rgba(148, 163, 184, 0.18);
  padding: 2px 8px;
  border-radius: 4px;
  word-break: break-all;
  user-select: all;
  min-width: 0;
}
.connect-url-hint {
  margin: 8px 0 0;
  font-size: 11.5px;
  color: #64748b;
  line-height: 1.4;
}
</style>
