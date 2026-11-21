import { Component, ErrorInfo, ReactNode } from 'react'
import { Alert } from './ui/Alert'

/**
 * Shows a page's error instead of a blank screen, so a broken view is
 * visible (and reportable) without opening the browser console.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('TowerCrane UI error:', error, info.componentStack)
  }

  render() {
    const { error } = this.state
    if (!error) return this.props.children
    return (
      <div className="mx-auto max-w-3xl space-y-4 p-6">
        <Alert>
          <p className="font-semibold">This page failed to render.</p>
          <p className="mt-1 font-mono text-xs">{error.message}</p>
          {error.stack && (
            <pre className="mt-2 max-h-60 overflow-auto whitespace-pre-wrap font-mono text-[11px] opacity-80">{error.stack}</pre>
          )}
        </Alert>
        <button
          type="button"
          className="text-sm font-semibold text-primary hover:underline"
          onClick={() => {
            this.setState({ error: null })
            window.location.reload()
          }}
        >
          Reload
        </button>
      </div>
    )
  }
}
