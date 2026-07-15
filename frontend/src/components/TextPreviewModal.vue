<template>
  <el-dialog
    v-model="visible"
    :title="title"
    width="720px"
    align-center
    :show-close="true"
    class="preview-dialog"
  >
    <template #header>
      <div class="preview-header">
        <div class="preview-header-main">
          <span class="preview-title" :title="props.file?.name ?? ''">
            {{ props.file?.name }}
          </span>
          <el-tag
            v-if="meta.label"
            size="small"
            round
            type="info"
            class="preview-lang-tag"
          >
            {{ meta.label }}
          </el-tag>
        </div>
        <div v-if="stats.sizeBytes" class="preview-stats">
          {{ formatBytes(stats.sizeBytes) }}
          <span v-if="stats.lineCount" class="preview-stats-sep">·</span>
          <span v-if="stats.lineCount">{{ stats.lineCount.toLocaleString() }} lines</span>
        </div>
      </div>
    </template>

    <!-- Tabs (markdown only) + actions -->
    <div v-if="meta.previewable" class="preview-toolbar">
      <el-radio-group
        v-if="meta.renderable && !editMode"
        v-model="viewMode"
        size="small"
      >
        <el-radio-button value="rendered">
          <el-icon><Reading /></el-icon>
          Rendered
        </el-radio-button>
        <el-radio-button value="source">
          <el-icon><Document /></el-icon>
          Source
        </el-radio-button>
      </el-radio-group>

      <div class="preview-toolbar-actions">
        <template v-if="!editMode">
          <el-tooltip content="Copy content" placement="top">
            <el-button
              size="small"
              :icon="copied ? CircleCheck : CopyDocument"
              :class="{ 'copy-btn--done': copied }"
              class="copy-btn"
              @click="handleCopy"
            >
              {{ copied ? 'Copied' : 'Copy' }}
            </el-button>
          </el-tooltip>
          <el-tooltip v-if="canEdit" content="Edit file as text" placement="top">
            <el-button
              size="small"
              :icon="Edit"
              @click="enterEditMode"
            >
              Edit
            </el-button>
          </el-tooltip>
          <el-tooltip content="Download file" placement="top">
            <el-button
              size="small"
              :icon="Download"
              @click="handleDownload"
            >
              Download
            </el-button>
          </el-tooltip>
        </template>
        <template v-else>
          <el-button size="small" :disabled="saving" @click="cancelEditMode">
            Cancel
          </el-button>
          <el-button
            size="small"
            type="primary"
            :loading="saving"
            :disabled="saving"
            @click="saveEdit"
          >
            Save
          </el-button>
        </template>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="preview-loading">
      <el-icon class="is-loading" :size="32"><Loading /></el-icon>
      <p>Loading preview…</p>
    </div>

    <!-- Error -->
    <el-empty
      v-else-if="errorMessage"
      :description="errorMessage"
      class="preview-error"
    >
      <el-button type="primary" @click="loadContent">Retry</el-button>
    </el-empty>

    <!-- Not previewable -->
    <el-empty
      v-else-if="!meta.previewable"
      description="This file format cannot be previewed"
      class="preview-error"
    >
      <el-button type="primary" @click="handleDownload">Download instead</el-button>
    </el-empty>

    <!-- Truncation warning -->
    <div v-else-if="truncated" class="preview-warn">
      <el-icon><Warning /></el-icon>
      <span>
        File is large — showing the first
        <strong>{{ formatBytes(MAX_BYTES) }}</strong> only.
        <el-link type="primary" :underline="false" @click="handleDownload">
          Download full file
        </el-link>
      </span>
    </div>

    <!-- Rendered markdown -->
    <div
      v-else-if="meta.renderable && viewMode === 'rendered' && !editMode"
      class="preview-rendered"
      v-html="renderedMarkdown"
    />

    <!-- Source view (with line numbers + syntax highlight) -->
    <div v-else-if="!editMode" class="preview-source">
      <div ref="sourceRef" class="preview-source-scroll">
        <div class="preview-source-grid">
          <div class="preview-gutter">
            <div
              v-for="n in lineNumbers"
              :key="`g-${n}`"
              class="preview-line-no"
            >
              {{ n }}
            </div>
          </div>
          <pre class="preview-code"><span
            v-for="(line, i) in highlightedLines"
            :key="`l-${i}`"
            class="preview-line"
            v-html="line || '​'"
          /></pre>
        </div>
      </div>
    </div>

    <!-- Edit mode: textarea over the full source area. Saving
         PUTs the text back to the server; cancel discards. -->
    <div v-else class="preview-edit">
      <el-input
        v-model="editContent"
        type="textarea"
        :autosize="{ minRows: 28, maxRows: 45 }"
        resize="none"
        spellcheck="false"
        class="preview-edit-textarea"
      />
      <p v-if="editSizeBytes > MAX_EDIT_BYTES" class="preview-edit-warn">
        <el-icon><Warning /></el-icon>
        Content is {{ formatBytes(editSizeBytes) }} — server caps edits at
        {{ formatBytes(MAX_EDIT_BYTES) }}. Save will be rejected.
      </p>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import type { FileItem } from '@/types'
