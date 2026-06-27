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
    users?: UserControl[]
}

export interface UserControl {
    email: string
    upload: boolean
    delete: boolean
    token: string
}

export interface UserInfo {
    email: string
    name: string
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