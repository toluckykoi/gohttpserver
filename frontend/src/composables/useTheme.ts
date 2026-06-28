import { ref, watch } from 'vue'

const themeKey = 'gohttpserver-theme'
// Available themes. 'white' is the default — a calm, neutral surface
// that suits a file manager. The other three (black, green, cyan)
// remain selectable from the theme picker for users who want a
// different accent colour.
const availableThemes = ['white', 'black', 'green', 'cyan'] as const
type Theme = typeof availableThemes[number]

// Pick the initial theme on first load. Two cases:
//   1. The user has a saved preference → honour it.
//   2. No preference yet → fall back to 'white'. A light surface is
//      the safer default for a file manager (offices, shared
//      screens) and the three accent variants are one click away.
function initialTheme(): Theme {
  const stored = localStorage.getItem(themeKey) as Theme | null
  if (stored && availableThemes.includes(stored)) return stored
  return 'white'
}

const currentTheme = ref<Theme>(initialTheme())

// Apply the theme class to <html> so we can scope variables globally.
// Watching in-place means every setTheme() call stays in sync with
// the DOM — no manual apply() needed in callers.
if (typeof document !== 'undefined') {
  watch(
    currentTheme,
    (theme) => {
      document.documentElement.dataset.theme = theme
    },
    { immediate: true }
  )
}

export function useTheme() {
  function setTheme(theme: Theme) {
    if (availableThemes.includes(theme)) {
      currentTheme.value = theme
      localStorage.setItem(themeKey, theme)
    }
  }

  function toggleTheme() {
    const currentIndex = availableThemes.indexOf(currentTheme.value)
    const nextIndex = (currentIndex + 1) % availableThemes.length
    setTheme(availableThemes[nextIndex])
  }

  return {
    currentTheme,
    availableThemes,
    setTheme,
    toggleTheme
  }
}