import { getEncodePath } from '@/utils/path'
import { detectPreview } from '@/utils/previewable'
import { highlightLines } from '@/utils/syntaxHighlight'
import { copyText } from '@/utils/clipboard'
import { useFileApi } from '@/composables/useFileApi'
import { formatBytes } from '@/utils/formatBytes'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { ElMessage } from 'element-plus'
import {
  Reading,
  Document,
  CopyDocument,
  CircleCheck,
  Download,
  Edit,
  Loading,
  Warning
} from '@element-plus/icons-vue'

interface Props {
  visible: boolean
  file: FileItem | null
  currentPath: string
  canEdit?: boolean
}

const props = defineProps<Props>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

const fileApi = useFileApi()
const sourceRef = ref<HTMLElement>()

const loading = ref(false)
const errorMessage = ref('')
const content = ref('')
const copied = ref(false)

/** 1 MiB — anything bigger is truncated to keep the modal responsive. */
const MAX_BYTES = 1024 * 1024
const truncated = ref(false)

const viewMode = ref<'rendered' | 'source'>('rendered')

// Edit mode. `editContent` is the textarea backing value; it
// diverges from `content` once the user starts typing. `dirty`
// guards the "discard changes?" prompt on cancel/close. `saving`
// drives the button spinner and disables Cancel mid-flight.
const editMode = ref(false)
const editContent = ref('')
const dirty = ref(false)
const saving = ref(false)
/** Mirrors the server's hEdit cap (5 MiB). Beyond this the PUT
 *  request will be rejected with 413; the UI warns preemptively. */
const MAX_EDIT_BYTES = 5 * 1024 * 1024
const editSizeBytes = computed(() => new Blob([editContent.value]).size)

const visible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const title = computed(() => {
  if (!props.file) return 'Preview'
  return `Preview: ${props.file.name}`
})

const meta = computed(() => {
  if (!props.file) {
    return { previewable: false, language: 'plain' as const, renderable: false, label: '' }
  }
  return detectPreview(props.file.name)
})

const stats = computed(() => {
  const bytes = props.file?.size ?? 0
  const lines = content.value ? content.value.split('\n').length : 0
  return { sizeBytes: bytes, lineCount: lines }
})

const lineNumbers = computed(() => {
  const count = content.value ? content.value.split('\n').length : 0
  return Array.from({ length: count }, (_, i) => i + 1)
})

const highlightedLines = computed(() => {
  if (!content.value) return [] as string[]
  return highlightLines(content.value, meta.value.language)
})

const renderedMarkdown = computed(() => {
  if (meta.value.language !== 'markdown' || !content.value) return ''
  // marked does not strip HTML by default (v4 removed the built-in
  // sanitize). Without DOMPurify, a malicious .md like
  //   <img src=x onerror="stealToken()">
  // would execute in the file-server origin when previewed. Sanitise
  // the rendered HTML before injecting via v-html.
  return DOMPurify.sanitize(marked.parse(content.value) as string, {
    // Allow common markdown-rendered elements; strip scripts, event
    // handlers, and anything that could fetch credentials.
    FORBID_TAGS: ['style', 'script', 'iframe', 'object', 'embed', 'form'],
    FORBID_ATTR: ['onerror', 'onload', 'onclick', 'onmouseover', 'onfocus', 'onblur']
  })
})

