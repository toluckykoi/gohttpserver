import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { AdminProfile, WebdavStatus, WebdavAccount } from '@/types'
import { useAdminApi } from '@/composables/useAdminApi'

/** 管理面板级 store，Profile / WebDAV / FRP 三个页签共用。 */
export const useAdminStore = defineStore('admin', () => {
  const api = useAdminApi()

  const profile = ref<AdminProfile | null>(null)
  const profileLoaded = ref(false)

  const webdavStatus = ref<WebdavStatus | null>(null)
  const webdavLoaded = ref(false)
  const webdavLoading = ref(false)

  const frpStatus = ref<{
    enabled: boolean
    running: boolean
    config?: {
      serverAddr: string
      serverPort: number
      authToken: string
      authTokenSet?: boolean
      proxyName: string
      registeredProxyName?: string
      proxyType: string
      localIP: string
      localPort: number
      remotePort: number
      proxyProtocolVersion?: string
    }
  } | null>(null)
  const frpLoaded = ref(false)
  const frpLoading = ref(false)

  // WebDAV 明文密码只在内存中保存：服务端只在创建/重置时返回一次密码，之后只存哈希。
  // 不再写入 localStorage——共享机器上残留全部凭据的风险远大于刷新后需重新创建的代价。
  // 旧版本写入的 webdav_passwords 条目在加载时清除。
  const WEBDAV_PW_KEY = 'webdav_passwords'
  // 清理旧版本残留的 localStorage 密码缓存。
  try {
    localStorage.removeItem(WEBDAV_PW_KEY)
  } catch {
    /* 忽略隐私模式 */
  }
  const webdavPasswords = ref<Record<string, string>>({})
  function setWebdavPassword(id: string, password: string) {
    webdavPasswords.value = { ...webdavPasswords.value, [id]: password }
  }
  function getWebdavPassword(id: string): string {
    return webdavPasswords.value[id] || ''
  }
  function removeWebdavPassword(id: string) {
    if (!(id in webdavPasswords.value)) return
    const next = { ...webdavPasswords.value }
    delete next[id]
    webdavPasswords.value = next
  }

  async function loadProfile() {
    const res = await api.fetchProfile()
    if (res.ok) {
      profile.value = res.data
    } else {
      // 静默失败；错误已在 useAdminApi 里打印。
      profile.value = null
    }
    profileLoaded.value = true
  }

  async function changeUsername(newUsername: string) {
    const res = await api.changeUsername(newUsername)
    if (res.ok) {
      // 同步本地 profile 让头部显示最新名字；App.vue 用 res.data 刷新 fileStore 的 user。
      if (profile.value) {
        profile.value = { ...profile.value, username: res.data.username }
      }
    }
    return res
  }

  async function changePassword(oldPassword: string, newPassword: string) {
    return await api.changePassword(oldPassword, newPassword)
  }

  async function loadWebdavStatus() {
    webdavLoading.value = true
    const res = await api.fetchWebdavStatus()
    if (res.ok) {
      webdavStatus.value = res.data
    } else if (!webdavStatus.value) {
      // 后续轮询失败时保留上一次的良好状态。
      webdavStatus.value = null
    }
    webdavLoaded.value = true
    webdavLoading.value = false
    return res
  }

  async function setWebdavEnabled(enabled: boolean) {
    const res = await api.setWebdavEnabled(enabled)
    if (res.ok && webdavStatus.value) {
      webdavStatus.value = { ...webdavStatus.value, enabled }
    }
    return res
  }

  async function createWebdavAccount(req: {
    remark: string
    root_path: string
    readonly: boolean
    protect_system_files: boolean
    quota_bytes: number
  }) {
    const res = await api.createWebdavAccount(req)
    if (res.ok) {
      // 追加时不带密码：密码单独缓存以便再次查看。
      const { password, ...acc } = res.data
      setWebdavPassword(acc.id, password)
      if (webdavStatus.value) {
        webdavStatus.value = {
          ...webdavStatus.value,
          accounts: [...webdavStatus.value.accounts, acc as WebdavAccount],
        }
      }
    }
    return res
  }

  async function updateWebdavAccount(
    id: string,
    req: {
      remark?: string
      root_path?: string
      readonly?: boolean
      protect_system_files?: boolean
      quota_bytes?: number
    },
  ) {
    const res = await api.updateWebdavAccount(id, req)
    if (res.ok && webdavStatus.value) {
      webdavStatus.value = {
        ...webdavStatus.value,
        accounts: webdavStatus.value.accounts.map((a) =>
          a.id === id
            ? {
                ...a,
                ...req,
                updated_at: Math.floor(Date.now() / 1000),
              }
            : a,
        ),
      }
    }
    return res
  }

  /** 重算所有账号的字节用量。 */
  async function recalculateWebdavUsage() {
    const res = await api.recalculateWebdavUsage()
    if (res.ok && webdavStatus.value) {
      const byID = new Map(res.data.results.map((r) => [r.id, r.used_bytes]))
      webdavStatus.value = {
        ...webdavStatus.value,
        accounts: webdavStatus.value.accounts.map((a) =>
          byID.has(a.id) ? { ...a, used_bytes: byID.get(a.id)! } : a,
        ),
      }
    }
    return res
  }

  async function deleteWebdavAccount(id: string) {
    const res = await api.deleteWebdavAccount(id)
    if (res.ok && webdavStatus.value) {
      webdavStatus.value = {
        ...webdavStatus.value,
        accounts: webdavStatus.value.accounts.filter((a) => a.id !== id),
      }
      // 同时清掉缓存的密码。
      removeWebdavPassword(id)
    }
    return res
  }

  async function resetWebdavPassword(id: string) {
    const res = await api.resetWebdavPassword(id)
    if (res.ok && webdavStatus.value) {
      // 缓存新生成的密码，便于继续查看。
      if (res.data?.password) setWebdavPassword(id, res.data.password)
      webdavStatus.value = {
        ...webdavStatus.value,
        accounts: webdavStatus.value.accounts.map((a) =>
          a.id === id
            ? { ...a, updated_at: Math.floor(Date.now() / 1000) }
            : a,
        ),
      }
    }
    return res
  }

  async function loadFRPStatus() {
    frpLoading.value = true
    const res = await api.fetchFRPStatus()
    if (res.ok) {
      frpStatus.value = res.data
    } else {
      frpStatus.value = null
    }
    frpLoaded.value = true
    frpLoading.value = false
    return res
  }

  async function setFRPEnabled(enabled: boolean) {
    const res = await api.setFRPEnabled(enabled)
    if (res.ok && frpStatus.value) {
      frpStatus.value = { ...frpStatus.value, enabled }
    }
    return res
  }

  async function updateFRPConfig(req: {
    serverAddr: string
    serverPort: number
    authToken: string
    proxyName: string
    proxyType: string
    localIP: string
    localPort: number
    remotePort: number
    proxyProtocolVersion?: string
  }) {
    const res = await api.updateFRPConfig(req)
    if (res.ok && frpStatus.value) {
      // 只回填可编辑字段，绝不回显 authToken——status 接口把它当作只写字段。
      // 保存后 FrpSection 会立刻重新拉取状态，以那次结果为准。
      const prev = frpStatus.value.config
      frpStatus.value = {
        ...frpStatus.value,
        config: {
          ...(prev || {}),
          serverAddr: req.serverAddr,
          serverPort: req.serverPort,
          proxyName: req.proxyName,
          proxyType: req.proxyType,
          localIP: req.localIP,
          localPort: req.localPort,
          remotePort: req.remotePort,
          proxyProtocolVersion: req.proxyProtocolVersion ?? prev?.proxyProtocolVersion ?? '',
          authToken: '',
          authTokenSet: req.authToken !== '' ? true : (prev?.authTokenSet ?? false),
        },
      }
    }
    return res
  }

  /** 按需拉取 auth token 明文（status 轮询从不携带它）。 */
  async function revealFRPAuthToken() {
    return await api.fetchFRPAuthToken()
  }

  const webdavEnabled = computed(() => webdavStatus.value?.enabled ?? false)
  const webdavAccounts = computed<WebdavAccount[]>(
    () => webdavStatus.value?.accounts ?? [],
  )
  const webdavUrl = computed(() => webdavStatus.value?.webdav_url ?? '/dav/')

  return {
    profile,
    profileLoaded,
    loadProfile,
    changeUsername,
    changePassword,
    webdavStatus,
    webdavLoaded,
    webdavLoading,
    webdavEnabled,
    webdavAccounts,
    webdavUrl,
    loadWebdavStatus,
    setWebdavEnabled,
    createWebdavAccount,
    updateWebdavAccount,
    deleteWebdavAccount,
    resetWebdavPassword,
    recalculateWebdavUsage,
    getWebdavPassword,
    frpStatus,
    frpLoaded,
    frpLoading,
    loadFRPStatus,
    setFRPEnabled,
    updateFRPConfig,
    revealFRPAuthToken,
  }
})
