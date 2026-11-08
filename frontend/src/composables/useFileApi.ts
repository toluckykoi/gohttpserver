import type { FileItem, AuthInfo, FileInfo, SystemInfo, UserInfo } from '@/types'
import { getEncodePath } from '@/utils/path'

interface FileListResponse {
  files: FileItem[]
  auth: AuthInfo
}

export function useFileApi() {
  async function fetchFiles(path: string, search?: string): Promise<FileListResponse> {
    let url = path
    const params = new URLSearchParams()
    params.append('json', 'true')
    if (search) {
      params.append('search', search)
    }
    const sep = url.includes('?') ? '&' : '?'
    url = url + sep + params.toString()

    const response = await fetch(url, { cache: 'no-store' })
    if (!response.ok) {
      throw new Error(`Failed to fetch files: ${response.statusText}`)
    }
    return await response.json()
  }

  async function fetchFileInfo(path: string, filename: string): Promise<FileInfo> {
    const encodePath = getEncodePath(filename, path)
    const url = `/-/info${encodePath}`
    const response = await fetch(url)
    if (!response.ok) {
      throw new Error(`Failed to fetch file info: ${response.statusText}`)
    }
    return await response.json()
  }

  async function uploadFile(
    path: string,
    file: File,
    options?: { filename?: string; unzip?: boolean }
  ): Promise<{ success: boolean; destination?: string; description?: string }> {
    const formData = new FormData()
    formData.append('file', file)
    if (options?.filename) {
      formData.append('filename', options.filename)
    }
    if (options?.unzip) {
      formData.append('unzip', 'true')
    }

    const response = await fetch(path, {
      method: 'POST',
      body: formData
    })

    if (!response.ok) {
      throw new Error(`Failed to upload file: ${response.statusText}`)
    }

    return await response.json()
  }

  function uploadFileWithProgress(
    path: string,
    file: File,
    onProgress: (percent: number) => void,
    options?: { filename?: string; unzip?: boolean; relativePath?: string }
  ): Promise<{ success: boolean; destination?: string; description?: string }> {
    const xhr = new XMLHttpRequest()
    const formData = new FormData()
    formData.append('file', file)
    if (options?.filename) {
      formData.append('filename', options.filename)
    }
    if (options?.relativePath) {
      // 服务端据此重建上传目录结构。
      formData.append('path', options.relativePath)
    }
    if (options?.unzip) {
      formData.append('unzip', 'true')
    }

    // 必须在 send() 前挂载，否则竞态漏掉早期进度事件。
    xhr.upload.addEventListener('progress', (e: ProgressEvent) => {
      if (e.lengthComputable) {
        onProgress(Math.round((e.loaded / e.total) * 100))
      }
    })

    const promise = new Promise<{ success: boolean; destination?: string; description?: string }>((resolve, reject) => {
      xhr.addEventListener('load', () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          try {
            resolve(JSON.parse(xhr.responseText))
          } catch {
            resolve({ success: true })
          }
        } else {
          reject(new Error(`Upload failed: ${xhr.status} ${xhr.statusText}`))
        }
      })
      xhr.addEventListener('error', () => reject(new Error('Upload failed: network error')))
      xhr.addEventListener('abort', () => reject(new Error('Upload cancelled')))
    })

    xhr.open('POST', path)
    xhr.send(formData)

    return promise
  }

  /** 解压进度以 NDJSON 流式返回：在 XHR progress 事件中逐行解析，遇到 done 行时 resolve。 */
  function uploadFileWithUnzipProgress(
    path: string,
    file: File,
    callbacks: {
      onUploadProgress: (percent: number) => void
      onUnzipProgress: (current: number, total: number, name: string) => void
    },
    options?: { filename?: string; relativePath?: string }
  ): Promise<{ success: boolean; description?: string }> {
    const xhr = new XMLHttpRequest()
    const formData = new FormData()
    formData.append('file', file)
    if (options?.filename) {
      formData.append('filename', options.filename)
    }
    if (options?.relativePath) {
      formData.append('path', options.relativePath)
    }
    formData.append('unzip', 'true')

    xhr.upload.addEventListener('progress', (e: ProgressEvent) => {
      if (e.lengthComputable) {
        callbacks.onUploadProgress(Math.round((e.loaded / e.total) * 100))
      }
    })

    // NDJSON 行解析：增量读取流式响应，未终结的尾行留在 tail 等下一块。
    let tail = ''
    let terminalResult: { success: boolean; description?: string } | null = null

    const processBuffer = () => {
      const full = xhr.responseText
      const newChunk = full.slice(tail.length)
      if (!newChunk) return
      const parts = newChunk.split('\n')
      // 末元素为 ''（块以 \n 结束）或存入 tail 的不完整行。
      tail = parts.pop() ?? ''
      for (const line of parts) {
        const trimmed = line.trim()
        if (!trimmed) continue
        try {
          const data = JSON.parse(trimmed)
          if (data && data.phase === 'unzip') {
            callbacks.onUnzipProgress(
              Number(data.current) || 0,
              Number(data.total) || 0,
              String(data.file ?? '')
            )
          } else if (data && data.phase === 'done') {
            terminalResult = {
              success: Boolean(data.success),
              description: typeof data.description === 'string' ? data.description : undefined
            }
          }
        } catch {
          // 服务端是唯一写入方，坏行直接跳过。
        }
      }
    }

    return new Promise((resolve, reject) => {
      xhr.addEventListener('progress', processBuffer)

      xhr.addEventListener('load', () => {
        // 清掉最后可能残留的半行。
        processBuffer()
        if (xhr.status < 200 || xhr.status >= 300) {
          reject(new Error(`Upload failed: ${xhr.status} ${xhr.statusText}`))
          return
        }
        if (terminalResult) {
          resolve(terminalResult)
        } else {
          // 2xx 却没有终结行时按成功处理。
          resolve({ success: true })
        }
      })
      xhr.addEventListener('error', () => reject(new Error('Upload failed: network error')))
      xhr.addEventListener('abort', () => reject(new Error('Upload cancelled')))

      xhr.open('POST', path)
      xhr.send(formData)
    })
  }

  async function createDirectory(path: string, name: string): Promise<void> {
    const encodePath = getEncodePath(name, path)
    const response = await fetch(encodePath, {
      method: 'POST'
    })
    if (!response.ok) {
      throw new Error(`Failed to create directory: ${response.statusText}`)
    }
  }

  async function deleteFile(path: string, filename: string): Promise<void> {
    const encodePath = getEncodePath(filename, path)
    const response = await fetch(encodePath, {
      method: 'DELETE'
    })
    if (!response.ok) {
      throw new Error(`Failed to delete file: ${response.statusText}`)
    }
  }

  async function fetchUser(): Promise<UserInfo | null> {
    const response = await fetch('/-/user')
    if (!response.ok) {
      throw new Error(`Failed to fetch user: ${response.statusText}`)
    }
    return await response.json()
  }

  /** 探测是否启用 --login 以及当前会话的认证状态。 */
  async function fetchAuthStatus(): Promise<{
    login_enabled: boolean
    authenticated?: boolean
    name?: string
  }> {
    try {
      const response = await fetch('/-/api/auth/status', { cache: 'no-store' })
      if (!response.ok) return { login_enabled: false }
      const data = await response.json()
      return {
        login_enabled: Boolean(data.login_enabled),
        authenticated: data.authenticated === true,
        name: typeof data.name === 'string' ? data.name : undefined
      }
    } catch {
      // 服务不可达时按未启用登录处理。
      return { login_enabled: false }
    }
  }

  /** 用 POST /-/login 提交凭证。redirect:'manual' 让浏览器应用 302 上的 Set-Cookie
   *  （自动跟随重定向时部分浏览器会丢掉它），再用一次认证探测确认结果。 */
  async function submitLogin(
    username: string,
    password: string,
    next: string
  ): Promise<{ ok: true } | { ok: false; error: string }> {
    const form = new FormData()
    form.append('username', username)
    form.append('password', password)
    form.append('next', next)
    let response: Response
    try {
      response = await fetch('/-/login', {
        method: 'POST',
        body: form,
        // 'manual' 阻止自动跟随 302，避免丢失 Set-Cookie。
        redirect: 'manual',
        credentials: 'same-origin'
      })
    } catch {
      return { ok: false, error: 'network' }
    }
    // 3xx 会以 type='opaqueredirect'、status 0 的形式上报。
    if (response.type === 'opaqueredirect') {
      // Set-Cookie 已生效，再用 auth-status 接口确认。
      try {
        const probe = await fetch('/-/api/auth/status', {
          credentials: 'same-origin',
          cache: 'no-store'
        })
        if (probe.ok) {
          const status = await probe.json().catch(() => null)
          if (status && status.authenticated === true) {
            return { ok: true }
          }
        }
      } catch {
        // 探测失败则回落到 invalid_credentials。
      }
      return { ok: false, error: 'invalid_credentials' }
    }
    // 400/401 表示凭证被拒。
    if (response.status === 401 || response.status === 400) {
      return { ok: false, error: 'invalid_credentials' }
    }
    // 其它 2xx：会话 cookie 已下发，下次导航会带上。
    if (response.ok) {
      return { ok: true }
    }
    return { ok: false, error: `HTTP ${response.status}` }
  }

  /** 修改当前用户密码；401 表示未登录。 */
  async function changePassword(
    oldPassword: string,
    newPassword: string
  ): Promise<{ ok: true } | { ok: false; error: string }> {
    try {
      const response = await fetch('/-/api/auth/password', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ old: oldPassword, new: newPassword }),
        credentials: 'same-origin'
      })
      if (response.ok) return { ok: true }
      const text = await response.text().catch(() => '')
      if (response.status === 401) return { ok: false, error: 'unauthorized' }
      if (text) return { ok: false, error: text }
      return { ok: false, error: `HTTP ${response.status}` }
    } catch {
      return { ok: false, error: 'network' }
    }
  }

  async function fetchSystemInfo(): Promise<SystemInfo> {
    const response = await fetch('/-/sysinfo')
    if (!response.ok) {
      throw new Error(`Failed to fetch system info: ${response.statusText}`)
    }
    return await response.json()
  }

  function downloadFile(path: string, filename: string): void {
    const encodePath = getEncodePath(filename, path)
    const sep = encodePath.includes('?') ? '&' : '?'
    window.location.href = `${encodePath}${sep}download=true`
  }

  function downloadArchive(path: string, directoryName: string): void {
    const encodePath = getEncodePath(directoryName, path)
    const sep = encodePath.includes('?') ? '&' : '?'
    window.location.href = `${encodePath}${sep}op=archive`
  }

  /** POST 多选打包接口，把响应 blob 触发浏览器保存；服务端保留每个条目的
   *  顶层 basename。网络错误与非 2xx 均 reject，供调用方弹错误提示。 */
  async function downloadMulti(paths: string[]): Promise<void> {
    if (paths.length === 0) return
    const response = await fetch('/-/zip', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths })
    })
    if (!response.ok) {
      throw new Error(`Failed to download archive: ${response.status} ${response.statusText}`)
    }
    const blob = await response.blob()
    const blobUrl = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = blobUrl
    a.download = 'download.zip'
    document.body.appendChild(a)
    a.click()
    a.remove()
    // 等一个 tick 再释放，给浏览器留出启动下载的时间。
    setTimeout(() => URL.revokeObjectURL(blobUrl), 0)
  }

  function getVideoPlayerUrl(path: string, filename: string): string {
    const encodePath = getEncodePath(filename, path)
    return `/-/video-player/${encodePath}`
  }

  function getIpaInstallUrl(path: string, filename: string): string {
    const encodePath = getEncodePath(filename, path)
    return `/-/ipa/link/${encodePath}`
  }

  /** PUT 覆盖文件内容（编辑器保存用）。服务端对 body 有几 MiB 上限（见 hEdit），
   *  调用方应先校验以免吃 413；body 以 text/plain 原样发送，避免服务端保 body
   *  认证检查（URL query 传 token）被表单解析干扰。 */
  async function updateFile(
    path: string,
    filename: string,
    content: string
  ): Promise<{ success: boolean; destination?: string; size?: number }> {
    const encodePath = getEncodePath(filename, path)
    const response = await fetch(encodePath, {
      method: 'PUT',
      headers: { 'Content-Type': 'text/plain;charset=utf-8' },
      body: content
    })
    if (!response.ok) {
      throw new Error(`Failed to save file: ${response.status} ${response.statusText}`)
    }
    return await response.json()
  }

  /** 触发服务端离线下载：POST 远端 URL 与目标文件名，服务端流式写入磁盘，请求
   *  阻塞到抓取完成；SSRF 防护在服务端（拒绝 loopback/私网 IP），UI 原样展示 502/4xx。 */
  async function fetchFromUrl(
    path: string,
    url: string,
    to: string
  ): Promise<{ success: boolean; destination?: string; size?: number; source?: string }> {
    const formData = new FormData()
    formData.append('url', url)
    formData.append('to', to)
    // 请求打到目录而非目标文件：/-/fetch 挂在路由变量的固定路径上。
    const target = path === '/' || path === '' ? '/' : (path.endsWith('/') ? path : path + '/')
    const response = await fetch(target + '-/fetch', {
      method: 'POST',
      body: formData
    })
    if (!response.ok) {
      throw new Error(`Fetch failed: ${response.status} ${response.statusText}`)
    }
    return await response.json()
  }

  return {
    fetchFiles,
    fetchFileInfo,
    uploadFile,
    uploadFileWithProgress,
    uploadFileWithUnzipProgress,
    createDirectory,
    deleteFile,
    fetchUser,
    fetchSystemInfo,
    fetchAuthStatus,
    submitLogin,
    changePassword,
    downloadFile,
    downloadArchive,
    downloadMulti,
    getVideoPlayerUrl,
    getIpaInstallUrl,
    updateFile,
    fetchFromUrl
  }
}