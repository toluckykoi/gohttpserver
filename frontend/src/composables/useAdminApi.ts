import type {
  AdminProfile,
  WebdavAccountWithPassword,
  WebdavStatus,
} from '@/types'

/** 错误以 { ok: false, error } 返回而非抛出，调用方无需 try/catch 即可解构。 */
type ApiResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: string }

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
  async function fetchProfile(): Promise<ApiResult<AdminProfile>> {
    return send(
      '/-/api/admin/profile',
      { method: 'GET' },
      async (res) => await res.json() as AdminProfile,
    )
  }

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

  async function fetchWebdavStatus(): Promise<ApiResult<WebdavStatus>> {
    return send(
      '/-/api/admin/webdav/status',
      { method: 'GET' },
      async (res) => await res.json() as WebdavStatus,
    )
  }

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

  async function createWebdavAccount(req: {
    remark: string
    root_path: string
    readonly: boolean
    protect_system_files: boolean
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

  async function deleteWebdavAccount(id: string): Promise<ApiResult<null>> {
    return send(
      `/-/api/admin/webdav/accounts/${encodeURIComponent(id)}`,
      { method: 'DELETE' },
      async () => null,
    )
  }

  async function resetWebdavPassword(id: string): Promise<ApiResult<{ password: string }>> {
    return send(
      `/-/api/admin/webdav/accounts/${encodeURIComponent(id)}/reset-password`,
      { method: 'POST' },
      async (res) => await res.json() as { password: string },
    )
  }

  async function fetchFRPStatus(): Promise<ApiResult<{
    enabled: boolean
    running: boolean
    config?: {
      serverAddr: string
      serverPort: number
      authToken: string
      authTokenSet: boolean
      proxyName: string
      registeredProxyName?: string
      proxyType: string
      localIP: string
      localPort: number
      remotePort: number
      proxyProtocolVersion: string
    }
  }>> {
    return send(
      '/-/api/admin/frp/status',
      { method: 'GET' },
      async (res) => await res.json() as any,
    )
  }

  /** 按需拉取 FRP auth token 明文（status 接口只返回 authTokenSet）。 */
  async function fetchFRPAuthToken(): Promise<ApiResult<{ authToken: string; authTokenSet: boolean }>> {
    return send(
      '/-/api/admin/frp/auth-token',
      { method: 'GET' },
      async (res) => await res.json() as { authToken: string; authTokenSet: boolean },
    )
  }

  async function setFRPEnabled(enabled: boolean): Promise<ApiResult<{ enabled: boolean }>> {
    return send(
      '/-/api/admin/frp/enabled',
      {
        method: 'PUT',
        body: JSON.stringify({ enabled }),
      },
      async (res) => await res.json() as { enabled: boolean },
    )
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
  }): Promise<ApiResult<null>> {
    return send(
      '/-/api/admin/frp/config',
      {
        method: 'PUT',
        body: JSON.stringify(req),
      },
      async () => null,
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
    fetchFRPStatus,
    fetchFRPAuthToken,
    setFRPEnabled,
    updateFRPConfig,
  }
}

/** 本地生成随机密码，用于预填创建账号表单。 */
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
