import { HTMLAttributes, ReactNode } from 'react'
import { cn } from '../../lib/utils'

type Highlight = 'attention' | 'danger' | 'primary'

const highlightClasses: Record<Highlight, string> = {
  attention: 'border-attention-line',
  danger: 'border-danger/40',
  primary: 'border-primary/40',
}

interface CardProps extends HTMLAttributes<HTMLDivElement> {
  /** Only for cards that ask for an action (e.g. a plan waiting for a yes). */
  highlight?: Highlight
}

export function Card({ className, highlight, children, ...props }: CardProps) {
  return (
    <div
      className={cn(
        'rounded-xl border bg-surface shadow-card',
        highlight ? highlightClasses[highlight] : 'border-line',
        className
      )}
      {...props}
    >
      {children}
    </div>
  )
}

export function CardHeader({ className, children, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('border-b border-line px-6 py-4', className)} {...props}>
      {children}
    </div>
  )
}

export function CardBody({ className, children, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('px-6 py-5', className)} {...props}>
      {children}
    </div>
  )
}

/** Title + optional description for a CardHeader. */
export function CardTitle({ icon, title, description }: { icon?: ReactNode; title: ReactNode; description?: ReactNode }) {
  return (
    <div className="min-w-0">
      <h2 className="flex items-center gap-2 text-base font-semibold text-ink">
        {icon}
        {title}
      </h2>
      {description && <p className="mt-1 text-sm text-muted">{description}</p>}
    </div>
  )
}
