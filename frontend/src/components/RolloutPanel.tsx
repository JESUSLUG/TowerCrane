import { ReactNode, useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowDown, ArrowUp, Layers, Pause, Play, RotateCcw, Terminal } from 'lucide-react'
import { Card, CardBody, CardHeader, CardTitle } from './ui/Card'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'
import { Alert } from './ui/Alert'
import { StatusBadge } from './ui/StatusBadge'
import { MetaLine } from './ui/MetaLine'
import { apiError } from '../api/admin'
import { rolloutAPI, type Rollout, type RolloutItem, type RolloutItemStatus } from '../api/rollouts'
import { rolloutItemStatus, rolloutStatus } from '../lib/status'
import { cn } from '../lib/utils'
import { OperationLog } from './OperationLog'
import { InstanceName, appNames, splitInstance } from './InstanceName'

const retryable: RolloutItemStatus[] = ['failed', 'blocked', 'rejected', 'discarded', 'skipped', 'unchanged']

/** Explains why a waiting item hasn't started yet. */
function waitingReason(item: RolloutItem, byApp: Map<string, RolloutItem>): string | null {
  if (item.status !== 'waiting') return null
  if (item.paused) return 'Paused by an admin'
  const pending = item.depends_on.filter((d) => {
    const st = byApp.get(d)?.status
    return st !== 'applied' && st !== 'no_changes'
  })
  if (pending.length > 0) return `Waiting for ${appNames(pending)}`
  return 'Next in line'
}

/** "after vpc, ecr · after group sandbox": dependsOn in the group, then promotion. */
function afterText(item: RolloutItem): string {
  const own = splitInstance(item.application).group
  const deps = item.depends_on.filter((d) => splitInstance(d).group === own)
  const others = item.depends_on
    .filter((d) => splitInstance(d).group !== own)
    .map((d) => {
      const { app, group } = splitInstance(d)
      return group ? `${app} (${group})` : app
    })
  return [deps.length > 0 && `after ${appNames(deps)}`, others.length > 0 && `after ${others.join(', ')}`]
    .filter(Boolean)
    .join(' · ')
}

/**
 * The latest rollout of a repository: every Application of the commit in the
 * order TowerCrane runs them. Admins can reorder Applications that are still
 * waiting (never against dependsOn), pause/resume them and retry failures.
 * With group, only that group's Applications are shown; with
 * ungroupedOnly, only the Applications without a group.
 */
