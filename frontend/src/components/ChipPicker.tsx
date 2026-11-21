import { Check } from 'lucide-react'
import { cn } from '../lib/utils'

/** Toggle chips to pick several items (repositories, cloud accounts...). */
export function ChipPicker({
  options,
  selected,
  onChange,
  disabled,
  empty,
}: {
  options: { id: string; label: string }[]
  selected: string[]
  onChange: (ids: string[]) => void
  disabled?: boolean
  empty?: string
}) {
  const on = new Set(selected)
  if (options.length === 0) return <p className="text-sm text-muted">{empty ?? 'Nothing to choose from'}</p>
  return (
    <div className="flex flex-wrap gap-1.5">
      {options.map((o) => {
        const active = on.has(o.id)
        return (
          <button
            key={o.id}
            type="button"
            disabled={disabled}
            aria-pressed={active}
            onClick={() => onChange(active ? selected.filter((x) => x !== o.id) : [...selected, o.id])}
            className={cn(
              'inline-flex h-7 items-center gap-1 rounded-full border px-3 font-mono text-xs transition-colors',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary disabled:opacity-50',
              active
                ? 'border-primary/40 bg-primary-soft text-primary-ink'
                : 'border-dashed border-line-strong text-muted hover:border-primary/40 hover:text-ink'
            )}
          >
            {active && <Check className="h-3 w-3" aria-hidden="true" />}
            {o.label}
          </button>
        )
      })}
    </div>
  )
}
