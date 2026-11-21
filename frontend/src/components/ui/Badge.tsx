import { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

export type Tone = 'neutral' | 'primary' | 'solid' | 'success' | 'attention' | 'danger'

export const toneClasses: Record<Tone, string> = {
  neutral: 'bg-subtle text-muted',
  primary: 'bg-primary-soft text-primary-ink',
  solid: 'bg-primary text-white',
  success: 'bg-success-soft text-success-ink',
  attention: 'bg-attention-soft text-attention-ink',
  danger: 'bg-danger-soft text-danger-ink',
}

interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  tone?: Tone
}

/**
 * A small label (roles, "Emergency", counters). For the state of something
 * use StatusBadge; for metadata (engine, apply mode) use MetaLine.
 */
export function Badge({ tone = 'neutral', className, children, ...props }: BadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex h-6 items-center gap-1 whitespace-nowrap rounded-full px-2.5 text-xs font-semibold',
        toneClasses[tone],
        className
      )}
      {...props}
    >
      {children}
    </span>
  )
}
