/**
 * Robust copy-to-clipboard helper.
 *
 * `navigator.clipboard.writeText()` is the modern API, but it fails silently
 * (with a rejected promise) in several real-world scenarios this app sees:
 *   - Served over plain HTTP on a LAN — the page is not a "secure context",
 *     so the Clipboard API is unavailable.
 *   - Document not focused (e.g. user just opened a modal and the focus is
 *     still on the page body).
 *   - Very large payloads (some browsers reject megabyte-sized writes).
 *   - Older browsers / privacy-focused browsers that disable the API.
 *
 * The fallback path uses a transient `<textarea>` + `document.execCommand`
 * which works in all of those cases. It's deprecated but still universally
 * supported and is exactly what libraries like clipboard.js do under the hood.
 *
 * Returns `true` if the copy succeeded, `false` otherwise.
 */
export async function copyText(text: string): Promise<boolean> {
  if (!text) return false

  // ── 1. Modern Clipboard API (preferred) ────────────────────────────────
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
      // Fall through to the legacy path. The most common cause here is the
      // document not being focused, which we can work around below.
    }
  }

  // ── 2. Legacy execCommand fallback ─────────────────────────────────────
  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  if (typeof document === 'undefined') return false

  // Create a textarea that's visible to the layout but invisible to the user.
  // (Setting `display: none` would prevent some browsers from copying.)
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

  // Preserve the current selection so we can restore it after the copy.
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
