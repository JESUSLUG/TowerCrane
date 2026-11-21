import { ComponentType } from 'react'
import { cn } from '../../lib/utils'

export interface TabItem<K extends string> {
  key: K
  label: string
  icon?: ComponentType<{ className?: string }>
  /** Small counter shown next to the label. */
  count?: number
}

interface TabsProps<K extends string> {
  items: readonly TabItem<K>[]
  value: K
  onChange: (key: K) => void
  className?: string
  'aria-label'?: string
}

/** Underlined tabs. One component for every tab bar in the app. */
export function Tabs<K extends string>({ items, value, onChange, className, ...props }: TabsProps<K>) {
  return (
    <div role="tablist" aria-label={props['aria-label']} className={cn('flex gap-1 overflow-x-auto border-b border-line', className)}>
      {items.map(({ key, label, icon: Icon, count }) => {
        const active = key === value
        return (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onChange(key)}
            className={cn(
              '-mb-px flex h-10 flex-shrink-0 items-center gap-2 border-b-2 px-3 text-sm transition-colors',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary',
              active ? 'border-primary font-semibold text-primary' : 'border-transparent font-medium text-muted hover:text-ink'
            )}
          >
            {Icon && <Icon className="h-4 w-4" />}
            {label}
            {count !== undefined && count > 0 && (
              <span className="rounded-full bg-subtle px-1.5 text-xs font-semibold text-muted">{count}</span>
            )}
          </button>
        )
      })}
    </div>
  )
}
