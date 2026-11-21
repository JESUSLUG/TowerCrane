import { Loader2 } from 'lucide-react'

/** Shown while a cached view is being refreshed in the background. */
export function RefreshingHint({ show }: { show: boolean }) {
  if (!show) return null
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted" role="status">
      <Loader2 className="h-3 w-3 animate-spin text-primary" aria-hidden="true" />
      Updating…
    </span>
  )
}
