import type {
  AdminProfile,
  WebdavAccountWithPassword,
  WebdavStatus,
} from '@/types'

/** Result type for admin API calls. Errors are returned as { ok: false,
 *  error } rather than thrown so callers can destructure without
 *  try/catch — matches the convention used by useFileApi.ts. */
type ApiResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: string }

/** Helper: turns a fetch failure into a structured ApiResult. */
async function send<T>(
  url: string,
  init: RequestInit,
  parse: (res: Response) => Promise<T>
): Promise<ApiResult<T>> {
  try {
    const res = await fetch(url, {
      ...init,
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        ...(init.headers || {}),
      },
    })
    if (!res.ok) {
      const text = await res.text().catch(() => '')
      if (res.status === 401) {
        return { ok: false, error: 'unauthorized' }
      }
      return { ok: false, error: text || `HTTP ${res.status}` }
    }
    return { ok: true, data: await parse(res) }
  } catch (err) {
    console.error('admin api failed', url, err)
    return { ok: false, error: 'network' }
  }
}

export function useAdminApi() {
  /** Fetch the current user's profile + version. */
  async function fetchProfile(): Promise<ApiResult<AdminProfile>> {
    return send(
      '/-/api/admin/profile',
      { method: 'GET' },
      async (res) => await res.json() as AdminProfile,
    )
  }

  /** Change the login username. Gated by the authenticated session
   *  itself — no current password required. */
  async function changeUsername(
    newUsername: string,
  ): Promise<ApiResult<{ username: string }>> {
    return send(
      '/-/api/admin/profile/username',
      {
        method: 'PUT',
        body: JSON.stringify({
          new_username: newUsername,
        }),
      },
      async (res) => await res.json() as { username: string },
    )
  }

  /** Change the login password. Re-exposed here under the admin
   *  namespace; the old /-/api/auth/password endpoint still works. */
  async function changePassword(
    oldPassword: string,
    newPassword: string,
  ): Promise<ApiResult<null>> {
    return send(
      '/-/api/admin/profile/password',
      {
        method: 'PUT',
        body: JSON.stringify({ old: oldPassword, new: newPassword }),
      },
      async () => null,
    )
  }

  /** Fetch WebDAV server status + account list in one round-trip. */
  async function fetchWebdavStatus(): Promise<ApiResult<WebdavStatus>> {
    return send(
      '/-/api/admin/webdav/status',
      { method: 'GET' },
      async (res) => await res.json() as WebdavStatus,
    )
  }

  /** Enable or disable the WebDAV server. */
  async function setWebdavEnabled(enabled: boolean): Promise<ApiResult<{ enabled: boolean }>> {
    return send(
      '/-/api/admin/webdav/enabled',
      {
        method: 'PUT',
        body: JSON.stringify({ enabled }),
      },
      async (res) => await res.json() as { enabled: boolean },
    )
  }

  /** Create a new WebDAV account. Returns the account + the plaintext
   *  password, which the UI must show exactly once. */
  async function createWebdavAccount(req: {
    remark: string
    root_path: string
    readonly: boolean
    protect_system_files: boolean
    /** Storage cap in bytes; 0 = unlimited. */
    quota_bytes: number
  }): Promise<ApiResult<WebdavAccountWithPassword>> {
    return send(
      '/-/api/admin/webdav/accounts',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
      async (res) => await res.json() as WebdavAccountWithPassword,
    )
  }

  /** Partial update of an existing account. Pass undefined for fields
   *  you don't want to change. Pass quota_bytes=0 to make the account
   *  unlimited; pass a positive value to apply a cap. */
  async function updateWebdavAccount(
    id: string,
    req: {
      remark?: string
      root_path?: string
      readonly?: boolean
      protect_system_files?: boolean
      quota_bytes?: number
    },
  ): Promise<ApiResult<null>> {
    return send(
      `/-/api/admin/webdav/accounts/${encodeURIComponent(id)}`,
      {
        method: 'PUT',
        body: JSON.stringify(req),
      },
      async () => null,
    )
  }

  /** Force-recalculate the byte counter for every WebDAV account by
   *  walking each account's chroot. Mirrors the Cloudreve admin
   *  "calibrate storage" button (inventory/user.go CalculateStorage).
   *  Returns per-account results with the new used_bytes. */
  async function recalculateWebdavUsage(): Promise<ApiResult<{
    success: boolean
    results: Array<{ id: string; ok: boolean; used_bytes: number; error?: string }>
  }>> {
    return send(
      '/-/api/admin/webdav/recalculate-usage',
      { method: 'POST' },
      async (res) => await res.json() as {
        success: boolean
        results: Array<{ id: string; ok: boolean; used_bytes: number; error?: string }>
      },
    )
  }

  /** Delete a WebDAV account. Idempotent. */
  async function deleteWebdavAccount(id: string): Promise<ApiResult<null>> {
    return send(
      `/-/api/admin/webdav/accounts/${encodeURIComponent(id)}`,
      { method: 'DELETE' },
      async () => null,
    )
  }

  /** Reset a WebDAV account's password. Returns the new plaintext
   *  password — same "show once" contract as createWebdavAccount. */
  async function resetWebdavPassword(id: string): Promise<ApiResult<{ password: string }>> {
    return send(
      `/-/api/admin/webdav/accounts/${encodeURIComponent(id)}/reset-password`,
      { method: 'POST' },
      async (res) => await res.json() as { password: string },
    )
  }

  return {
    fetchProfile,
    changeUsername,
    changePassword,
    fetchWebdavStatus,
    setWebdavEnabled,
    createWebdavAccount,
    updateWebdavAccount,
    deleteWebdavAccount,
    resetWebdavPassword,
    recalculateWebdavUsage,
  }
}

/** Local-only helper: generate a random 10-char password on the client.
 *  Used to pre-fill the create-account form so the user can preview the
 *  password before submitting — the server is the source of truth and
 *  will return its own generated password in the response. */
export function previewRandomPassword(length = 10): string {
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
  const buf = new Uint8Array(length)
  crypto.getRandomValues(buf)
  let out = ''
  for (let i = 0; i < length; i++) {
    out += alphabet[buf[i] % alphabet.length]
  }
  return out
}
