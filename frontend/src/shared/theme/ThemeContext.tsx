import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

export type ThemeMode = 'light' | 'dark' | 'system'

type ThemeContextValue = {
  mode: ThemeMode
  resolvedMode: Exclude<ThemeMode, 'system'>
  setMode: (mode: ThemeMode) => void
}

const THEME_MODE_STORAGE_KEY = 'dbre-theme-mode'
const SYSTEM_DARK_QUERY = '(prefers-color-scheme: dark)'
const ThemeContext = createContext<ThemeContextValue | null>(null)

function isThemeMode(value: string | null): value is ThemeMode {
  return value === 'light' || value === 'dark' || value === 'system'
}

function storedThemeMode(): ThemeMode {
  try {
    const value = window.localStorage.getItem(THEME_MODE_STORAGE_KEY)
    return isThemeMode(value) ? value : 'system'
  } catch {
    return 'system'
  }
}

function systemPrefersDark() {
  return typeof window.matchMedia === 'function' && window.matchMedia(SYSTEM_DARK_QUERY).matches
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(storedThemeMode)
  const [systemDark, setSystemDark] = useState(systemPrefersDark)
  const resolvedMode = mode === 'system' ? (systemDark ? 'dark' : 'light') : mode

  useEffect(() => {
    if (typeof window.matchMedia !== 'function') {
      return
    }
    const mediaQuery = window.matchMedia(SYSTEM_DARK_QUERY)
    const handleChange = (event: MediaQueryListEvent) => setSystemDark(event.matches)
    setSystemDark(mediaQuery.matches)
    mediaQuery.addEventListener('change', handleChange)
    return () => mediaQuery.removeEventListener('change', handleChange)
  }, [])

  useEffect(() => {
    document.documentElement.classList.toggle('dark', resolvedMode === 'dark')
  }, [resolvedMode])

  const value = useMemo<ThemeContextValue>(() => ({
    mode,
    resolvedMode,
    setMode(nextMode) {
      setModeState(nextMode)
      try {
        window.localStorage.setItem(THEME_MODE_STORAGE_KEY, nextMode)
      } catch {
        // Theme selection remains active for this session when storage is unavailable.
      }
    },
  }), [mode, resolvedMode])

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useTheme must be used within ThemeProvider')
  }
  return context
}
