import { ref } from 'vue'

const themeKey = 'gohttpserver-theme'
const defaultTheme = 'white'
const availableThemes = ['white', 'black', 'green', 'cyan'] as const

type Theme = typeof availableThemes[number]

export function useTheme() {
  const currentTheme = ref<Theme>(
    (localStorage.getItem(themeKey) as Theme) || defaultTheme
  )

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