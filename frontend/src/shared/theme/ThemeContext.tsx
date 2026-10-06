import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

export type ThemeMode = 'light' | 'dark' | 'system'
export type ThemePreset = 'default' | 'ocean' | 'forest' | 'amber' | 'amethyst'

export const THEME_PRESETS = [
  { value: 'default', label: 'Default', swatches: ['#18181b', '#f4f4f5', '#d4d4d8', '#ffffff'] },
  { value: 'ocean', label: 'Ocean', swatches: ['#0369a1', '#e0f2fe', '#bae6fd', '#f8fafc'] },
  { value: 'forest', label: 'Forest', swatches: ['#15803d', '#dcfce7', '#bbf7d0', '#fafcfa'] },
  { value: 'amber', label: 'Amber', swatches: ['#9a3412', '#ffedd5', '#fed7aa', '#fffdfa'] },
  { value: 'amethyst', label: 'Amethyst', swatches: ['#6d28d9', '#ede9fe', '#ddd6fe', '#fdfcff'] },
] as const satisfies ReadonlyArray<{ value: ThemePreset; label: string; swatches: readonly string[] }>

type ThemeContextValue = {
  mode: ThemeMode
  resolvedMode: Exclude<ThemeMode, 'system'>
  preset: ThemePreset
  setMode: (mode: ThemeMode) => void
  setPreset: (preset: ThemePreset) => void
}

const THEME_MODE_STORAGE_KEY = 'dbre-theme-mode'
const THEME_PRESET_STORAGE_KEY = 'dbre-theme-preset'
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

function isThemePreset(value: string | null): value is ThemePreset {
  return THEME_PRESETS.some((preset) => preset.value === value)
}

function storedThemePreset(): ThemePreset {
  try {
    const value = window.localStorage.getItem(THEME_PRESET_STORAGE_KEY)
    return isThemePreset(value) ? value : 'default'
  } catch {
    return 'default'
  }
}

function systemPrefersDark() {
  return typeof window.matchMedia === 'function' && window.matchMedia(SYSTEM_DARK_QUERY).matches
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(storedThemeMode)
  const [preset, setPresetState] = useState<ThemePreset>(storedThemePreset)
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

  useEffect(() => {
    document.documentElement.dataset.theme = preset
  }, [preset])

  const value = useMemo<ThemeContextValue>(() => ({
    mode,
    resolvedMode,
    preset,
    setMode(nextMode) {
      setModeState(nextMode)
      try {
        window.localStorage.setItem(THEME_MODE_STORAGE_KEY, nextMode)
      } catch {
        // Theme selection remains active for this session when storage is unavailable.
      }
    },
    setPreset(nextPreset) {
      setPresetState(nextPreset)
      try {
        window.localStorage.setItem(THEME_PRESET_STORAGE_KEY, nextPreset)
      } catch {
        // Theme selection remains active for this session when storage is unavailable.
      }
    },
  }), [mode, preset, resolvedMode])

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const context = useContext(ThemeContext)
  if (!context) {
    throw new Error('useTheme must be used within ThemeProvider')
  }
  return context
}
