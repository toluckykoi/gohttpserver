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
      return order === 'ascending' ? cmp : -cmp
    })

    return result
  })

  // Actions
  async function loadFiles(path?: string, search?: string) {
    loading.value = true
    try {
      const targetPath = path || currentPath.value || '/'
      const response = await fileApi.fetchFiles(targetPath, search)
      files.value = response.files
      auth.value = response.auth
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
      loading.value = false
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
    try {
      await fileApi.deleteFile(currentPath.value, filename)
      ElMessage.success('File deleted successfully')
      await loadFiles()
    } catch (error) {
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
    setSort
  }
})