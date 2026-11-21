import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { CalendarClock, Play, Terminal, Timer, Trash2 } from 'lucide-react'
import { jobAPI, type Job, type JobRun } from '../api/jobs'
import { apiError } from '../api/admin'
import type { StatusInfo } from '../lib/status'
import { timeAgo } from '../lib/utils'
import { Card, CardBody, CardHeader, CardTitle } from './ui/Card'
import { Button } from './ui/Button'
import { Alert } from './ui/Alert'
import { StatusBadge } from './ui/StatusBadge'
import { MetaLine } from './ui/MetaLine'
import { InstanceName } from './InstanceName'
import { OperationLog } from './OperationLog'

const runStatus: Record<JobRun['status'], StatusInfo> = {
  running: { state: 'progress', label: 'Running' },
  success: { state: 'ok', label: 'Success' },
  failed: { state: 'error', label: 'Failed' },
  skipped: { state: 'inert', label: 'Skipped' },
}

const triggerLabel: Record<string, string> = {
  schedule: 'on schedule',
  after: 'after its time was up',
  manual: 'by hand',
  'catch-up': 'catching up (TowerCrane was down)',
}

/** "Thu 22:00 · in 3 h" in the browser's time zone. */
function when(iso?: string) {
  if (!iso) return null
  const d = new Date(iso)
  const label = d.toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
  const mins = Math.round((d.getTime() - Date.now()) / 60000)
  const rel = mins <= 0 ? 'due now' : mins < 60 ? `in ${mins} min` : mins < 48 * 60 ? `in ${Math.round(mins / 60)} h` : `in ${Math.round(mins / 1440)} d`
  return `${label} · ${rel}`
}

/**
 * Jobs (kind: Job) of a group: ephemeral Applications TowerCrane applies at
 * scheduled times or on demand and destroys later, through the usual
 * plan/apply/destroy.
 */
