import { Component, type ErrorInfo, type ReactNode } from 'react'

type AppErrorBoundaryState = {
  error: Error | null
  componentStack: string
}

const CHUNK_RELOAD_STORAGE_KEY = 'dbre-maestro:chunk-reload-attempted'
const VERSION_RELOAD_STORAGE_KEY = 'dbre-maestro:version-reload-attempted'
const VERSION_POLL_INTERVAL_MS = 5 * 60 * 1000
let chunkReloadRequested = false
let versionReloadRequested = false

// Returns null (rather than falling back to the raw markup) when no /assets/
// references are found — comparing full markup would compare a live,
// hydrated DOM against static server HTML, which never match even when
// nothing changed. A truncated fetch response (network hiccup mid-request)
// can also legitimately produce zero matches; treat that as "can't tell",
// not "definitely different".
function documentAssetSignature(markup: string): string | null {
  const assetPaths = Array.from(markup.matchAll(/\b(?:src|href)=["']([^"']*\/assets\/[^"']+)["']/g))
    .map((match) => match[1])
    .sort()

  return assetPaths.length > 0 ? assetPaths.join('|') : null
}

const initialDocumentAssetSignature = typeof document === 'undefined'
  ? null
  : documentAssetSignature(document.documentElement.outerHTML)

// Identifies which of the self-healing paths triggered a reload — sent as
// telemetry so a stale-bundle reload is distinguishable from a real bug in
// the audit log after the fact, instead of vanishing silently.
type ReloadReason =
  | 'chunk-load-error'
  | 'render-error'
  | 'window-error'
  | 'unhandled-rejection'
  | 'visibility-change'
  | 'periodic-poll'

function reportReloadEvent(reason: ReloadReason, extra: { errorMessage?: string; currentSignature?: string }) {
  if (typeof navigator === 'undefined' || typeof navigator.sendBeacon !== 'function') {
    return
  }
  try {
    const payload = JSON.stringify({
      reason,
      error_message: extra.errorMessage?.slice(0, 500) ?? '',
      route: typeof window !== 'undefined' ? window.location.pathname + window.location.search : '',
      previous_signature: initialDocumentAssetSignature ?? '',
      current_signature: extra.currentSignature ?? '',
    })
    navigator.sendBeacon('/api/frontend/reload-events', new Blob([payload], { type: 'application/json' }))
  } catch {
    // Best-effort only — never let telemetry failure block the reload itself.
  }
}

function formatUnknownError(error: unknown) {
  if (error instanceof Error) {
    return error
  }

  return new Error(typeof error === 'string' ? error : 'An unexpected frontend error occurred')
}

function isDynamicImportLoadError(error: Error) {
  return /failed to fetch dynamically imported module|loading chunk|importing a module script failed/i.test(error.message)
}

function isCurrentOriginURL(rawURL: string) {
  if (typeof window === 'undefined') {
    return false
  }
  if (!/^https?:\/\//i.test(rawURL) && !rawURL.startsWith('/')) {
    return false
  }

  try {
    return new URL(rawURL, window.location.href).origin === window.location.origin
  } catch {
    return false
  }
}

function extractSourceURLs(sourceText: string) {
  return Array.from(sourceText.matchAll(/\b(?:https?:\/\/|chrome-extension:\/\/|moz-extension:\/\/|safari-web-extension:\/\/|ms-browser-extension:\/\/)[^\s)"']+/gi))
    .map((match) => match[0])
}

function shouldHandleWindowLevelError(error: Error, eventSource = '') {
  const sourceText = [eventSource, error.stack ?? ''].filter(Boolean).join('\n')
  if (!sourceText.trim()) {
    return false
  }
  if (/\b(?:chrome-extension|moz-extension|safari-web-extension|ms-browser-extension):\/\//i.test(sourceText)) {
    return false
  }
  if (eventSource && isCurrentOriginURL(eventSource)) {
    return true
  }

  const urls = extractSourceURLs(sourceText)
  if (urls.some((url) => isCurrentOriginURL(url))) {
    return true
  }
  if (urls.length > 0) {
    return false
  }

  return false
}

function maybeReloadForDynamicImportError(error: Error) {
  if (!isDynamicImportLoadError(error)) {
    return false
  }

  const dedupeKey = initialDocumentAssetSignature ?? 'unknown'
  try {
    if (window.sessionStorage.getItem(CHUNK_RELOAD_STORAGE_KEY) === dedupeKey) {
      return false
    }
    window.sessionStorage.setItem(CHUNK_RELOAD_STORAGE_KEY, dedupeKey)
  } catch {
    return false
  }

  reportReloadEvent('chunk-load-error', { errorMessage: error.message })
  window.location.reload()
  chunkReloadRequested = true
  return true
}

async function maybeReloadForVersionMismatch(reason: ReloadReason, error?: Error) {
  if (typeof window === 'undefined' || typeof fetch === 'undefined' || versionReloadRequested) {
    return false
  }
  if (initialDocumentAssetSignature === null) {
    return false
  }

  try {
    const response = await fetch(window.location.href, {
      cache: 'no-store',
      credentials: 'same-origin',
      headers: {
        Accept: 'text/html',
      },
    })
    if (!response.ok) {
      return false
    }

    const currentMarkup = await response.text()
    const currentSignature = documentAssetSignature(currentMarkup)
    if (currentSignature === null || currentSignature === initialDocumentAssetSignature) {
      return false
    }
    if (window.sessionStorage.getItem(VERSION_RELOAD_STORAGE_KEY) === currentSignature) {
      return false
    }

    window.sessionStorage.setItem(VERSION_RELOAD_STORAGE_KEY, currentSignature)
    versionReloadRequested = true
    reportReloadEvent(reason, { errorMessage: error?.message, currentSignature })
    window.location.reload()
    return true
  } catch {
    return false
  }
}

function RuntimeErrorPanel({
  title,
  error,
  detail,
}: {
  title: string
  error: Error
  detail?: string
}) {
  const loadError = isDynamicImportLoadError(error)

  return (
    <div className="min-h-screen bg-page px-4 py-8 sm:px-6">
      <div className="mx-auto flex min-h-[calc(100vh-4rem)] max-w-4xl items-center justify-center">
        <div className="w-full rounded-card border border-danger/20 bg-panel p-6 shadow-card">
          <p className="text-[11px] font-bold uppercase tracking-[0.24em] text-danger">Frontend Error</p>
          <h1 className="mt-2 font-display text-2xl font-black tracking-tight text-ink">{title}</h1>
          <p className="mt-3 text-sm text-muted">
            {loadError
              ? 'The app could not load a frontend module. This can happen during a deployment or when the frontend server is unavailable.'
              : 'This page did not render correctly. Please share the error details below for debugging.'}
          </p>

          <div className="mt-5 rounded-card border border-danger/20 bg-red-50 p-4">
            <p className="font-mono text-sm font-semibold text-danger">{error.name}: {error.message}</p>
            {detail ? (
              <pre className="mt-3 overflow-x-auto whitespace-pre-wrap break-words text-xs text-danger">{detail}</pre>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  )
}

export class AppErrorBoundary extends Component<{ children: ReactNode }, AppErrorBoundaryState> {
  state: AppErrorBoundaryState = {
    error: null,
    componentStack: '',
  }

  static getDerivedStateFromError(error: Error): AppErrorBoundaryState {
    if (typeof window !== 'undefined' && maybeReloadForDynamicImportError(error)) {
      return {
        error: null,
        componentStack: '',
      }
    }

    return {
      error,
      componentStack: '',
    }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    if (isDynamicImportLoadError(error)) {
      if (chunkReloadRequested) {
        return
      }
      this.setState({
        error,
        componentStack: info.componentStack ?? '',
      })
      return
    }

    const componentStack = info.componentStack ?? ''
    void maybeReloadForVersionMismatch('render-error', error).then((reloading) => {
      if (!reloading) {
        this.setState({ error, componentStack })
      }
    })
  }

  render() {
    if (this.state.error) {
      return (
        <RuntimeErrorPanel
          title="Frontend runtime error"
          error={this.state.error}
          detail={this.state.componentStack || undefined}
        />
      )
    }

    return this.props.children
  }
}

type GlobalErrorBoundaryProps = {
  children: ReactNode
}

type GlobalErrorBoundaryState = {
  error: Error | null
}

export class GlobalErrorBoundary extends Component<GlobalErrorBoundaryProps, GlobalErrorBoundaryState> {
  state: GlobalErrorBoundaryState = {
    error: null,
  }

  private versionPollTimer: ReturnType<typeof setInterval> | null = null

  private handleVisibilityChange = () => {
    if (document.visibilityState === 'visible') {
      void maybeReloadForVersionMismatch('visibility-change')
    }
  }

  private handleErrorEvent = (event: ErrorEvent) => {
    const error = formatUnknownError(event.error ?? event.message)
    if (maybeReloadForDynamicImportError(error)) {
      return
    }
    if (!shouldHandleWindowLevelError(error, event.filename)) {
      return
    }
    void maybeReloadForVersionMismatch('window-error', error).then((reloading) => {
      if (!reloading) {
        this.setState({ error })
      }
    })
  }

  private handleRejectionEvent = (event: PromiseRejectionEvent) => {
    const error = formatUnknownError(event.reason)
    if (maybeReloadForDynamicImportError(error)) {
      return
    }
    if (!shouldHandleWindowLevelError(error)) {
      return
    }
    void maybeReloadForVersionMismatch('unhandled-rejection', error).then((reloading) => {
      if (!reloading) {
        this.setState({ error })
      }
    })
  }

  componentDidMount() {
    window.addEventListener('error', this.handleErrorEvent)
    window.addEventListener('unhandledrejection', this.handleRejectionEvent)
    document.addEventListener('visibilitychange', this.handleVisibilityChange)
    this.versionPollTimer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void maybeReloadForVersionMismatch('periodic-poll')
      }
    }, VERSION_POLL_INTERVAL_MS)
  }

  componentWillUnmount() {
    window.removeEventListener('error', this.handleErrorEvent)
    window.removeEventListener('unhandledrejection', this.handleRejectionEvent)
    document.removeEventListener('visibilitychange', this.handleVisibilityChange)
    if (this.versionPollTimer !== null) {
      clearInterval(this.versionPollTimer)
    }
  }

  render() {
    if (this.state.error) {
      return (
        <RuntimeErrorPanel
          title="Frontend initialization failed"
          error={this.state.error}
        />
      )
    }

    return <AppErrorBoundary>{this.props.children}</AppErrorBoundary>
  }
}