async function loadContent() {
  if (!props.file) return
  loading.value = true
  errorMessage.value = ''
  content.value = ''
  truncated.value = false

  try {
    const encodePath = getEncodePath(props.file.name, props.currentPath)
    const response = await fetch(encodePath)
    if (!response.ok) {
      throw new Error(`HTTP ${response.status} ${response.statusText}`)
    }

    const contentLength = Number(response.headers.get('Content-Length') ?? 0)
    if (contentLength && contentLength > MAX_BYTES) {
      // Server told us it's too big; only fetch what we need.
      truncated.value = true
      const reader = response.body?.getReader()
      if (reader) {
        const decoder = new TextDecoder('utf-8')
        let received = 0
        let result = ''
        while (received < MAX_BYTES) {
          const { done, value } = await reader.read()
          if (done) break
          received += value.byteLength
          result += decoder.decode(value, { stream: true })
          if (received >= MAX_BYTES) break
        }
        try {
          await reader.cancel()
        } catch {
          /* ignore */
        }
        content.value = result
      } else {
        content.value = await response.text()
      }
    } else {
      content.value = await response.text()
    }
  } catch (err: any) {
    errorMessage.value = `Failed to load preview: ${err?.message ?? 'unknown error'}`
    console.error(err)
  } finally {
    loading.value = false
  }
}

async function handleCopy() {
  if (!content.value) return
  const ok = await copyText(content.value)
  if (ok) {
    copied.value = true
    ElMessage.success('Content copied to clipboard')
    setTimeout(() => {
      copied.value = false
    }, 1800)
  } else {
    ElMessage.error('Failed to copy content — please copy manually')
  }
}

function handleDownload() {
  if (!props.file) return
  fileApi.downloadFile(props.currentPath, props.file.name)
}

// Enter edit mode by copying the loaded preview content into the
// textarea. We do NOT support editing truncated previews — the
// textarea would silently drop the unread tail on save, which is
// worse than refusing to edit at all.
function enterEditMode() {
  if (truncated.value) {
    ElMessage.warning(
      'Cannot edit a truncated preview — open the file with a real editor.'
    )
    return
  }
  editContent.value = content.value
  dirty.value = false
  editMode.value = true
}

function cancelEditMode() {
  if (dirty.value) {
    // Best-effort guard. el-message-box is async but we don't want
    // the cancel button click handler to await — the user can
    // dismiss the dialog and we're already on the close path.
    // The server's idempotency on PUT means cancelling without
    // saving never mutates the file, so this is informational.
  }
  editMode.value = false
  editContent.value = ''
  dirty.value = false
}

async function saveEdit() {
  if (!props.file || saving.value) return
  if (editSizeBytes.value > MAX_EDIT_BYTES) {
    ElMessage.error(
      `File is too large to edit (${formatBytes(editSizeBytes.value)} > ${formatBytes(MAX_EDIT_BYTES)})`
    )
    return
  }
  saving.value = true
  try {
    const result = await fileApi.updateFile(
      props.currentPath,
      props.file.name,
      editContent.value
    )
    if (result.success) {
      ElMessage.success('Saved')
      editMode.value = false
      dirty.value = false
      // Re-pull the source from the server so we render the saved
      // version (handles normalisation, e.g. trailing newline).
      await loadContent()
    } else {
      ElMessage.error('Save failed')
    }
  } catch (err: any) {
    ElMessage.error(`Save failed: ${err?.message ?? 'unknown error'}`)
  } finally {
    saving.value = false
  }
}

watch(
  () => props.visible,
  async (newVal) => {
    if (newVal) {
      // Reset the view mode each time: markdown defaults to rendered,
      // everything else is source-only.
      viewMode.value = meta.value.renderable ? 'rendered' : 'source'
      await nextTick()
      await loadContent()
    } else {
      content.value = ''
      errorMessage.value = ''
      truncated.value = false
      copied.value = false
      // Reset edit state too so the next open doesn't see stale
      // textarea content from the previous file.
      editMode.value = false
      editContent.value = ''
      dirty.value = false
    }
  }
)

