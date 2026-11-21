import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Boxes, Layers, Link2, Settings2, SlidersHorizontal } from 'lucide-react'
import { Layout } from '../components/Layout'
import { Card, CardBody } from '../components/ui/Card'
import { Button, ButtonLink } from '../components/ui/Button'
import { ProjectSettings } from '../components/ProjectSettings'
import { ProjectNotices } from '../components/ProjectNotices'
import { projectAPI, type Project } from '../api/projects'
import { Alert } from '../components/ui/Alert'
import { Spinner } from '../components/ui/Spinner'
import { EmptyState } from '../components/ui/EmptyState'
import { PageHeader } from '../components/ui/PageHeader'
import { MetaLine } from '../components/ui/MetaLine'
import { StatusBadge } from '../components/ui/StatusBadge'
import { ApplicationMeta } from '../components/ApplicationMeta'
import { RefreshButton } from '../components/RefreshButton'
import { timeAgo } from '../lib/utils'
import { ProtectionStatus } from '../components/ProtectionStatus'
import { RefreshingHint } from '../components/RefreshingHint'
import { useResourceMap } from '../hooks/useResourceMap'
import { RolloutPanel } from '../components/RolloutPanel'
import type { MapApplication } from '../api/resourceMap'
import { GroupCard } from '../components/GroupCloud'
import { InstanceName, appNames } from '../components/InstanceName'
import { useAuth } from '../hooks/useAuth'
import { atLeast } from '../lib/roles'
import { syncStatus } from '../lib/status'

/** A project's home: its groups (and Applications without group), plus their rollout. */
export function ApplicationsPage() {
  const { repo_id = '' } = useParams<{ repo_id: string }>()
  const { user } = useAuth()
  const isAdmin = atLeast(user?.role, 'admin')
  const [project, setProject] = useState<Project | null>(null)
  const [settings, setSettings] = useState(false)
  const { map, error, refreshing, loading, reload: load } = useResourceMap(repo_id)

  useEffect(() => {
    projectAPI.get(repo_id).then(setProject).catch(() => undefined)
  }, [repo_id])


  const apps = map?.applications ?? []
  const groups = map?.groups ?? []
  const ungrouped = apps.filter((a) => !a.group)

  return (
    <Layout>
      <PageHeader
        back={{ to: '/projects', label: 'Projects' }}
        title={project?.name || 'Project'}
        description={
          project && (
            <>
              Manifests from <code className="font-mono">{project.config_repository.full_name}</code>
              {project.manifests_dir ? (
                <>
                  {' '}
                  (<code className="font-mono">{project.manifests_dir}/</code>)
                </>
              ) : (
                ' (repository root)'
              )}
              {project.manifest_file && (
                <>
                  {' '}· defined by <code className="font-mono">{project.manifest_file}</code>
                </>
              )}
              {project.source_repositories.length > 0 && (
                <>
                  {' '}· code from{' '}
                  <code className="font-mono">{project.source_repositories.map((r) => r.full_name).join(', ')}</code>
                </>
              )}
            </>
          )
        }
        meta={
          map && (
            <>
              <MetaLine
                items={[
                  map.branch,
                  map.commit_sha && `commit ${map.commit_sha.slice(0, 7)}`,
                  map.read_at && `read ${timeAgo(map.read_at)}`,
                  groups.length > 0 && `${groups.length} groups`,
                  `${apps.length} Applications`,
                ]}
              />
              <RefreshingHint show={refreshing} />
            </>
          )
        }
        actions={
          <>
            <ButtonLink to={`/projects/${repo_id}/operations`}>
              <Settings2 className="h-4 w-4" /> Operations
            </ButtonLink>
            {isAdmin && project && (
              <Button variant={settings ? 'secondary' : 'ghost'} onClick={() => setSettings(!settings)}>
                <SlidersHorizontal className="h-4 w-4" /> Settings
              </Button>
            )}
            <RefreshButton repoId={repo_id} onProcessing={() => setTimeout(load, 2000)} />
          </>
        }
      />

      {settings && project && (
        <ProjectSettings
          project={project}
          onSaved={(p) => {
            setProject(p)
            setSettings(false)
            load()
          }}
          onClose={() => setSettings(false)}
        />
      )}
      {project && <ProjectNotices project={project} isAdmin={isAdmin} onChange={setProject} />}
      {error && <Alert className="mb-6">{error}</Alert>}
      <ProtectionStatus repoId={repo_id} />
      {(map?.manifest_warnings?.length ?? 0) > 0 && (
        <Alert tone="info" className="mb-6">
          {map!.manifest_warnings!.join(' · ')}
        </Alert>
      )}
      {(map?.manifest_errors?.length ?? 0) > 0 && (
        <Alert tone="attention" className="mb-6">
          Invalid manifests were ignored: {map!.manifest_errors!.join(' · ')}
        </Alert>
      )}

      {/* With groups, each group's page holds its rollout; here only the ungrouped one. */}
      <RolloutPanel repoId={repo_id} isAdmin={isAdmin} ungroupedOnly={groups.length > 0} />

      {loading ? (
        <Spinner label="Reading the manifests..." />
      ) : apps.length === 0 && groups.length === 0 && map ? (
        <EmptyState
          icon={Boxes}
          title="No Applications yet"
          description={`Add an Application manifest in ${project?.manifests_dir ? project.manifests_dir + '/' : 'the repository root'} (apiVersion: towercrane.io/v1alpha1) to let TowerCrane manage an environment.`}
        />
      ) : (
        <div className="space-y-8">
          {groups.length > 0 && (
            <section aria-labelledby="groups-heading">
              <h2 id="groups-heading" className="mb-3 flex items-center gap-2 text-base font-semibold text-ink">
                <Layers className="h-4 w-4 text-primary" aria-hidden="true" /> Groups
              </h2>
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
                {groups.map((g) => (
                  <GroupCard key={g.name} repoId={repo_id} group={g} apps={apps.filter((a) => a.group === g.name)} />
                ))}
              </div>
            </section>
          )}
          {ungrouped.length > 0 && (
            <section aria-label="Applications without group" className="space-y-3">
              {groups.length > 0 && (
                <h2 className="flex items-center gap-2 text-base font-semibold text-ink">
                  <Boxes className="h-4 w-4 text-primary" aria-hidden="true" /> Without group
                </h2>
              )}
              <ApplicationGrid repoId={repo_id} apps={ungrouped} />
            </section>
          )}
        </div>
      )}
    </Layout>
  )
}

