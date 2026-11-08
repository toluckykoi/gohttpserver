import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import type { UpdateStatus } from '@/types'
import { useUpdateApi, UpdateUnavailableError } from '@/composables/useUpdateApi'
import i18n from '@/i18n'

/** 更新对话框打开期间，下载中状态以 1s 轮询刷新进度。 */
const POLL_INTERVAL_MS = 1000

/** /check 返回的 reason 到 i18n key 的映射，供更新 UI 翻译（key 见 i18n update.* 域）。 */
export const UPDATE_REASON_TEXTS: Record<string, string> = {
    up_to_date: 'update.runningLatest',
    client_ahead: 'update.latestIsNewer',
    no_matching_artifact: 'update.noArtifact',
    no_published_release: 'update.noRelease',
    new_version: 'update.newVersionAvailable'
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
        if (st.state === 'ready' || st.state === 'applied') {
            return i18n.global.t('update.restartLabel', 'Restart to update')
        }
        if (st.state === 'downloading' && st.artifact_size) {
            const pct = Math.min(100, Math.round(((st.downloaded || 0) / st.artifact_size) * 100))
            return i18n.global.t('update.updatingPercent', { pct }, 'Updating {pct}%')
        }
        if (st.state === 'available' && st.release_version) return `v${st.release_version}`
        return ''
    })

    let pollTimer: number | null = null
    let pollBusy = false // 上一 tick 未返回前跳过本次，防止响应乱序覆盖新状态
    let pollFailures = 0
    let pollGen = 0 // 代次标记：stopPolling 后未决的 tick 不再自我续期

    function stopPolling() {
        pollGen++
        if (pollTimer !== null) {
            window.clearTimeout(pollTimer)
            pollTimer = null
        }
        pollFailures = 0
    }

    function startPolling() {
        if (pollTimer !== null) return
        pollFailures = 0
        const gen = ++pollGen
        const tick = async () => {
            if (!pollBusy) {
                pollBusy = true
                try {
                    status.value = await updateApi.fetchStatus()
                    pollFailures = 0
                } catch {
                    /* 单次轮询失败不打断下载，下个周期重试 */
                    pollFailures++
                } finally {
                    pollBusy = false
                }
            }
            // 服务下线等持续失败时逐级退避（最长 15s），避免 1s 空转；成功后恢复节奏。
            const delay = pollFailures > 0 ? Math.min(POLL_INTERVAL_MS * pollFailures, 15000) : POLL_INTERVAL_MS
            // 期间若被 stopPolling 停掉，则不再续期。
            if (gen === pollGen) {
                pollTimer = window.setTimeout(tick, delay)
            }
        }
        pollTimer = window.setTimeout(tick, POLL_INTERVAL_MS)
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
