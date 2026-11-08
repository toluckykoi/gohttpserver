import type { UpdateStatus } from '@/types'

/** 401/404 等导致的"功能不可用"，调用方据此隐藏更新入口。 */
export class UpdateUnavailableError extends Error {}

async function request<T>(url: string, method: string): Promise<T> {
    let response: Response
    try {
        response = await fetch(url, { method, credentials: 'same-origin', cache: 'no-store' })
    } catch {
        throw new UpdateUnavailableError('network')
    }
    if (response.status === 401 || response.status === 404) {
        throw new UpdateUnavailableError(`HTTP ${response.status}`)
    }
    const body = await response.json().catch(() => null)
    if (!response.ok) {
        throw new Error(
            (body && typeof body.error === 'string' && body.error) || `HTTP ${response.status}`
        )
    }
    return body as T
}

/** /-/api/update/* 的轻量封装。更新接口均走后端代理（CSP connect-src 'self'），
 *  会话由 cookie 携带；401/404 统一归一为 UpdateUnavailableError。 */
export function useUpdateApi() {
    function fetchStatus(): Promise<UpdateStatus> {
        return request<UpdateStatus>('/-/api/update/status', 'GET')
    }

    /** 同步触发一次检查（后端最长约 15s），响应即最新状态。 */
    function check(): Promise<UpdateStatus> {
        return request<UpdateStatus>('/-/api/update/check', 'POST')
    }

    /** 启动后台下载（202）；冲突时抛出后端错误信息。 */
    function startDownload(): Promise<UpdateStatus> {
        return request<UpdateStatus>('/-/api/update/download', 'POST')
    }

    /** 替换磁盘上的二进制（要求状态 ready）。 */
    function apply(): Promise<UpdateStatus> {
        return request<UpdateStatus>('/-/api/update/apply', 'POST')
    }

    /** 优雅停机并退出进程；依赖 systemd 等守护进程以新版本拉起。 */
    async function restart(): Promise<void> {
        await request<{ message: string }>('/-/api/update/restart', 'POST')
    }

    return { fetchStatus, check, startDownload, apply, restart }
}
