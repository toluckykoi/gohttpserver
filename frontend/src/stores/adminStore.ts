import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { AdminProfile, WebdavStatus, WebdavAccount } from '@/types'
import { useAdminApi } from '@/composables/useAdminApi'

/** Admin-panel-wide store. Holds profile info and the WebDAV account
 *  list so the two tabs (个人中心 / 参数设置) can share state without
 *  each one re-fetching. */
export const useAdminStore = defineStore('admin', () => {
  const api = useAdminApi()

  // ─── Profile tab state ────────────────────────────────────────────
  const profile = ref<AdminProfile | null>(null)
  const profileLoaded = ref(false)

  // ─── WebDAV tab state ─────────────────────────────────────────────
  const webdavStatus = ref<WebdavStatus | null>(null)
  const webdavLoaded = ref(false)
  const webdavLoading = ref(false)

  // Plaintext WebDAV passwords, cached locally. The server only ever
  // returns a password once (at create / reset time) and stores only a
  // hash afterwards — so to let the operator re-view a password in the
  // account's "basic info" panel we stash it in localStorage keyed by
  // account id. Cleared when the account is deleted.
  const WEBDAV_PW_KEY = 'webdav_passwords'
  function loadWebdavPasswords(): Record<string, string> {
    try {
      const raw = localStorage.getItem(WEBDAV_PW_KEY)
      return raw ? JSON.parse(raw) : {}
    } catch {
      return {}
    }
  }
  const webdavPasswords = ref<Record<string, string>>(loadWebdavPasswords())
  function persistWebdavPasswords() {
    try {
      localStorage.setItem(WEBDAV_PW_KEY, JSON.stringify(webdavPasswords.value))
    } catch {
      // Ignore quota / private-mode write failures — the password is
      // still shown for this session via the in-memory ref.
    }
  }
  function setWebdavPassword(id: string, password: string) {
    webdavPasswords.value = { ...webdavPasswords.value, [id]: password }
    persistWebdavPasswords()
  }
  function getWebdavPassword(id: string): string {
    return webdavPasswords.value[id] || ''
  }
  function removeWebdavPassword(id: string) {
    if (!(id in webdavPasswords.value)) return
    const next = { ...webdavPasswords.value }
    delete next[id]
    webdavPasswords.value = next
    persistWebdavPasswords()
  }

  // ─── Profile actions ──────────────────────────────────────────────
  async function loadProfile() {
    const res = await api.fetchProfile()
    if (res.ok) {
      profile.value = res.data
    } else {
      // Silently fail — the panel will show empty fields rather than
      // crash. The error is already logged in useAdminApi.
      profile.value = null
    }
    profileLoaded.value = true
  }

  async function changeUsername(newUsername: string) {
    const res = await api.changeUsername(newUsername)
    if (res.ok) {
      // Update local profile so the header / panel reflects the new
      // name immediately. The fileStore's user object is updated
      // separately by App.vue after this returns (it reads res.data).
      if (profile.value) {
        profile.value = { ...profile.value, username: res.data.username }
      }
    }
    return res
  }

  async function changePassword(oldPassword: string, newPassword: string) {
    return await api.changePassword(oldPassword, newPassword)
  }

  // ─── WebDAV actions ───────────────────────────────────────────────
  async function loadWebdavStatus() {
    webdavLoading.value = true
    const res = await api.fetchWebdavStatus()
    if (res.ok) {
      webdavStatus.value = res.data
    } else {
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
      // Append the new account (without the password) to the local
      // list so the table updates without a full reload. The plaintext
      // password is cached separately so it can be re-viewed later.
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

  /** Force-recalculate byte usage for every account. On success,
   *  merge the returned used_bytes into the local account list so the
   *  UI updates without a full /status round-trip. */
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
      // Drop the cached password too — the account is gone.
      removeWebdavPassword(id)
    }
    return res
  }

  async function resetWebdavPassword(id: string) {
    const res = await api.resetWebdavPassword(id)
    if (res.ok && webdavStatus.value) {
      // Cache the freshly-generated password so it stays viewable.
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

  // ─── Computed convenience accessors ───────────────────────────────
  const webdavEnabled = computed(() => webdavStatus.value?.enabled ?? false)
  const webdavAccounts = computed<WebdavAccount[]>(
    () => webdavStatus.value?.accounts ?? [],
  )
  const webdavUrl = computed(() => webdavStatus.value?.webdav_url ?? '/dav/')

  return {
    // profile
    profile,
    profileLoaded,
    loadProfile,
    changeUsername,
    changePassword,
    // webdav
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
  }
})
