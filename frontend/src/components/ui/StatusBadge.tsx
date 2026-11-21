import { cn } from '../../lib/utils'
import type { StatusInfo, StatusState } from '../../lib/status'

const stateClasses: Record<StatusState, { badge: string; dot: string }> = {
  ok: { badge: 'bg-success-soft text-success-ink', dot: 'bg-success' },
  attention: { badge: 'bg-attention-soft text-attention-ink', dot: 'bg-attention-hover' },
  progress: { badge: 'bg-primary-soft text-primary-ink', dot: 'bg-primary animate-pulse' },
  error: { badge: 'bg-danger-soft text-danger-ink', dot: 'bg-danger' },
  inert: { badge: 'bg-subtle text-muted', dot: 'bg-muted/60' },
}

/** The state of something (an Application, operation, rollout step): dot + text, never color alone. */
export function StatusBadge({ state, label, className }: StatusInfo & { className?: string }) {
  const cls = stateClasses[state]
  return (
    <span
      className={cn(
        'inline-flex h-6 items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 text-xs font-semibold',
        cls.badge,
        className
      )}
    >
      <span className={cn('h-1.5 w-1.5 rounded-full', cls.dot)} aria-hidden="true" />
      {label}
    </span>
  )
}

/** Just the dot, with the label for screen readers and as a tooltip. */
export function StatusDot({ state, label, className }: StatusInfo & { className?: string }) {
  return (
    <span className={cn('inline-block h-2.5 w-2.5 flex-shrink-0 rounded-full', stateClasses[state].dot, className)} title={label}>
      <span className="sr-only">{label}</span>
    </span>
  )
}
