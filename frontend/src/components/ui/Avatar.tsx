import { cn } from '../../lib/utils'

/** GitHub avatar, or the first letter of the name on a soft purple circle. */
export function Avatar({ src, name, className }: { src?: string; name: string; className?: string }) {
  if (src) return <img src={src} alt="" className={cn('h-8 w-8 flex-shrink-0 rounded-full', className)} />
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-primary-soft text-xs font-semibold text-primary-ink',
        className
      )}
    >
      {(name || '?').charAt(0).toUpperCase()}
    </span>
  )
}
