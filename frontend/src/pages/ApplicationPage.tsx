import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowRight, Boxes, FileCode2, History, Link2, Share2, Terminal } from 'lucide-react'
import { Layout } from '../components/Layout'
import { Card, CardBody, CardHeader } from '../components/ui/Card'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { Alert } from '../components/ui/Alert'
import { Spinner } from '../components/ui/Spinner'
import { EmptyState } from '../components/ui/EmptyState'
import { PageHeader } from '../components/ui/PageHeader'
import { Tabs } from '../components/ui/Tabs'
import { StatusBadge } from '../components/ui/StatusBadge'
import { MetaLine } from '../components/ui/MetaLine'
import { CodeBlock } from '../components/ui/CodeBlock'
import { Field, Select } from '../components/ui/Field'
import { ApplicationMeta } from '../components/ApplicationMeta'
import { RefreshButton } from '../components/RefreshButton'
import { RefreshingHint } from '../components/RefreshingHint'
import { OperationLog } from '../components/OperationLog'
import { GroupTag, InstanceName } from '../components/InstanceName'
import { AwaitingYesCard, RunOperationCard } from '../components/operations'
import { useResourceMap } from '../hooks/useResourceMap'
import { useAuth } from '../hooks/useAuth'
import { operationAPI } from '../api/operations'
import type { Operation } from '../store/operationStore'
import { atLeast } from '../lib/roles'
import { operationStatus, syncStatus } from '../lib/status'

type Tab = 'logs' | 'operations' | 'manifest'

/**
 * One service (an Application in its group): what it relates to, the live
 * logs of its plans and applies, its operations (run, confirm, history) and
 * its manifest.
 */
