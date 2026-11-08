import { createI18n } from 'vue-i18n'
import type { MessageCompiler } from 'vue-i18n'
import type { MessageContext } from '@intlify/core-base'
import en from './en'
import zh from './zh'

export type AppLocale = 'en' | 'zh'

export const STORAGE_KEY = 'gohttpserver:locale'

export const SUPPORTED_LOCALES: AppLocale[] = ['en', 'zh']

/** 从 localStorage / 浏览器语言推断首选语言；都无法确定时回退 zh（项目主要受众）。 */
function detectLocale(): AppLocale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'en' || saved === 'zh') return saved
  } catch {
    /* 隐私模式忽略 */
  }
  // 无用户显式选择时遵循浏览器语言，中文（含 zh-CN/zh-TW 等）用 zh，其余用 en。
  try {
    for (const lang of navigator.languages ?? [navigator.language]) {
      if (lang?.toLowerCase().startsWith('zh')) return 'zh'
      if (lang?.toLowerCase().startsWith('en')) return 'en'
    }
  } catch {
    /* 某些环境 navigator 受限 */
  }
  return 'zh'
}

const initialLocale = detectLocale()

/**
 * 自定义消息编译器：把 `{param}` 占位符编译成纯字符串插值。
 *
 * vue-i18n v9 默认的消息编译器在浏览器运行时用 `new Function` 编译消息，
 * 会被 Go 服务端严格的 CSP（script-src 不含 'unsafe-eval'）拦截，导致白屏。
 * 本项目的消息只使用 `{param}` 文本占位符（无复数、无 HTML 标记），
 * 因此这里用正文替换实现，编译结果与内置格式完全一致且不依赖 eval。
 */
const messageCompiler: MessageCompiler = (message) => {
  const raw = String(message)
  // 匹配 {name} / { name }，保留原始占位符以便下文替换
  const parts = raw.split(/(\{[^{}]+\})/)
  const extract = (s: string) => /^\{([^{}]+)\}$/.exec(s)?.[1]
  return (ctx: MessageContext) => {
    // 变量名查找：命名参数走 ctx.named()，{0} 列表参数走 ctx.list()。
    // 注意 MessageContext 不把参数暴露为自身属性，直接取 ctx[key] 拿不到值。
    let out = ''
    for (const part of parts) {
      const key = extract(part)?.trim()
      if (key) {
        const v = /^\d+$/.test(key)
          ? ctx.list(Number(key))
          : ctx.named(key)
        out += v == null ? part : String(v)
      } else {
        out += part
      }
    }
    return out
  }
}

const i18n = createI18n({
  legacy: false,
  globalInjection: true,
  locale: initialLocale,
  fallbackLocale: 'en',
  messages: { en, zh },
  messageCompiler,
})

// <html lang> 与页面初始语言保持一致。
document.documentElement.lang = initialLocale === 'zh' ? 'zh-CN' : 'en'

export function setLocale(locale: AppLocale) {
  i18n.global.locale.value = locale as any
  document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en'
  try {
    localStorage.setItem(STORAGE_KEY, locale)
  } catch {
    /* 隐私模式忽略 */
  }
}

export default i18n