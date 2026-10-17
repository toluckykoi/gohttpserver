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
    /** 认证渠道；"login" 表示本地用户名/密码登录。 */
    provider?: string
}

export interface FileInfo {
    name: string
    type: string
    size: number
    path: string
    mtime: number
    extra?: any
    // 来自 ?op=info 的十六进制摘要；仅文件不超过哈希大小上限时才有。
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

/** WebDAV 账号（绝不包含密码哈希）。 */
export interface WebdavAccount {
    id: string
    remark: string
    username: string
    root_path: string
    readonly: boolean
    protect_system_files: boolean
    /** 容量上限（字节），0 表示不限。 */
    quota_bytes: number
    /** 当前用量（字节），每次 /status 调用时刷新。 */
    used_bytes: number
    created_at: number
    updated_at: number
}

/** 创建账号/重置密码的响应；明文密码仅展示一次。 */
export interface WebdavAccountWithPassword extends WebdavAccount {
    password: string
}

/** WebDAV 服务状态 + 账号列表。 */
export interface WebdavStatus {
    enabled: boolean
    accounts: WebdavAccount[]
    webdav_url: string
}

/** 当前用户资料。 */
export interface AdminProfile {
    username: string
    provider: string
    version: string
}