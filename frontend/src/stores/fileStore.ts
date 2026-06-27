import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { FileItem, AuthInfo, UserInfo } from '@/types'
import { useFileApi } from '@/composables/useFileApi'
import { useTheme } from '@/composables/useTheme'
import { ElMessage } from 'element-plus'

export const useFileStore = defineStore('file', () => {
  const { currentTheme } = useTheme()
  const fileApi = useFileApi()

  // State
  const currentPath = ref('')
  const files = ref<FileItem[]>([])
  const auth = ref<AuthInfo>({ upload: false, delete: false })
  const user = ref<UserInfo | null>(null)
  const version = ref('')
  const loading = ref(false)
  const showHidden = ref(false)
  const searchQuery = ref('')
  const sortProp = ref('mtime')
  const sortOrder = ref<'ascending' | 'descending'>('descending')
  // Multi-select state. Keyed by FileItem.path (URL-encoded).
  // Selection is cleared automatically on navigation (see loadFiles).
  const selectedFiles = ref<Set<string>>(new Set())
  // Mobile-only: when true, every card shows its checkbox and tapping a
  // card toggles selection instead of navigating. Desktop ignores this.
  const selectionMode = ref(false)

  // Computed
  const sortedFiles = computed(() => {
    let result = [...files.value]

    if (!showHidden.value) {
      result = result.filter(f => !f.name.startsWith('.'))
    }

    const prop = sortProp.value
    const order = sortOrder.value

    result.sort((a, b) => {
      // Directories always first
      const aIsDir = a.type === 'dir' ? 1 : 0
      const bIsDir = b.type === 'dir' ? 1 : 0
      if (aIsDir !== bIsDir) return bIsDir - aIsDir

      // Then sort by selected column
      let cmp = 0
      if (prop === 'name') {
        cmp = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
      } else if (prop === 'size') {
        cmp = (a.size || 0) - (b.size || 0)
      } else {
        cmp = a.mtime - b.mtime
      }
      // Name tiebreaker. JS Array.sort is stable, so two items with
      // the same primary key would otherwise keep whatever order the
      // backend sent — which is "no particular order" because Go map
      // iteration is intentionally randomised. Without this, rows
      // sharing an mtime (files uploaded in the same second, files
      // copied together and never touched) visibly shuffle on every
      // refresh. Name is unique within a directory, so this gives a
      // fully deterministic order regardless of backend behaviour.
      if (cmp === 0) {
        cmp = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
      }
      return order === 'ascending' ? cmp : -cmp
    })

    return result
  })

  const selectedCount = computed(() => selectedFiles.value.size)
  // "All selected" only makes sense when there's at least one row to select
  // and every visible row is in the selection set.
  const isAllSelected = computed(
    () => selectedCount.value > 0 && selectedCount.value === sortedFiles.value.length
  )

  // Actions
  async function loadFiles(path?: string, search?: string, options: { silent?: boolean } = {}) {
    if (!options.silent) {
      loading.value = true
    }
    try {
      const targetPath = path || currentPath.value || '/'
      const response = await fileApi.fetchFiles(targetPath, search)
      files.value = response.files
      auth.value = response.auth
      // Clear selection when navigating to a different directory.
      // Same-path search re-runs preserve selection.
      if (path !== undefined && path !== currentPath.value) {
        clearSelection()
        selectionMode.value = false
      }
      if (path) {
        currentPath.value = path
      }
      if (search) {
        searchQuery.value = search
      }
    } catch (error) {
      ElMessage.error(`Failed to load files: ${error}`)
      console.error(error)
    } finally {
      if (!options.silent) {
        loading.value = false
      }
    }
  }

  async function loadUser() {
    try {
      const userInfo = await fileApi.fetchUser()
      user.value = userInfo
    } catch (error) {
      console.error('Failed to load user:', error)
    }
  }

  async function loadSystemInfo() {
    try {
      const info = await fileApi.fetchSystemInfo()
      version.value = info.version
    } catch (error) {
      console.error('Failed to load system info:', error)
    }
  }

  async function uploadFile(file: File, options?: { filename?: string; unzip?: boolean }) {
    try {
      await fileApi.uploadFile(currentPath.value, file, options)
      ElMessage.success('File uploaded successfully')
      await loadFiles()
    } catch (error) {
      ElMessage.error(`Failed to upload file: ${error}`)
      throw error
    }
  }

  async function createDirectory(name: string) {
    try {
      await fileApi.createDirectory(currentPath.value, name)
      ElMessage.success('Directory created successfully')
      await loadFiles()
    } catch (error) {
      ElMessage.error(`Failed to create directory: ${error}`)
      throw error
    }
  }

  async function deleteFile(filename: string) {
    const target = files.value.find(f => f.name === filename)

    // Optimistic local removal. The previous flow waited on a full
    // directory GET before updating the list, which — especially for
    // large files in directories with many siblings — left the row
    // visible behind the loading spinner for long enough to feel
    // like the click was lost. Drop the row immediately on confirm,
    // then reconcile with the server.
    if (target) {
      files.value = files.value.filter(f => f.name !== filename)
      if (selectedFiles.value.size > 0) {
        const next = new Set(selectedFiles.value)
        next.delete(target.path)
        selectedFiles.value = next
      }
    }

    try {
      await fileApi.deleteFile(currentPath.value, filename)
      ElMessage.success('File deleted successfully')
      // Silent background sync. Picks up anything that changed
      // concurrently (another client uploading, etc.) without
      // re-triggering the loading spinner over the list the user is
      // already looking at. Errors are swallowed: the optimistic
      // state already matches the server's confirmation.
      loadFiles(undefined, undefined, { silent: true }).catch(err =>
        console.error('Background refresh after delete failed:', err)
      )
    } catch (error) {
      // Server rejected the delete — pull the real list back so the
      // UI reflects ground truth. Silent for the same reason as the
      // success path: no spinner flash over the user's current view.
      loadFiles(undefined, undefined, { silent: true }).catch(() => {})
      ElMessage.error(`Failed to delete file: ${error}`)
      throw error
    }
  }

  function toggleShowHidden() {
    showHidden.value = !showHidden.value
  }

  function setSort(prop: string, order: 'ascending' | 'descending' | null) {
    if (order) {
      sortProp.value = prop
      sortOrder.value = order
    } else {
      // Reset to default sort
      sortProp.value = 'mtime'
      sortOrder.value = 'descending'
    }
  }

  // Multi-select actions. The Set is replaced wholesale on every change so
  // Vue's reactivity reliably picks up the new contents.
  function toggleSelect(path: string) {
    const next = new Set(selectedFiles.value)
    if (next.has(path)) {
      next.delete(path)
    } else {
      next.add(path)
    }
    selectedFiles.value = next
  }

  function setSelection(paths: string[]) {
    selectedFiles.value = new Set(paths)
  }

  function selectAll(paths: string[]) {
    selectedFiles.value = new Set(paths)
  }

  function clearSelection() {
    if (selectedFiles.value.size === 0) return
    selectedFiles.value = new Set()
  }

  return {
    currentPath,
    files,
    sortedFiles,
    auth,
    user,
    version,
    loading,
    showHidden,
    searchQuery,
    selectedFiles,
    selectedCount,
    isAllSelected,
    selectionMode,
    currentTheme,
    loadFiles,
    loadUser,
    loadSystemInfo,
    uploadFile,
    createDirectory,
    deleteFile,
    toggleShowHidden,
    sortProp,
    sortOrder,
    setSort,
    toggleSelect,
    setSelection,
    selectAll,
    clearSelection
  }
})