export function RolloutPanel({
  repoId,
  isAdmin,
  group,
  ungroupedOnly,
  fallback,
}: {
  repoId: string
  isAdmin: boolean
  group?: string
  ungroupedOnly?: boolean
  /** Shown instead when there is no rollout (with these Applications) yet. */
  fallback?: ReactNode
}) {
  const [rollouts, setRollouts] = useState<Rollout[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [openLogs, setOpenLogs] = useState<Set<string>>(new Set())
  const toggleLogs = (app: string) =>
    setOpenLogs((prev) => {
      const next = new Set(prev)
      if (next.has(app)) next.delete(app)
      else next.add(app)
      return next
    })

  const load = useCallback(() => {
    rolloutAPI
      .list(repoId)
      .then((r) => {
        setRollouts(r)
        setLoadError(null)
      })
      .catch((err) => setLoadError(apiError(err, 'Failed to load rollouts')))
  }, [repoId])

  useEffect(load, [load])
  const latest = rollouts[0]
  useEffect(() => {
    if (latest?.status !== 'running') return
    const t = setInterval(load, 4000)
    return () => clearInterval(t)
  }, [latest?.status, load])

  if (!latest) {
    return <>{fallback}</>
  }

  const all = [...latest.items].sort((a, b) => a.position - b.position)
  const byApp = new Map(all.map((i) => [i.application, i]))
  const items = all.filter((i) => {
    const g = splitInstance(i.application).group
    if (group) return g === group
    if (ungroupedOnly) return !g
    return true
  })
  if (items.length === 0) {
    return <>{fallback}</>
  }

  const run = async (key: string, fn: () => Promise<Rollout>) => {
    setBusy(key)
    setError(null)
    try {
      await fn()
      load()
    } catch (err) {
      setError(apiError(err, 'Action failed'))
    } finally {
      setBusy(null)
    }
  }

  // Swaps two shown Applications in the full order (other groups keep theirs).
  const move = (index: number, delta: number) => {
    const order = all.map((i) => i.application)
    const a = order.indexOf(items[index].application)
    const b = order.indexOf(items[index + delta].application)
    ;[order[a], order[b]] = [order[b], order[a]]
    run(`move-${index}`, () => rolloutAPI.reorder(latest.id, order))
  }

  return (
    <Card className="mb-6">
      <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <CardTitle
          icon={<Layers className="h-5 w-5 text-primary" />}
          title="Rollout"
          description={
            <>
              Applications of the latest commit, in the order TowerCrane runs them. dependsOn always wins
              {isAdmin ? '; you can reorder the ones that are still waiting.' : '.'}
            </>
          }
        />
        <div className="flex flex-shrink-0 items-center gap-2">
          <MetaLine items={[latest.commit_sha.slice(0, 7)]} />
          <StatusBadge {...rolloutStatus[latest.status]} />
        </div>
      </CardHeader>
      <CardBody className="space-y-3">
        {(error || loadError) && <Alert>{error || loadError}</Alert>}
        <ol className="space-y-2">
          {items.map((item, index) => {
            const reason = waitingReason(item, byApp)
            const canMove = isAdmin && latest.status === 'running' && item.status === 'waiting'
            return (
              <li
                key={item.application}
                className={cn(
                  'rounded-lg border px-3 py-2.5',
                  item.status === 'awaiting_yes' ? 'border-attention-line bg-attention-soft/50' : 'border-line bg-surface'
                )}
              >
                <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                  <div className="flex min-w-0 items-start gap-3">
                    <span className="mt-0.5 flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-primary-soft text-xs font-semibold text-primary-ink">
                      {index + 1}
                    </span>
                    <div className="min-w-0 space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <Link
                          to={`/projects/${repoId}/apps/${encodeURIComponent(item.application)}`}
                          className="font-mono text-sm font-semibold text-ink hover:text-primary"
                        >
                          <InstanceName name={item.application} />
                        </Link>
                        <StatusBadge {...rolloutItemStatus[item.status]} />
                        {item.paused && <Badge tone="attention">Paused</Badge>}
                        {afterText(item) && <span className="text-xs text-muted">{afterText(item)}</span>}
                      </div>
                      {(item.message || reason) && <p className="break-words text-xs text-muted">{item.message || reason}</p>}
                    </div>
                  </div>
                  <div className="flex flex-shrink-0 gap-1">
                    {item.plan_op_id && (
                      <Button
                        size="sm"
                        variant={openLogs.has(item.application) ? 'secondary' : 'ghost'}
                        onClick={() => toggleLogs(item.application)}
                        aria-expanded={openLogs.has(item.application)}
                        title="Show the plan and apply logs"
                      >
                        <Terminal className="h-4 w-4" /> Logs
                      </Button>
                    )}
                    {isAdmin && (
                      <>
                        {canMove && (
                          <>
                            <Button
                              size="sm"
                              variant="ghost"
                              disabled={index === 0 || busy !== null}
                              onClick={() => move(index, -1)}
                              aria-label="Move up"
                              title="Move up"
                            >
                              <ArrowUp className="h-4 w-4" />
                            </Button>
                            <Button
                              size="sm"
                              variant="ghost"
                              disabled={index === items.length - 1 || busy !== null}
                              onClick={() => move(index, 1)}
                              aria-label="Move down"
                              title="Move down"
                            >
                              <ArrowDown className="h-4 w-4" />
                            </Button>
                            <Button
                              size="sm"
                              variant="ghost"
                              loading={busy === `pause-${item.application}`}
                              onClick={() =>
                                run(`pause-${item.application}`, () =>
                                  rolloutAPI.action(latest.id, item.application, item.paused ? 'resume' : 'pause')
                                )
                              }
                              aria-label={item.paused ? 'Resume' : 'Pause'}
                              title={item.paused ? 'Resume' : 'Pause'}
                            >
                              {busy !== `pause-${item.application}` &&
                                (item.paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />)}
                            </Button>
                          </>
                        )}
                        {retryable.includes(item.status) && latest.status !== 'superseded' && (
                          <Button
                            size="sm"
                            variant="secondary"
                            loading={busy === `retry-${item.application}`}
                            onClick={() =>
                              run(`retry-${item.application}`, () => rolloutAPI.action(latest.id, item.application, 'retry'))
                            }
                          >
                            {busy !== `retry-${item.application}` && <RotateCcw className="h-4 w-4" />}{' '}
                            {item.status === 'unchanged' ? 'Plan' : 'Retry'}
                          </Button>
                        )}
                      </>
                    )}
                  </div>
                </div>
                {openLogs.has(item.application) && item.plan_op_id && (
                  <div className="mt-3 space-y-3 border-t border-line pt-3">
                    <OperationLog key={item.plan_op_id} operationId={item.plan_op_id} title="Plan" />
                    {item.apply_op_id && <OperationLog key={item.apply_op_id} operationId={item.apply_op_id} title="Apply" />}
                  </div>
                )}
              </li>
            )
          })}
        </ol>
      </CardBody>
    </Card>
  )
}
