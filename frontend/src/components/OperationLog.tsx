import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { operationAPI } from '../api/operations'
import { apiError } from '../api/admin'
import type { Operation } from '../store/operationStore'
import { operationStatus } from '../lib/status'
import { StatusBadge } from './ui/StatusBadge'
import { CodeBlock } from './ui/CodeBlock'

/**
 * Live console of one operation (plan or apply): the terragrunt/tofu output
 * as it runs, refreshed every 2 seconds until the operation finishes.
 */
export function OperationLog({ operationId, title }: { operationId: string; title: string }) {
  const [op, setOp] = useState<Operation | null>(null)
  const [error, setError] = useState<string | null>(null)
  const pre = useRef<HTMLPreElement>(null)
  const stickToBottom = useRef(true)

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const load = async () => {
      try {
        const next = await operationAPI.getOperation(operationId)
        if (cancelled) return
        setOp(next)
        setError(null)
        if (next.status === 'pending' || next.status === 'running') {
          timer = setTimeout(load, 2000)
        }
      } catch (err) {
        if (!cancelled) {
          setError(apiError(err, 'Failed to load the log'))
          timer = setTimeout(load, 5000)
        }
      }
    }
    load()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [operationId])

  // Follow the output while the user is at the bottom.
  useEffect(() => {
    const el = pre.current
    if (el && stickToBottom.current) el.scrollTop = el.scrollHeight
  }, [op?.log])

  const running = op?.status === 'pending' || op?.status === 'running'

  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="font-semibold uppercase tracking-wide text-muted">{title}</span>
        {op && <StatusBadge {...operationStatus[op.status]} />}
        {running && <Loader2 className="h-3 w-3 animate-spin text-primary" aria-hidden="true" />}
        {/* The operation's own Application page (its Logs and Operations); old operations without one just show the id. */}
        {op?.application ? (
          <Link
            to={`/projects/${op.repository_id}/apps/${encodeURIComponent(op.application)}`}
            className="ml-auto font-mono text-muted hover:text-primary"
            title="Open this Application"
          >
            {operationId.slice(0, 8)}
          </Link>
        ) : (
          <span className="ml-auto font-mono text-muted">{operationId.slice(0, 8)}</span>
        )}
      </div>
      {error && <p className="text-xs text-danger">{error}</p>}
      <CodeBlock
        ref={pre}
        diff
        onScroll={(e) => {
          const el = e.currentTarget
          stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
        }}
        content={op ? op.log?.trim() || (running ? 'Starting…' : 'No output') : 'Loading…'}
      />
      {op?.error && op.status === 'failed' && <p className="whitespace-pre-wrap break-words text-xs text-danger">{op.error}</p>}
    </div>
  )
}
