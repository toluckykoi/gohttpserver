export interface FileItem {
    name: string
    path: string
    type: 'file' | 'dir'
    size: number
    mtime: number
}

export interface AuthInfo {
    upload: boolean
    delete: boolean
    edit: boolean
    users?: UserControl[]
}

export interface UserControl {
    email: string
    upload: boolean
    delete: boolean
    edit: boolean
    token: string
}

export interface UserInfo {
    email?: string
    name: string
    /** Authentication provider. "login" = local username/password. */
    provider?: string
}

export interface FileInfo {
    name: string
    type: string
    size: number
    path: string
    mtime: number
    extra?: any
    // Hex-encoded digests from the server's `?op=info` response.
    // Populated only for files under the server's hash-size cap;
    // absent otherwise (and absent for directories).
    md5?: string
    sha256?: string
}

export interface ApkInfo {
    packageName: string
    mainActivity: string
    version: {
        code: number
        name: string
    }
}

export interface SystemInfo {
    version: string
}

/** WebDAV account as returned by /-/api/admin/webdav/status.
 *  Password hash is never included — plaintext is only present in the
 *  create / reset-password responses (WebdavAccountWithPassword). */
export interface WebdavAccount {
    id: string
    remark: string
    username: string
    root_path: string
    readonly: boolean
    protect_system_files: boolean
    /** Storage cap in bytes. 0 = unlimited. */
    quota_bytes: number
    /** Server-computed current usage in bytes. Refreshed on every
     *  /status call; for live updates the operator can trigger a
     *  recalculate via POST /-/api/admin/webdav/recalculate-usage. */
    used_bytes: number
    created_at: number
    updated_at: number
}

/** Response shape from POST /-/api/admin/webdav/accounts and
 *  POST /-/api/admin/webdav/accounts/:id/reset-password. Plaintext
 *  password is shown to the operator exactly once. */
export interface WebdavAccountWithPassword extends WebdavAccount {
    password: string
}

/** WebDAV server status + account list, from GET /-/api/admin/webdav/status. */
export interface WebdavStatus {
    enabled: boolean
    accounts: WebdavAccount[]
    webdav_url: string
}

/** Current user profile, from GET /-/api/admin/profile. */
export interface AdminProfile {
    username: string
    provider: string
    version: string
}