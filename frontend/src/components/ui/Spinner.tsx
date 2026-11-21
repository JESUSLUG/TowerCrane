import { Loader2 } from 'lucide-react'
import { cn } from '../../lib/utils'

interface SpinnerProps {
  size?: 'sm' | 'md' | 'lg'
  className?: string
  label?: string
}

const sizeClasses = {
  sm: 'h-4 w-4',
  md: 'h-8 w-8',
  lg: 'h-12 w-12',
}

export function Spinner({ size = 'md', className, label }: SpinnerProps) {
  return (
    <div role="status" className="flex flex-col items-center justify-center gap-3 py-10 text-muted">
      <Loader2 className={cn('animate-spin text-primary', sizeClasses[size], className)} aria-hidden="true" />
      {label ? <p className="text-sm">{label}</p> : <span className="sr-only">Loading</span>}
    </div>
  )
}
