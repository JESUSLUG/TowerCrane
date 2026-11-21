import { forwardRef, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from 'react'
import { ChevronDown } from 'lucide-react'
import { cn } from '../../lib/utils'

export const inputClass = cn(
  'h-10 w-full rounded-control border border-line-strong bg-surface px-3 text-sm text-ink placeholder-muted/70 transition',
  'focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/25',
  'disabled:cursor-not-allowed disabled:bg-subtle disabled:text-muted'
)

/** A label wrapping its control, with an optional hint underneath. */
export function Field({ label, hint, children, className }: { label: string; hint?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <label className={cn('block', className)}>
      <span className="mb-1.5 block text-sm font-medium text-ink">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-muted">{hint}</span>}
    </label>
  )
}

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement> & { mono?: boolean }>(
  ({ className, mono, ...props }, ref) => <input ref={ref} className={cn(inputClass, mono && 'font-mono', className)} {...props} />
)
Input.displayName = 'Input'

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(
  ({ className, children, ...props }, ref) => (
    <span className={cn('relative block', className)}>
      <select ref={ref} className={cn(inputClass, 'appearance-none pr-9')} {...props}>
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" aria-hidden="true" />
    </span>
  )
)
Select.displayName = 'Select'
