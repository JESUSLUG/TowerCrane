import { Layers } from 'lucide-react'
import { cn } from '../lib/utils'

/**
 * TowerCrane names an Application of a group "<app>@<group>". People read
 * it as the Application's name plus a group tag.
 */
export function splitInstance(name: string): { app: string; group?: string } {
  const at = name.lastIndexOf('@')
  return at > 0 ? { app: name.slice(0, at), group: name.slice(at + 1) } : { app: name }
}

/** The group tag shown next to an Application name. */
export function GroupTag({ group, className }: { group: string; className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex h-5 items-center gap-1 whitespace-nowrap rounded-full bg-primary-soft px-2 font-sans text-[11px] font-semibold text-primary-ink',
        className
      )}
    >
      <Layers className="h-3 w-3" aria-hidden="true" />
      {group}
    </span>
  )
}

/** "ecr [sandbox]" for "ecr@sandbox"; just "ecr" without a group. */
export function InstanceName({ name, className }: { name: string; className?: string }) {
  const { app, group } = splitInstance(name)
  return (
    <span className={cn('inline-flex items-center gap-1.5', className)}>
      <span>{app}</span>
      {group && <GroupTag group={group} />}
    </span>
  )
}

/** Plain-text form for lists ("vpc, ecr"): the group is implied by context. */
export function appNames(names: string[]): string {
  return names.map((n) => splitInstance(n).app).join(', ')
}
