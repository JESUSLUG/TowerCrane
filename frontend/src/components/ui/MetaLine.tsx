import { Fragment, ReactNode } from 'react'
import { cn } from '../../lib/utils'

/**
 * Plain metadata joined by "·" (path · manual · OpenTofu · aws-prod).
 * Metadata is never a badge: color is reserved for status.
 */
export function MetaLine({ items, className }: { items: (ReactNode | false | null | undefined)[]; className?: string }) {
  const shown = items.filter((i) => i !== false && i !== null && i !== undefined && i !== '')
  return (
    <span className={cn('inline-flex flex-wrap items-center gap-x-1.5 font-mono text-xs text-muted', className)}>
      {shown.map((item, i) => (
        <Fragment key={i}>
          {i > 0 && <span aria-hidden="true">·</span>}
          <span>{item}</span>
        </Fragment>
      ))}
    </span>
  )
}
