import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import type { FileItem, AuthInfo, UserInfo } from '@/types'
import { useFileApi } from '@/composables/useFileApi'
import { useTheme } from '@/composables/useTheme'
import { ElMessage } from 'element-plus'

const SORT_STORAGE_KEY = 'gohttpserver_sort'

function loadSortPreference(): { prop: string; order: 'ascending' | 'descending' } {
  try {
    const raw = localStorage.getItem(SORT_STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (['name', 'size', 'mtime'].includes(parsed.prop) &&
        (parsed.order === 'ascending' || parsed.order === 'descending')) {
        return parsed
      }
    }
  } catch { /* ignore */ }
  return { prop: 'name', order: 'ascending' }
}

export const useFileStore = defineStore('file', () => {
  const { currentTheme } = useTheme()
  const fileApi = useFileApi()

  const currentPath = ref('')
  const files = ref<FileItem[]>([])
  const auth = ref<AuthInfo>({ upload: false, delete: false, edit: false })
  const user = ref<UserInfo | null>(null)
  const version = ref('')
  const loading = ref(false)
  const showHidden = ref(false)
  const searchQuery = ref('')
  const savedSort = loadSortPreference()
  const sortProp = ref(savedSort.prop)
  const sortOrder = ref<'ascending' | 'descending'>(savedSort.order)
  // 选中项以 URL 编码后的 path 为键，导航时清空。
  const selectedFiles = ref<Set<string>>(new Set())
  // 仅移动端：每张卡片都显示复选框，点击切换选中而不跳转。
  const selectionMode = ref(false)
  // `user` 填充前 App.vue 一直显示登录路由。
  const loginEnabled = ref(false)
  const loginChecked = ref(false)

  const sortedFiles = computed(() => {
    let result = [...files.value]

    if (!showHidden.value) {
      result = result.filter(f => !f.name.startsWith('.'))
    }

    const prop = sortProp.value
    const order = sortOrder.value

    result.sort((a, b) => {
      const aIsDir = a.type === 'dir' ? 1 : 0
      const bIsDir = b.type === 'dir' ? 1 : 0
      if (aIsDir !== bIsDir) return bIsDir - aIsDir

      let cmp = 0
      if (prop === 'name') {
        cmp = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
      } else if (prop === 'size') {
        cmp = (a.size || 0) - (b.size || 0)
      } else {
        cmp = a.mtime - b.mtime
      }
      // 用名称兜底排序：后端顺序是随机的（Go map 迭代顺序）。
      if (cmp === 0) {
        cmp = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
      }
      return order === 'ascending' ? cmp : -cmp
    })

    return result
  })

  const selectedCount = computed(() => selectedFiles.value.size)
  const isAllSelected = computed(
    () => selectedCount.value > 0 && selectedCount.value === sortedFiles.value.length
  )

  // 目录列表请求序号：慢响应不得覆盖后发起的新视图（旧数据覆盖新目录会
  // 造成界面显示 A 目录、删除/上传却打到 B 目录的错位）。
  let loadFilesSeq = 0

  async function loadFiles(path?: string, search?: string, options: { silent?: boolean } = {}) {
    const seq = ++loadFilesSeq
    if (!options.silent) {
      loading.value = true
    }
    try {
      const targetPath = path || currentPath.value || '/'
      const response = await fileApi.fetchFiles(targetPath, search)
      // 已有更新的请求发出：本次响应作废。
      if (seq !== loadFilesSeq) return
      files.value = response.files
      auth.value = response.auth
      // 切换目录时清空选中；同路径的搜索重跑保留选中。
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
      if (seq !== loadFilesSeq) return
      ElMessage.error(`Failed to load files: ${error}`)
      console.error(error)
    } finally {
      if (seq === loadFilesSeq && !options.silent) {
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

  /** 探测 /-/api/auth/status，判断是否启用了 --login、当前请求是否已有会话。
   *  幂等，可安全地在 App.vue onMounted 中调用。 */
  async function loadLoginStatus() {
    try {
      const status = await fileApi.fetchAuthStatus()
      loginEnabled.value = status.login_enabled
      // 登录已启用且会话有效时服务端会返回用户信息，直接并入 user 状态，
      // 这样 App.vue 不必再维护一份并行的 session 对象。
      if (status.login_enabled && status.authenticated) {
        user.value = { name: status.name || 'admin', provider: 'login' }
      }
    } catch (error) {
      console.error('Failed to load login status:', error)
    } finally {
      loginChecked.value = true
    }
  }

  /** POST /-/login 提交凭证；成功后只记录本地 user，不在这里预取文件列表——
   *  Login.vue 随后会 window.location.replace(target) 硬刷新 SPA，由 App.vue
   *  的 onMounted 发起第一次（也是唯一一次）loadFiles；在此预取会与刷新竞态且白费请求。 */
  async function loginWithCredentials(username: string, password: string) {
    const next = currentPath.value || '/'
    const res = await fileApi.submitLogin(username, password, next)
    if (res.ok) {
      user.value = { name: username, provider: 'login' }
      return { ok: true as const }
    }
    return { ok: false as const, error: res.error }
  }

  /** GET /-/logout 由服务端清掉会话 cookie，随后重置本地认证态，
   *  使 App.vue 下次渲染时挂回登录页。 */
  async function logout() {
    try {
      await fetch('/-/logout', { method: 'GET', credentials: 'same-origin' })
    } catch {
      // 网络失败也照样清本地状态；cookie 可能到下次刷新前仍然有效。
    }
    user.value = null
    files.value = []
  }

  /** 改密码 UI 的 PUT /-/api/auth/password 包装。 */
  async function changePassword(oldPw: string, newPw: string) {
    const res = await fileApi.changePassword(oldPw, newPw)
    return res
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

    // 乐观删除：先本地移除该行，再与服务端对齐，避免等待整目录 GET
    // 时行仍留在原地、让用户以为点击没生效。
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
      // 静默后台同步：吸收并发变更（如其他客户端上传），且不在用户正在看的
      // 列表上再触发一次 loading 转圈。失败可忽略——乐观状态已与服务端确认一致。
      loadFiles(undefined, undefined, { silent: true }).catch(err =>
        console.error('Background refresh after delete failed:', err)
      )
    } catch (error) {
      // 服务端拒绝删除，拉回真实列表还原 UI；静默的理由同成功路径——不在当前
      // 视图上闪出转圈。
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
      sortProp.value = 'name'
      sortOrder.value = 'ascending'
    }
  }

  // 排序偏好持久化到 localStorage，刷新后仍生效。
  watch([sortProp, sortOrder], () => {
    try {
      localStorage.setItem(SORT_STORAGE_KEY, JSON.stringify({
        prop: sortProp.value,
        order: sortOrder.value,
      }))
    } catch { /* ignore */ }
  })

  // 每次变更都整体替换 Set，Vue 响应式才能可靠地感知新内容。
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
    loginEnabled,
    loginChecked,
    loadFiles,
    loadUser,
    loadSystemInfo,
    loadLoginStatus,
    loginWithCredentials,
    logout,
    changePassword,
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