export function JobsPanel({ repoId, group, canRun }: { repoId: string; group: string; canRun: boolean }) {
  const [jobs, setJobs] = useState<Job[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [openLog, setOpenLog] = useState<string | null>(null)

  const load = useCallback(() => {
    jobAPI
      .list(repoId)
      .then((all) => {
        setJobs(all.filter((j) => j.group === group))
        setError(null)
      })
      .catch((err) => setError(apiError(err, 'Failed to load jobs')))
  }, [repoId, group])
  useEffect(load, [load])
  // Fast while something runs; otherwise keep the "next" times current.
  const running = jobs?.some((j) => j.running) ?? false
  useEffect(() => {
    const t = setInterval(load, running ? 3000 : 30000)
    return () => clearInterval(t)
  }, [running, load])

  if (!jobs || (jobs.length === 0 && !error)) return null

  const run = async (job: Job, action: 'apply' | 'destroy') => {
    if (action === 'destroy' && !confirm(`Destroy every resource of ${job.application} now?`)) return
    setBusy(job.name + action)
    setError(null)
    try {
      const r = await jobAPI.run(repoId, job.name, action)
      setOpenLog(r.id)
      load()
    } catch (err) {
      setError(apiError(err, `Failed to ${action}`))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section aria-labelledby="jobs-heading" className="mb-6 space-y-3">
      <h2 id="jobs-heading" className="flex items-center gap-2 text-base font-semibold text-ink">
        <CalendarClock className="h-4 w-4 text-primary" aria-hidden="true" /> Jobs
      </h2>
      {error && <Alert>{error}</Alert>}
      {jobs.map((job) => {
        const state: StatusInfo = job.running
          ? { state: 'progress', label: 'Running' }
          : job.up
            ? { state: 'ok', label: 'Up' }
            : { state: 'inert', label: 'Destroyed' }
        return (
          <Card key={job.name}>
            <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
              <CardTitle
                title={
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="font-mono">{job.name}</span>
                    <StatusBadge {...state} />
                  </span>
                }
                description={
                  <span className="flex flex-wrap items-center gap-1.5">
                    runs
                    <Link
                      to={`/projects/${repoId}/apps/${encodeURIComponent(job.application)}`}
                      className="font-mono text-primary hover:underline"
                    >
                      <InstanceName name={job.application} />
                    </Link>
                    {job.description && <span>· {job.description}</span>}
                  </span>
                }
              />
              {canRun && job.manual && (
                <div className="flex flex-shrink-0 gap-2">
                  <Button
                    size="sm"
                    variant="primary"
                    disabled={job.running || busy !== null}
                    loading={busy === job.name + 'apply'}
                    onClick={() => run(job, 'apply')}
                  >
                    <Play className="h-4 w-4" /> Run now
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-danger hover:bg-danger-soft hover:text-danger"
                    disabled={job.running || !job.up || busy !== null}
                    loading={busy === job.name + 'destroy'}
                    onClick={() => run(job, 'destroy')}
                    title={job.up ? undefined : 'Nothing to destroy'}
                  >
                    <Trash2 className="h-4 w-4" /> Destroy now
                  </Button>
                </div>
              )}
            </CardHeader>
            <CardBody className="space-y-4">
              <dl className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
                <div>
                  <dt className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted">
                    <Play className="h-3.5 w-3.5" aria-hidden="true" /> Apply
                  </dt>
                  <dd className="mt-1 space-y-0.5">
                    {job.apply_schedule ? (
                      <>
                        <MetaLine items={[job.apply_schedule, job.timezone]} />
                        <p className="text-ink">{when(job.next_apply)}</p>
                      </>
                    ) : (
                      <p className="text-muted">Only with Run now</p>
                    )}
                  </dd>
                </div>
                <div>
                  <dt className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted">
                    <Timer className="h-3.5 w-3.5" aria-hidden="true" /> Destroy
                  </dt>
                  <dd className="mt-1 space-y-0.5">
                    {job.destroy_schedule && <MetaLine items={[job.destroy_schedule, job.timezone]} />}
                    {job.destroy_after && <MetaLine items={[`${job.destroy_after} after each apply`]} />}
                    {!job.destroy_schedule && !job.destroy_after && <p className="text-muted">Only with Destroy now</p>}
                    {job.up && job.next_destroy && <p className="text-ink">{when(job.next_destroy)}</p>}
                    {job.up && job.up_since && <p className="text-xs text-muted">up since {timeAgo(job.up_since)}</p>}
                  </dd>
                </div>
              </dl>

              <div>
                <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">Runs</p>
                {job.runs.length === 0 ? (
                  <p className="text-sm text-muted">No runs yet.</p>
                ) : (
                  <ul className="divide-y divide-line rounded-lg border border-line">
                    {job.runs.slice(0, 8).map((r) => (
                      <li key={r.id} className="px-3 py-2 text-sm">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="w-16 font-mono text-xs font-semibold uppercase text-ink">{r.action}</span>
                          <StatusBadge {...runStatus[r.status]} />
                          <span className="text-xs text-muted">{triggerLabel[r.trigger] ?? r.trigger}</span>
                          <span className="ml-auto text-xs text-muted">{new Date(r.created_at).toLocaleString()}</span>
                          {r.operation_id && (
                            <Button
                              size="sm"
                              variant={openLog === r.id ? 'secondary' : 'ghost'}
                              onClick={() => setOpenLog(openLog === r.id ? null : r.id)}
                              aria-expanded={openLog === r.id}
                            >
                              <Terminal className="h-4 w-4" /> Logs
                            </Button>
                          )}
                        </div>
                        {r.message && <p className="mt-1 break-words text-xs text-muted">{r.message}</p>}
                        {openLog === r.id && r.operation_id && (
                          <div className="mt-2 border-t border-line pt-2">
                            <OperationLog key={r.operation_id} operationId={r.operation_id} title={r.action} />
                          </div>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </CardBody>
          </Card>
        )
      })}
    </section>
  )
}
