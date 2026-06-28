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
      // Server uses this to recreate directory structure for folder
      // uploads. Without it the server falls back to the flat `filename`
      // / multipart-part name, which collapses the upload into a single
      // directory.
      formData.append('path', options.relativePath)
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

  /**
   * Upload a file and stream NDJSON progress events from the server. The
   * server emits one line per file during unzip, then a final terminal
   * line carrying the success status. The response is sent as
   * `application/x-ndjson` with chunked transfer encoding, and the XHR
   * `progress` event fires per chunk — we use that to incrementally parse
   * each complete line as it arrives.
   *
   * Returns when the terminal `done` line is observed. Rejects on network
   * error, abort, or non-2xx HTTP status.
   */
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
      // See uploadFileWithProgress — same field, same rationale.
      formData.append('path', options.relativePath)
    }
    formData.append('unzip', 'true')

    // Upload progress (request body bytes) — same semantics as
    // uploadFileWithProgress.
    xhr.upload.addEventListener('progress', (e: ProgressEvent) => {
      if (e.lengthComputable) {
        callbacks.onUploadProgress(Math.round((e.loaded / e.total) * 100))
      }
    })

    // NDJSON line parser. The server may emit one line per zip entry; the
    // response body can be large, so we read the response stream in chunks
    // (the XHR `progress` event) and split on `\n` incrementally. An
    // incomplete trailing line is held in `tail` until the next chunk
    // arrives.
    let tail = ''
    let terminalResult: { success: boolean; description?: string } | null = null

    const processBuffer = () => {
      // xhr.responseText grows as the body streams in. The portion we
      // haven't yet examined is `xhr.responseText.slice(tail.length)` —
      // we keep `tail` of any unterminated suffix and prepend it to the
      // new chunk.
      const full = xhr.responseText
      const newChunk = full.slice(tail.length)
      if (!newChunk) return
      const parts = newChunk.split('\n')
      // Last element is either '' (chunk ended on \n) or an incomplete
      // line that we save into `tail` for the next round.
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
          // Skip malformed lines — server is the only writer, so this
          // should not happen in practice.
        }
      }
    }

    return new Promise((resolve, reject) => {
      xhr.addEventListener('progress', processBuffer)

      xhr.addEventListener('load', () => {
        // Drain any final partial line before resolving.
        processBuffer()
        if (xhr.status < 200 || xhr.status >= 300) {
          reject(new Error(`Upload failed: ${xhr.status} ${xhr.statusText}`))
          return
        }
        if (terminalResult) {
          resolve(terminalResult)
        } else {
          // Server returned 2xx but no terminal line — treat as success
          // to match the existing uploadFileWithProgress fallback.
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

  /**
   * Download an arbitrary mix of files and directories as a single zip.
   * Posts a JSON body to the multi-select archive endpoint and pipes the
   * response blob into a hidden anchor to trigger the browser save
   * dialog. The server preserves each entry's basename at the top level
   * of the zip, so the unpacked layout mirrors what the user selected.
   *
   * Network errors and non-2xx responses are surfaced as a rejected
   * promise so the caller can show an error toast.
   */
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
    // Release the blob URL on the next tick so the browser has time to
    // start the download before we tear the reference down.
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

  /**
   * Update (overwrite) the contents of an existing file via PUT.
   * Used by the in-browser editor when the user saves a text file
   * they were editing. The server caps PUT bodies at a few MiB
   * (see hEdit); callers should validate before sending to avoid
   * a 413 round-trip.
   *
   * Body is sent as raw text with Content-Type: text/plain so the
   * server's body-preserving auth check (token via URL query, see
   * hEdit / canUploadSession) is not confused by form parsing.
   */
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

  /**
   * Trigger the server-side offline downloader. POSTs the remote
   * URL and target filename to /-/fetch; the server streams the
   * remote response body to disk under the supplied path. Returns
   * the JSON {success, destination, size, source} from the server.
   *
   * Caller is responsible for showing progress / a spinner — the
   * request blocks for as long as the remote fetch takes. The
   * server applies SSRF protection (rejects loopback / private IPs)
   * so the UI just needs to surface 502/4xx errors as-is.
   */
  async function fetchFromUrl(
    path: string,
    url: string,
    to: string
  ): Promise<{ success: boolean; destination?: string; size?: number; source?: string }> {
    const formData = new FormData()
    formData.append('url', url)
    formData.append('to', to)
    // Send against the directory, not against the destination file —
    // /-/fetch lives at a fixed path on the route var.
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
    downloadFile,
    downloadArchive,
    downloadMulti,
    getVideoPlayerUrl,
    getIpaInstallUrl,
    updateFile,
    fetchFromUrl
  }
}