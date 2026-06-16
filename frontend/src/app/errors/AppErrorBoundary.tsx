import { Component, type ErrorInfo, type ReactNode } from 'react'

type AppErrorBoundaryState = {
  error: Error | null
  componentStack: string
}

function formatUnknownError(error: unknown) {
  if (error instanceof Error) {
    return error
  }

  return new Error(typeof error === 'string' ? error : '發生未預期的前端錯誤')
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
          <p className="mt-3 text-sm text-muted">頁面沒有正常 render。請把下面錯誤內容貼回來，我會直接針對它修。</p>

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
    return {
      error,
      componentStack: '',
    }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    this.setState({
      error,
      componentStack: info.componentStack ?? '',
    })
  }

  render() {
    if (this.state.error) {
      return (
        <RuntimeErrorPanel
          title="前端執行時發生錯誤"
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
    this.setState({ error: formatUnknownError(event.error ?? event.message) })
  }

  private handleRejectionEvent = (event: PromiseRejectionEvent) => {
    this.setState({ error: formatUnknownError(event.reason) })
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
          title="前端初始化失敗"
          error={this.state.error}
        />
      )
    }

    return <AppErrorBoundary>{this.props.children}</AppErrorBoundary>
  }
}
