import { Component, type ErrorInfo, type ReactNode } from 'react'

type AppErrorBoundaryState = {
  error: Error | null
  componentStack: string
}

const CHUNK_RELOAD_STORAGE_KEY = 'dbre-maestro:chunk-reload-attempted'
let chunkReloadRequested = false

function formatUnknownError(error: unknown) {
  if (error instanceof Error) {
    return error
  }

  return new Error(typeof error === 'string' ? error : 'An unexpected frontend error occurred')
}

function isDynamicImportLoadError(error: Error) {
  return /failed to fetch dynamically imported module|loading chunk|importing a module script failed/i.test(error.message)
}

function maybeReloadForDynamicImportError(error: Error) {
  if (!isDynamicImportLoadError(error)) {
    return false
  }

  try {
    if (window.sessionStorage.getItem(CHUNK_RELOAD_STORAGE_KEY) === '1') {
      return false
    }
    window.sessionStorage.setItem(CHUNK_RELOAD_STORAGE_KEY, '1')
  } catch {
    return false
  }

  window.location.reload()
  chunkReloadRequested = true
  return true
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
  return (
    <div className="min-h-screen bg-page px-4 py-8 sm:px-6">
      <div className="mx-auto flex min-h-[calc(100vh-4rem)] max-w-4xl items-center justify-center">
        <div className="w-full rounded-card border border-danger/20 bg-panel p-6 shadow-card">
          <p className="text-[11px] font-bold uppercase tracking-[0.24em] text-danger">Frontend Error</p>
          <h1 className="mt-2 font-display text-2xl font-black tracking-tight text-ink">{title}</h1>
          <p className="mt-3 text-sm text-muted">This page did not render correctly. Please share the error details below for debugging.</p>

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
    if (chunkReloadRequested && isDynamicImportLoadError(error)) {
      return
    }
    this.setState({
      error,
      componentStack: info.componentStack ?? '',
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

  private handleErrorEvent = (event: ErrorEvent) => {
    const error = formatUnknownError(event.error ?? event.message)
    if (maybeReloadForDynamicImportError(error)) {
      return
    }
    this.setState({ error })
  }

  private handleRejectionEvent = (event: PromiseRejectionEvent) => {
    const error = formatUnknownError(event.reason)
    if (maybeReloadForDynamicImportError(error)) {
      return
    }
    this.setState({ error })
  }

  componentDidMount() {
    window.addEventListener('error', this.handleErrorEvent)
    window.addEventListener('unhandledrejection', this.handleRejectionEvent)
  }

  componentWillUnmount() {
    window.removeEventListener('error', this.handleErrorEvent)
    window.removeEventListener('unhandledrejection', this.handleRejectionEvent)
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
