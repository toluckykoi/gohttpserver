/** 复制到剪贴板；非安全上下文（HTTP 局域网）、文档失焦或旧浏览器时
 *  回退到 execCommand。 */
export async function copyText(text: string): Promise<boolean> {
  if (!text) return false

  if (
    typeof navigator !== 'undefined' &&
    navigator.clipboard &&
    typeof navigator.clipboard.writeText === 'function' &&
    typeof window !== 'undefined' &&
    window.isSecureContext
  ) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // 失败则走旧版回退路径。
    }
  }

  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  if (typeof document === 'undefined') return false

  // 对布局可见但对用户不可见：display:none 在部分浏览器里会导致复制失败。
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.setAttribute('aria-hidden', 'true')
  textarea.style.position = 'fixed'
  textarea.style.top = '0'
  textarea.style.left = '0'
  textarea.style.width = '1px'
  textarea.style.height = '1px'
  textarea.style.padding = '0'
  textarea.style.border = 'none'
  textarea.style.outline = 'none'
  textarea.style.boxShadow = 'none'
  textarea.style.background = 'transparent'
  textarea.style.opacity = '0'
  textarea.style.pointerEvents = 'none'

  document.body.appendChild(textarea)

  // 记下当前选区，复制后恢复。
  const previousActive = document.activeElement as HTMLElement | null
  const previousSelection =
    typeof window.getSelection === 'function'
      ? window.getSelection()?.rangeCount
        ? window.getSelection()?.getRangeAt(0)
        : null
      : null

  let success = false
  try {
    textarea.focus({ preventScroll: true })
    textarea.select()
    textarea.setSelectionRange(0, text.length)
    success = document.execCommand('copy')
  } catch {
    success = false
  } finally {
    document.body.removeChild(textarea)
    if (previousActive && typeof previousActive.focus === 'function') {
      try {
        previousActive.focus({ preventScroll: true })
      } catch {
        /* ignore */
      }
    }
    if (previousSelection && typeof window.getSelection === 'function') {
      const sel = window.getSelection()
      if (sel) {
        sel.removeAllRanges()
        sel.addRange(previousSelection)
      }
    }
  }

  return success
}
