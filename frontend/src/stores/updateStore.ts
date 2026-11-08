import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import type { UpdateStatus } from '@/types'
import { useUpdateApi, UpdateUnavailableError } from '@/composables/useUpdateApi'

/** 更新对话框打开期间，下载中状态以 1s 轮询刷新进度。 */
const POLL_INTERVAL_MS = 1000

/** /check 返回的 reason 到用户可读文案的映射，供更新 UI 共用。 */
export const UPDATE_REASON_TEXTS: Record<string, string> = {
    up_to_date: '当前已是最新版本',
    client_ahead: '当前版本高于服务端最新版本（内测版）',
    no_matching_artifact: '暂无适用于当前系统的更新包',
    no_published_release: '该渠道尚无已发布的版本',
    new_version: '发现新版本'
}

export const useUpdateStore = defineStore('update', () => {
    const updateApi = useUpdateApi()

    // null 表示功能不可用（后端未注册 / 未登录）。
    const status = ref<UpdateStatus | null>(null)
    const dialogVisible = ref(false)
    // 重启点击后进入"等待服务恢复"阶段，页面随后整页刷新。
    const restarting = ref(false)

    const available = computed(() => {
        const st = status.value
        return !!st && ['available', 'downloading', 'ready', 'applied'].includes(st.state)
    })

    /** 页脚徽标文案：有新版本 / 下载进度 / 待重启，覆盖下载在后台进行的情形。 */
    const badgeText = computed(() => {
        const st = status.value
        if (!st) return ''
        if (st.state === 'ready' || st.state === 'applied') return '待重启更新'
        if (st.state === 'downloading' && st.artifact_size) {
            const pct = Math.min(100, Math.round(((st.downloaded || 0) / st.artifact_size) * 100))
            return `更新中 ${pct}%`
        }
        if (st.state === 'available' && st.release_version) return `新版本 ${st.release_version}`
        return ''
    })

    let pollTimer: number | null = null

    function stopPolling() {
        if (pollTimer !== null) {
            window.clearInterval(pollTimer)
            pollTimer = null
        }
    }

    function startPolling() {
        if (pollTimer !== null) return
        pollTimer = window.setInterval(async () => {
            // 仅在下载中轮询；状态离开后由 watch 关闭定时器。
            try {
                status.value = await updateApi.fetchStatus()
            } catch {
                /* 单次轮询失败不打断下载，下个周期重试 */
            }
        }, POLL_INTERVAL_MS)
    }

    // 轮询生命周期由"正在下载"状态驱动（与对话框是否打开无关，
    // 这样页脚徽标的进度才不会停在旧值）；状态离开 downloading 即停。
    watch(
        () => status.value?.state,
        (state) => {
            if (state === 'downloading') {
                startPolling()
            } else {
                stopPolling()
            }
        },
        { immediate: true }
    )

    /** 拉取一次状态；功能不可用时静默置 null。 */
    async function refresh() {
        try {
            status.value = await updateApi.fetchStatus()
        } catch (err) {
            status.value = null
            if (!(err instanceof UpdateUnavailableError)) {
                console.error('Failed to load update status:', err)
            }
        }
    }

    /** 手动检查更新；错误向上抛给调用方展示。 */
    async function check() {
        status.value = await updateApi.check()
    }

    async function startDownload() {
        status.value = await updateApi.startDownload()
    }

    async function apply() {
        status.value = await updateApi.apply()
    }

    async function restart() {
        restarting.value = true
        try {
            await updateApi.restart()
        } catch (err) {
            restarting.value = false
            throw err
        }
    }

    function openDialog() {
        dialogVisible.value = true
        void refresh()
    }

    return {
        status,
        dialogVisible,
        restarting,
        available,
        badgeText,
        refresh,
        check,
        startDownload,
        apply,
        restart,
        openDialog
    }
})