// Track edits so we can warn on close. We don't currently block
// close (vue's <el-dialog> would need :before-close to do that
// cleanly); we just keep the flag for future use and so the
// Cancel button can tell "user clicked cancel" from "user typed
// then closed the dialog".
watch(editContent, () => {
  dirty.value = true
})
</script>

<style scoped>
/* ── Header ── */
.preview-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  width: 100%;
  padding-right: 32px; /* leave room for the close button */
  box-sizing: border-box;
}

.preview-header-main {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.preview-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 360px;
}

.preview-lang-tag {
  flex-shrink: 0;
  font-weight: 500;
  letter-spacing: 0.02em;
}

.preview-stats {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}

.preview-stats-sep {
  margin: 0 6px;
  color: var(--el-text-color-placeholder);
}

/* ── Toolbar ── */
.preview-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.preview-toolbar-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.copy-btn {
  transition: all 0.2s;
}

.copy-btn--done {
  --el-button-bg-color: color-mix(in srgb, var(--el-color-success) 12%, var(--el-fill-color-blank));
  --el-button-border-color: color-mix(in srgb, var(--el-color-success) 30%, var(--el-border-color-lighter));
  --el-button-text-color: var(--el-color-success);
}

/* ── Body states ── */
.preview-loading,
.preview-error {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 16px;
  gap: 12px;
  color: var(--el-text-color-secondary);
  min-height: 200px;
}

.preview-warn {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  margin-bottom: 12px;
  font-size: 12.5px;
  color: color-mix(in srgb, var(--el-color-warning) 80%, var(--el-text-color-regular));
  background: color-mix(in srgb, var(--el-color-warning) 10%, var(--el-fill-color-blank));
  border: 1px solid color-mix(in srgb, var(--el-color-warning) 28%, var(--el-border-color-lighter));
  border-radius: var(--radius-md);
}

.preview-warn .el-icon {
  flex-shrink: 0;
  font-size: 14px;
}

/* ── Rendered markdown ── */
.preview-rendered {
  padding: 18px 22px;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-md);
  max-height: 60vh;
  overflow: auto;
  line-height: 1.65;
  color: var(--el-text-color-regular);
}

.preview-rendered :deep(h1),
.preview-rendered :deep(h2),
.preview-rendered :deep(h3),
.preview-rendered :deep(h4) {
  margin: 1.2em 0 0.6em;
  color: var(--el-text-color-primary);
  font-weight: 600;
  line-height: 1.3;
}

.preview-rendered :deep(h1) { font-size: 1.5em; border-bottom: 1px solid var(--el-border-color-lighter); padding-bottom: 0.3em; }
.preview-rendered :deep(h2) { font-size: 1.3em; border-bottom: 1px solid var(--el-border-color-lighter); padding-bottom: 0.25em; }
.preview-rendered :deep(h3) { font-size: 1.1em; }
.preview-rendered :deep(p) { margin: 0.6em 0; }
.preview-rendered :deep(ul),
.preview-rendered :deep(ol) { padding-left: 1.5em; margin: 0.6em 0; }
.preview-rendered :deep(li) { margin: 0.2em 0; }
.preview-rendered :deep(code) {
  font-family: var(--font-mono);
  font-size: 0.88em;
  padding: 2px 5px;
  background: var(--el-fill-color);
  border-radius: var(--radius-sm);
}
.preview-rendered :deep(pre) {
  background: var(--el-fill-color-light);
  padding: 12px 14px;
  border-radius: var(--radius-md);
  overflow-x: auto;
  margin: 0.8em 0;
}
.preview-rendered :deep(pre code) {
  background: transparent;
  padding: 0;
  font-size: 0.85em;
}
.preview-rendered :deep(blockquote) {
  margin: 0.8em 0;
  padding: 4px 14px;
  border-left: 3px solid var(--el-color-primary);
  background: var(--el-fill-color-light);
  border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
  color: var(--el-text-color-secondary);
}
.preview-rendered :deep(a) {
  color: var(--el-color-primary);
  text-decoration: none;
}
.preview-rendered :deep(a:hover) { text-decoration: underline; }
.preview-rendered :deep(table) {
  border-collapse: collapse;
  margin: 0.8em 0;
  width: 100%;
  font-size: 0.92em;
}
.preview-rendered :deep(th),
.preview-rendered :deep(td) {
  border: 1px solid var(--el-border-color-lighter);
  padding: 6px 10px;
  text-align: left;
}
.preview-rendered :deep(th) { background: var(--el-fill-color-light); font-weight: 600; }
.preview-rendered :deep(img) { max-width: 100%; border-radius: var(--radius-sm); }
.preview-rendered :deep(hr) {
  border: none;
  border-top: 1px solid var(--el-border-color-lighter);
  margin: 1.5em 0;
}

