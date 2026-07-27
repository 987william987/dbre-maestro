import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const INITIAL_ASSET_PATH = '/assets/index-AAA.js'
const CHANGED_MARKUP_TEXT = '<script type="module" src="/assets/index-BBB.js"></script>'
const UNCHANGED_MARKUP_TEXT = `<script type="module" src="${INITIAL_ASSET_PATH}"></script>`

function BoomOnRender(): never {
  throw new Error('NotFoundError: Failed to execute \'insertBefore\' on \'Node\'')
}

function mockLocationReload() {
  const originalLocation = window.location
  const reload = vi.fn()
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: { ...originalLocation, href: 'http://localhost:3000/sql-editor', reload },
  })
  return {
    reload,
    restore: () => {
      Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    },
  }
}

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: state })
}

function mockFetchReturning(markupText: string) {
  return vi.fn(async () => new Response(markupText, {
    status: 200,
    headers: { 'Content-Type': 'text/html' },
  }))
}

// The module keeps its "already reloaded this session" state in module-level
// variables and derives its baseline signature from document.documentElement
// at import time — both must be reset per test via a fresh module instance.
// A <link> marker (not a body replacement) seeds the baseline signature
// without disturbing document.body, which @testing-library/react's `screen`
// holds a reference to.
async function loadFreshModule() {
  const marker = document.createElement('link')
  marker.rel = 'preload'
  marker.href = INITIAL_ASSET_PATH
  document.head.appendChild(marker)
  vi.resetModules()
  const mod = await import('./AppErrorBoundary')
  document.head.removeChild(marker)
  return mod
}

describe('GlobalErrorBoundary — proactive version-mismatch reload', () => {
  let restoreLocation: (() => void) | undefined

  beforeEach(() => {
    window.sessionStorage.clear()
    setVisibility('visible')
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    restoreLocation?.()
    restoreLocation = undefined
  })

  it('分頁隱藏時，背景輪詢不會打 fetch 也不會 reload', async () => {
    vi.useFakeTimers()
    const { GlobalErrorBoundary } = await loadFreshModule()
    const fetchMock = mockFetchReturning(CHANGED_MARKUP_TEXT)
    vi.stubGlobal('fetch', fetchMock)
    const { reload, restore } = mockLocationReload()
    restoreLocation = restore
    setVisibility('hidden')

    render(<GlobalErrorBoundary><div>ok</div></GlobalErrorBoundary>)
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000)

    expect(fetchMock).not.toHaveBeenCalled()
    expect(reload).not.toHaveBeenCalled()
  })

  it('分頁可見時，背景輪詢偵測到新版本會自動 reload 一次', async () => {
    vi.useFakeTimers()
    const { GlobalErrorBoundary } = await loadFreshModule()
    const fetchMock = mockFetchReturning(CHANGED_MARKUP_TEXT)
    vi.stubGlobal('fetch', fetchMock)
    const { reload, restore } = mockLocationReload()
    restoreLocation = restore

    render(<GlobalErrorBoundary><div>ok</div></GlobalErrorBoundary>)
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('版本沒變時不會 reload —— 避免誤觸發', async () => {
    vi.useFakeTimers()
    const { GlobalErrorBoundary } = await loadFreshModule()
    const fetchMock = mockFetchReturning(UNCHANGED_MARKUP_TEXT)
    vi.stubGlobal('fetch', fetchMock)
    const { reload, restore } = mockLocationReload()
    restoreLocation = restore

    render(<GlobalErrorBoundary><div>ok</div></GlobalErrorBoundary>)
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000)
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000)

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(reload).not.toHaveBeenCalled()
  })

  it('分頁從背景切回前景時，立即檢查一次版本並 reload', async () => {
    const { GlobalErrorBoundary } = await loadFreshModule()
    const fetchMock = mockFetchReturning(CHANGED_MARKUP_TEXT)
    vi.stubGlobal('fetch', fetchMock)
    const { reload, restore } = mockLocationReload()
    restoreLocation = restore

    render(<GlobalErrorBoundary><div>ok</div></GlobalErrorBoundary>)

    setVisibility('visible')
    document.dispatchEvent(new Event('visibilitychange'))

    await vi.waitFor(() => {
      expect(reload).toHaveBeenCalledTimes(1)
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('卸載後停止輪詢，不會再打 fetch', async () => {
    vi.useFakeTimers()
    const { GlobalErrorBoundary } = await loadFreshModule()
    const fetchMock = mockFetchReturning(UNCHANGED_MARKUP_TEXT)
    vi.stubGlobal('fetch', fetchMock)
    const { restore } = mockLocationReload()
    restoreLocation = restore

    const { unmount } = render(<GlobalErrorBoundary><div>ok</div></GlobalErrorBoundary>)
    unmount()
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000)

    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('AppErrorBoundary — 非 chunk-load 的 render 錯誤走版本檢查', () => {
  let restoreLocation: (() => void) | undefined
  let consoleErrorSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    window.sessionStorage.clear()
    setVisibility('visible')
    consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    restoreLocation?.()
    restoreLocation = undefined
    consoleErrorSpy.mockRestore()
  })

  it('偵測到版本更新時會自動 reload（不用等使用者按按鈕）', async () => {
    // getDerivedStateFromError sets error state synchronously (may flash the
    // fallback panel for one frame) before this async check resolves and
    // reloads — that one-frame flash is an accepted tradeoff, not a bug.
    const { AppErrorBoundary } = await loadFreshModule()
    const fetchMock = mockFetchReturning(CHANGED_MARKUP_TEXT)
    vi.stubGlobal('fetch', fetchMock)
    const { reload, restore } = mockLocationReload()
    restoreLocation = restore

    render(
      <AppErrorBoundary>
        <BoomOnRender />
      </AppErrorBoundary>,
    )

    await vi.waitFor(() => {
      expect(reload).toHaveBeenCalledTimes(1)
    })
  })

  it('版本沒變時照常顯示錯誤畫面 —— 真的 bug 不會被吃掉，且沒有 Reload app 按鈕', async () => {
    const { AppErrorBoundary } = await loadFreshModule()
    const fetchMock = mockFetchReturning(UNCHANGED_MARKUP_TEXT)
    vi.stubGlobal('fetch', fetchMock)
    const { reload, restore } = mockLocationReload()
    restoreLocation = restore

    render(
      <AppErrorBoundary>
        <BoomOnRender />
      </AppErrorBoundary>,
    )

    await screen.findByText('Frontend runtime error')
    expect(reload).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: 'Reload app' })).not.toBeInTheDocument()
  })
})