export function ApplicationGrid({ repoId, apps }: { repoId: string; apps: MapApplication[] }) {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
      {apps.map((app) => (
        <ApplicationCard key={app.name} repoId={repoId} app={app} />
      ))}
    </div>
  )
}

function ApplicationCard({ repoId, app }: { repoId: string; app: MapApplication }) {
  return (
    <Link
      to={`/projects/${repoId}/apps/${encodeURIComponent(app.name)}`}
      className="group block rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
    >
      <Card className="h-full transition group-hover:border-primary/40 group-hover:shadow-pop">
        <CardBody className="space-y-3">
          <div className="flex items-start justify-between gap-2">
            <p className="min-w-0 truncate font-mono text-base font-semibold text-ink group-hover:text-primary">
              <InstanceName name={app.name} />
            </p>
            <StatusBadge {...syncStatus[app.status ?? 'unknown']} />
          </div>
          <ApplicationMeta app={app} />
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-line pt-3 text-xs text-muted">
            <span>{app.resource_count} blocks</span>
            {app.depends_on.length > 0 && (
              <span className="flex items-center gap-1">
                <Link2 className="h-3.5 w-3.5" /> after {appNames(app.depends_on)}
              </span>
            )}
            {app.promotion.length > 0 && <span>promoted after {groupsOf(app.promotion).join(', ')}</span>}
          </div>
        </CardBody>
      </Card>
    </Link>
  )
}

/** Distinct groups of instance names ("vpc@sandbox" -> "sandbox"). */
function groupsOf(instances: string[]): string[] {
  return [...new Set(instances.map((n) => n.split('@')[1] ?? n))]
}