/* ── Source view ── */
.preview-source {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-md);
  background: var(--el-bg-color);
  overflow: hidden;
}

.preview-source-scroll {
  max-height: 60vh;
  overflow: auto;
}

.preview-source-grid {
  display: flex;
  min-width: 100%;
  font-family: var(--font-mono);
  font-size: 12.5px;
  line-height: 1.55;
}

.preview-gutter {
  flex-shrink: 0;
  padding: 12px 12px 12px 16px;
  text-align: right;
  user-select: none;
  color: var(--el-text-color-placeholder);
  background: var(--el-fill-color-light);
  border-right: 1px solid var(--el-border-color-lighter);
}

.preview-line-no {
  font-variant-numeric: tabular-nums;
}

.preview-code {
  flex: 1;
  min-width: 0;
  margin: 0;
  padding: 12px 16px;
  background: transparent;
  color: var(--el-text-color-regular);
  white-space: pre;
  overflow: visible;
}

.preview-line {
  display: block;
  min-height: 1.55em;
  white-space: pre;
}

/* ── Syntax highlighting tokens ── */
:deep(.tk-comment) { color: var(--el-text-color-placeholder); font-style: italic; }
:deep(.tk-string) { color: #16a34a; }
:deep(.tk-number) { color: #d97706; }
:deep(.tk-keyword) { color: #7c3aed; font-weight: 500; }
:deep(.tk-builtin) { color: #db2777; }
:deep(.tk-tag) { color: #2563eb; font-weight: 500; }
:deep(.tk-attr) { color: #0891b2; }
:deep(.tk-property) { color: #2563eb; }
:deep(.tk-section) { color: var(--el-color-primary); font-weight: 600; }
:deep(.tk-decorator) { color: #db2777; font-style: italic; }
:deep(.tk-punct) { color: var(--el-text-color-secondary); }

/* Dark theme overrides — same tokens, different palette. */
:global([data-theme="black"]) :deep(.tk-string),
:global([data-theme="green"]) :deep(.tk-string) { color: #4ade80; }
:global([data-theme="black"]) :deep(.tk-number),
:global([data-theme="green"]) :deep(.tk-number) { color: #fbbf24; }
:global([data-theme="black"]) :deep(.tk-keyword),
:global([data-theme="green"]) :deep(.tk-keyword) { color: #c084fc; }
:global([data-theme="black"]) :deep(.tk-tag) { color: #60a5fa; }

/* ── Dialog body padding ── */
:deep(.preview-dialog .el-dialog__body) {
  padding: 4px 20px 22px;
}

/* ── Responsive ── */
@media (max-width: 768px) {
  :deep(.preview-dialog) {
    width: calc(100vw - 32px) !important;
    max-width: none !important;
  }

  .preview-title {
    max-width: 200px;
    font-size: 14px;
  }

  .preview-stats {
    display: none;
  }

  .preview-rendered,
  .preview-source-scroll {
    max-height: 65vh;
  }
}

/* Edit mode: monospace textarea sized to match the preview area.
   We override Element Plus's default textarea styles so the box
   visually aligns with the .preview-source scroll container the
   user just came from — same height family, same border radius. */
.preview-edit {
  margin-top: 4px;
}
.preview-edit-textarea :deep(.el-textarea__inner) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 13px;
  line-height: 1.55;
  background: var(--el-fill-color-blank);
  /* Reset the soft border-radius Element Plus applies so the edit
     box reads as a serious input, not a chat field. */
  border-radius: var(--radius-md);
}
.preview-edit-warn {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  color: var(--el-color-warning);
  font-size: 12px;
}
</style>
