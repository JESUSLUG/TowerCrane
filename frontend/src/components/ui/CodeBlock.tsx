import { forwardRef, UIEventHandler } from 'react'
import { cn } from '../../lib/utils'

interface CodeBlockProps {
  content: string
  /** Color + / - / ~ lines like a terraform/tofu plan. */
  diff?: boolean
  className?: string
  onScroll?: UIEventHandler<HTMLPreElement>
}

function lineClass(line: string): string | undefined {
  const t = line.trimStart()
  if (t.startsWith('-/+') || t.startsWith('+/-')) return 'text-attention-ink'
  if (t.startsWith('+')) return 'text-success-ink'
  if (t.startsWith('-')) return 'text-danger-ink'
  if (t.startsWith('~')) return 'text-attention-ink'
  if (t.startsWith('Error') || t.startsWith('│ Error')) return 'text-danger-ink font-semibold'
  return undefined
}

/** Monospace output (plans, logs, manifests, HCL). */
export const CodeBlock = forwardRef<HTMLPreElement, CodeBlockProps>(({ content, diff, className, onScroll }, ref) => (
  <pre
    ref={ref}
    onScroll={onScroll}
    className={cn(
      'max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-control border border-line bg-subtle p-3 font-mono text-xs leading-relaxed text-ink',
      className
    )}
  >
    {diff
      ? content.split('\n').map((line, i) => (
          <span key={i} className={cn('block', lineClass(line))}>
            {line || ' '}
          </span>
        ))
      : content}
  </pre>
))
CodeBlock.displayName = 'CodeBlock'

/** Reads "Plan: 3 to add, 1 to change, 0 to destroy." from tofu/terraform output. */
export function parsePlanSummary(plan?: string): { add: number; change: number; destroy: number } | null {
  const m = plan?.match(/Plan:\s*(\d+) to add,\s*(\d+) to change,\s*(\d+) to destroy/)
  return m ? { add: Number(m[1]), change: Number(m[2]), destroy: Number(m[3]) } : null
}

/** +add ~change −destroy chips; destroy turns red when non-zero. */
export function PlanSummary({ plan, className }: { plan?: string; className?: string }) {
  const s = parsePlanSummary(plan)
  if (!s) return null
  return (
    <div className={cn('flex flex-wrap gap-1.5 font-mono text-xs font-semibold', className)}>
      <span className="rounded-md bg-success-soft px-2 py-1 text-success-ink">+{s.add} add</span>
      <span className="rounded-md bg-attention-soft px-2 py-1 text-attention-ink">~{s.change} change</span>
      <span className={cn('rounded-md px-2 py-1', s.destroy > 0 ? 'bg-danger text-white' : 'bg-subtle text-muted')}>
        −{s.destroy} destroy
      </span>
    </div>
  )
}