export function ApplicationPage() {
  const { repo_id = '', app_name = '' } = useParams<{ repo_id: string; app_name: string }>()
  const { user } = useAuth()
  const canApply = atLeast(user?.role, 'approver')
  const isAdmin = atLeast(user?.role, 'admin')
  // The whole repository's map: shared (cached) with the repository and group pages.
  const { map, error: mapError, refreshing, loading, reload: reloadMap } = useResourceMap(repo_id)
  const [ops, setOps] = useState<Operation[]>([])
  const [error, setError] = useState<string | null>(null)
  const [tab, setTab] = useState<Tab>('logs')
  // The operation shown in Logs; null = the latest one.
  const [logOp, setLogOp] = useState<string | null>(null)

  const loadOps = useCallback(() => {
    operationAPI
      .listOperations(repo_id, 100)
      .then((r) => setOps(r.operations.filter((o) => o.application === app_name)))
      .catch(() => undefined)
  }, [repo_id, app_name])
  useEffect(loadOps, [loadOps])
  const load = useCallback(() => {
    reloadMap()
    loadOps()
  }, [reloadMap, loadOps])

  // Keep the list fresh while something runs (the open log follows itself).
  const running = ops.some((o) => o.status === 'pending' || o.status === 'running')
  useEffect(() => {
    if (!running) return
    const t = setInterval(loadOps, 3000)
    return () => clearInterval(t)
  }, [running, loadOps])

  const app = map?.applications?.find((a) => a.name === app_name)
  const awaiting = ops.filter((o) => o.type === 'plan' && o.awaiting_confirmation)
  const shownLog = logOp ?? ops[0]?.id ?? null
  const showLogs = (id: string) => {
    setLogOp(id)
    setTab('logs')
  }
  const started = (op: Operation) => {
    setOps((list) => [op, ...list])
    setLogOp(null) // follow the new operation
    setTab('logs')
    loadOps()
  }
  const appLink = (name: string) => (
    <Link
      key={name}
      to={`/projects/${repo_id}/apps/${encodeURIComponent(name)}`}
      className="rounded-md bg-primary-soft px-2 py-0.5 font-mono text-xs font-medium text-primary-ink hover:bg-primary hover:text-white"
    >
      <InstanceName name={name} />
    </Link>
  )

  return (
    <Layout>
      {(error || mapError) && <Alert className="mb-6">{error || mapError}</Alert>}

      {loading ? (
        <Spinner label="Loading Application..." />
      ) : map && !app ? (
        <EmptyState
          icon={Boxes}
          title={`Application ${app_name} not found`}
          description="It may have been renamed or removed from the manifests."
        />
      ) : app ? (
        <>
          <PageHeader
            back={
              app.group
                ? { to: `/projects/${repo_id}/groups/${encodeURIComponent(app.group)}`, label: `Group ${app.group}` }
                : { to: `/projects/${repo_id}`, label: 'Project' }
            }
            mono
            title={
              <span className="inline-flex items-center gap-2">
                {app.app}
                {app.group && <GroupTag group={app.group} className="h-6 px-2.5 text-xs" />}
              </span>
            }
            meta={
              <>
                <StatusBadge {...syncStatus[app.status ?? 'unknown']} />
                <ApplicationMeta app={app} />
                {app.source_repo && <MetaLine items={[`code: ${app.source_repo}`]} />}
                <RefreshingHint show={refreshing} />
              </>
            }
            actions={<RefreshButton repoId={repo_id} onProcessing={() => setTimeout(load, 2000)} />}
          />

          {/* What this Application relates to */}
          <div className="mb-6 grid grid-cols-1 gap-3 text-sm md:grid-cols-3">
            <Relation icon={ArrowRight} title="Applied after (dependsOn, group order)" empty="No dependencies">
              {[...app.depends_on, ...app.promotion].map(appLink)}
            </Relation>
            <Relation icon={Link2} title="Applied before (dependents)" empty="Nothing depends on it">
              {app.dependents.map(appLink)}
            </Relation>
            <Relation icon={Share2} title="Shares modules with" empty="No shared modules">
              {app.shares_modules_with.map(appLink)}
            </Relation>
          </div>

          {/* A plan waiting for a yes comes first. */}
          <AwaitingYesCard
            plans={awaiting}
            canApply={canApply}
            isAdmin={isAdmin}
            onChanged={(created) => (created ? started(created) : loadOps())}
            onError={setError}
          />

          <Tabs
            className="mb-4"
            aria-label="Application views"
            value={tab}
            onChange={setTab}
            items={[
              { key: 'logs', label: 'Logs', icon: Terminal },
              { key: 'operations', label: 'Operations', icon: History, count: ops.length },
              { key: 'manifest', label: 'Manifest', icon: FileCode2 },
            ]}
          />

          {tab === 'logs' &&
            (shownLog === null ? (
              <EmptyState icon={Terminal} title="No logs yet" description="Plans and applies of this Application show their output here." />
            ) : (
              <Card>
                <CardHeader>
                  <Field label="Operation">
                    <Select value={shownLog} onChange={(e) => setLogOp(e.target.value)}>
                      {ops.map((op, i) => (
                        <option key={op.id} value={op.id}>
                          {op.type} · {operationStatus[op.status].label} · {new Date(op.created_at).toLocaleString()}
                          {i === 0 ? ' (latest)' : ''}
                        </option>
                      ))}
                    </Select>
                  </Field>
                </CardHeader>
                <CardBody>
                  <OperationLog key={shownLog} operationId={shownLog} title={ops.find((o) => o.id === shownLog)?.type ?? 'log'} />
                </CardBody>
              </Card>
            ))}

          {tab === 'operations' && (
            <>
              <RunOperationCard
                repoId={repo_id}
                appName={app.name}
                canApply={canApply}
                isAdmin={isAdmin}
                onStarted={started}
                onError={setError}
              />
              {ops.length === 0 ? (
                <EmptyState icon={History} title="No operations yet" description="Merged changes are planned automatically; you can also run a plan above." />
              ) : (
                <Card>
                  <ul className="divide-y divide-line">
                    {ops.map((op) => (
                      <li key={op.id} className="px-5 py-3 text-sm">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="w-16 font-mono text-xs font-semibold uppercase text-ink">{op.type}</span>
                          <StatusBadge {...operationStatus[op.status]} />
                          {op.emergency && <Badge tone="danger">Emergency</Badge>}
                          {op.awaiting_confirmation && <StatusBadge state="attention" label="Waiting for yes" />}
                          {op.type === 'plan' && op.status === 'success' && !op.has_changes && <Badge tone="neutral">No changes</Badge>}
                          <MetaLine className="ml-auto" items={[op.commit_sha?.slice(0, 7), new Date(op.created_at).toLocaleString()]} />
                          <Button size="sm" variant="ghost" onClick={() => showLogs(op.id)} title={`Show the ${op.type} log`}>
                            <Terminal className="h-4 w-4" /> Logs
                          </Button>
                        </div>
                        {op.policy_note && <p className="mt-1 text-xs text-muted">{op.policy_note.split('\n')[0]}</p>}
                        {op.error && <p className="mt-1 break-words text-xs text-danger">{op.error.split('\n')[0]}</p>}
                      </li>
                    ))}
                  </ul>
                </Card>
              )}
            </>
          )}

          {tab === 'manifest' && (
            <Card>
              <CardHeader>
                <p className="font-mono text-sm text-muted">{app.file}</p>
              </CardHeader>
              <CardBody>
                <CodeBlock content={app.manifest} className="max-h-none" />
              </CardBody>
            </Card>
          )}
        </>
      ) : null}
    </Layout>
  )
}

function Relation({
  icon: Icon,
  title,
  empty,
  children,
}: {
  icon: typeof Link2
  title: string
  empty: string
  children: React.ReactNode[]
}) {
  return (
    <div className="rounded-xl border border-line bg-surface px-4 py-3">
      <p className="mb-2 flex items-center gap-1.5 text-xs font-medium text-muted">
        <Icon className="h-3.5 w-3.5" /> {title}
      </p>
      <div className="flex flex-wrap gap-1.5">
        {children.length > 0 ? children : <span className="text-xs text-muted">{empty}</span>}
      </div>
    </div>
  )
}
