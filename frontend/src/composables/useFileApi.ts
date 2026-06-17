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
    const url = `${encodePath}?op=info`
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
    options?: { filename?: string; unzip?: boolean }
  ): Promise<{ success: boolean; destination?: string; description?: string }> {
    const xhr = new XMLHttpRequest()
    const formData = new FormData()
    formData.append('file', file)
    if (options?.filename) {
      formData.append('filename', options.filename)
    }
    if (options?.unzip) {
      formData.append('unzip', 'true')
    }

    // Attach progress listener BEFORE send() to avoid race condition
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

  function getVideoPlayerUrl(path: string, filename: string): string {
    const encodePath = getEncodePath(filename, path)
    return `/-/video-player/${encodePath}`
  }

  function getIpaInstallUrl(path: string, filename: string): string {
    const encodePath = getEncodePath(filename, path)
    return `/-/ipa/link/${encodePath}`
  }

  return {
    fetchFiles,
    fetchFileInfo,
    uploadFile,
    uploadFileWithProgress,
    createDirectory,
    deleteFile,
    fetchUser,
    fetchSystemInfo,
    downloadFile,
    downloadArchive,
    getVideoPlayerUrl,
    getIpaInstallUrl
  }
}