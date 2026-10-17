import { ref, watch } from 'vue'

const themeKey = 'gohttpserver-theme'
// 'white' 为默认；'dark' 是真正的深色模式。
const availableThemes = ['white', 'black', 'green', 'cyan', 'dark'] as const
type Theme = typeof availableThemes[number]

// 已保存的偏好优先，否则回退到 'white'。
function initialTheme(): Theme {
  const stored = localStorage.getItem(themeKey) as Theme | null
  if (stored && availableThemes.includes(stored)) return stored
  return 'white'
}

const currentTheme = ref<Theme>(initialTheme())

// theme 类挂在 <html> 上并自动保持同步。
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