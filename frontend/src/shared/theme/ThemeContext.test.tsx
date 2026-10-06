import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ThemeProvider, useTheme } from '@/shared/theme/ThemeContext'

function mockSystemTheme(initialDark: boolean) {
  let listener: ((event: MediaQueryListEvent) => void) | undefined
  const mediaQuery = {
    matches: initialDark,
    media: '(prefers-color-scheme: dark)',
    addEventListener: vi.fn((_event: string, nextListener: (event: MediaQueryListEvent) => void) => { listener = nextListener }),
    removeEventListener: vi.fn(),
  }
  vi.stubGlobal('matchMedia', vi.fn(() => mediaQuery))
  return {
    change(matches: boolean) {
      mediaQuery.matches = matches
      listener?.({ matches } as MediaQueryListEvent)
    },
  }
}

function mockLocalStorage() {
  const values = new Map<string, string>()
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
    clear: () => values.clear(),
    key: (index: number) => Array.from(values.keys())[index] ?? null,
    get length() { return values.size },
  } satisfies Storage
  Object.defineProperty(window, 'localStorage', { configurable: true, value: storage })
  return storage
}

describe('ThemeProvider', () => {
  afterEach(() => {
    document.documentElement.classList.remove('dark')
    delete document.documentElement.dataset.theme
    Object.defineProperty(window, 'localStorage', { configurable: true, value: undefined })
    vi.unstubAllGlobals()
  })

  it('follows system changes and persists an explicit mode override', () => {
    const storage = mockLocalStorage()
    const systemTheme = mockSystemTheme(false)
    const { result } = renderHook(() => useTheme(), { wrapper: ThemeProvider })

    expect(result.current).toMatchObject({ mode: 'system', resolvedMode: 'light' })
    expect(document.documentElement).not.toHaveClass('dark')

    act(() => systemTheme.change(true))
    expect(result.current.resolvedMode).toBe('dark')
    expect(document.documentElement).toHaveClass('dark')

    act(() => result.current.setMode('light'))
    expect(result.current).toMatchObject({ mode: 'light', resolvedMode: 'light' })
    expect(storage.getItem('dbre-theme-mode')).toBe('light')
    expect(document.documentElement).not.toHaveClass('dark')
  })

  it('applies and persists a color preset without changing the display mode', () => {
    const storage = mockLocalStorage()
    storage.setItem('dbre-theme-mode', 'dark')
    const { result } = renderHook(() => useTheme(), { wrapper: ThemeProvider })

    act(() => result.current.setPreset('ocean'))

    expect(result.current).toMatchObject({ mode: 'dark', preset: 'ocean', resolvedMode: 'dark' })
    expect(document.documentElement).toHaveAttribute('data-theme', 'ocean')
    expect(storage.getItem('dbre-theme-preset')).toBe('ocean')
  })

  it('falls back to the default preset when storage contains an unknown value', () => {
    const storage = mockLocalStorage()
    storage.setItem('dbre-theme-preset', 'unknown')

    const { result } = renderHook(() => useTheme(), { wrapper: ThemeProvider })

    expect(result.current.preset).toBe('default')
    expect(document.documentElement).toHaveAttribute('data-theme', 'default')
  })

})
