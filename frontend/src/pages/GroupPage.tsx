import { Link, useParams } from 'react-router-dom'
import { Cloud, FileCode2, Layers, Workflow } from 'lucide-react'
import { Layout } from '../components/Layout'
import { Card, CardBody, CardHeader, CardTitle } from '../components/ui/Card'
import { Alert } from '../components/ui/Alert'
import { Spinner } from '../components/ui/Spinner'
import { EmptyState } from '../components/ui/EmptyState'
import { PageHeader } from '../components/ui/PageHeader'
import { MetaLine } from '../components/ui/MetaLine'
import { StatusBadge } from '../components/ui/StatusBadge'
import { CodeBlock } from '../components/ui/CodeBlock'
import { RefreshButton } from '../components/RefreshButton'
import { timeAgo } from '../lib/utils'
import { RefreshingHint } from '../components/RefreshingHint'
import { useResourceMap } from '../hooks/useResourceMap'
import { RolloutPanel } from '../components/RolloutPanel'
import { useAuth } from '../hooks/useAuth'
import { atLeast } from '../lib/roles'
import { cloudAccountStatus } from '../components/GroupCloud'
import { GroupDiagram } from '../components/GroupDiagram'
import { JobsPanel } from '../components/JobsPanel'

/** One group: the cloud account it applies to, its promotion order and its Applications. */
export function GroupPage() {
  const { repo_id = '', group_name = '' } = useParams<{ repo_id: string; group_name: string }>()
  const { user } = useAuth()
  const isAdmin = atLeast(user?.role, 'admin')
  const canApply = atLeast(user?.role, 'approver')
  const { map, error, refreshing, loading, reload: load } = useResourceMap(repo_id)

  const group = map?.groups?.find((g) => g.name === group_name)
  const apps = (map?.applications ?? []).filter((a) => a.group === group_name)
  const diagram = map?.diagrams?.find((d) => d.group === group_name)
  const nextGroups = (map?.groups ?? []).filter((g) => g.after.includes(group_name)).map((g) => g.name)
  const groupLink = (name: string) => (
    <Link
      key={name}
      to={`/projects/${repo_id}/groups/${encodeURIComponent(name)}`}
      className="rounded-md bg-primary-soft px-2 py-0.5 font-mono text-xs font-medium text-primary-ink hover:bg-primary hover:text-white"
    >
      {name}
    </Link>
  )

  return (
    <Layout>
      {error && <Alert className="mb-6">{error}</Alert>}
      {loading ? (
        <Spinner label="Loading group..." />
      ) : map && !group ? (
        <EmptyState icon={Layers} title={`Group ${group_name} not found`} description="It may have been renamed or removed from the manifests." />
      ) : group ? (
        <>
          <PageHeader
            back={{ to: `/projects/${repo_id}`, label: 'Project' }}
            title={
              <span className="flex items-center gap-2">
                <Layers className="h-6 w-6 text-primary" aria-hidden="true" /> {group.name}
              </span>
            }
            meta={
              <>
                <MetaLine
                  items={[
                    group.cloud_account,
                    `${apps.length} Applications`,
                    map?.commit_sha && `commit ${map.commit_sha.slice(0, 7)}`,
                    map?.read_at && `read ${timeAgo(map.read_at)}`,
                  ]}
                />
                <RefreshingHint show={refreshing} />
              </>
            }
            description={group.description}
            actions={<RefreshButton repoId={repo_id} onProcessing={() => setTimeout(load, 2000)} />}
          />

          {/* First thing in a group: its diagram, alive with the latest rollout. */}
          <section aria-labelledby="diagram-heading" className="mb-6 space-y-2">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 id="diagram-heading" className="flex items-center gap-2 text-base font-semibold text-ink">
                <Workflow className="h-4 w-4 text-primary" aria-hidden="true" /> Diagram
              </h2>
              <MetaLine items={[diagram ? diagram.file : 'default layout (add a kind: Diagram manifest to style it)']} />
            </div>
            {apps.length === 0 ? (
              <EmptyState icon={Workflow} title="No Applications in this group" description={`Add "groups: [${group.name}]" to an Application manifest.`} />
            ) : (
              <GroupDiagram repoId={repo_id} apps={apps} spec={diagram?.spec ?? null} />
            )}
          </section>

          <JobsPanel repoId={repo_id} group={group.name} canRun={canApply} />

          <RolloutPanel repoId={repo_id} isAdmin={isAdmin} group={group.name} />

          <div className="mb-8 mt-8 grid grid-cols-1 gap-4 lg:grid-cols-3">
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle
                  icon={<Cloud className="h-4 w-4 text-primary" />}
                  title="Cloud provider"
                  description="Every Application of this group is applied with this cloud account (spec.cloudAccount of the Group)."
                />
              </CardHeader>
              <CardBody className="space-y-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-mono text-lg font-semibold text-ink">{group.cloud_account}</span>
                  <StatusBadge {...cloudAccountStatus[group.cloud_account_status]} />
                  {group.account && (
                    <StatusBadge {...(group.account.verified ? { state: 'ok', label: 'Verified' } : { state: 'inert', label: 'Not verified' })} />
                  )}
                </div>
                {group.account ? (
                  <dl className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
                    <Detail label="Provider" value={group.account.provider.toUpperCase()} />
                    <Detail label="Region" value={group.account.region} />
                    <Detail label="Account ID" value={group.account.aws_account_id} />
                    {group.account.endpoint_url && <Detail label="Endpoint" value={group.account.endpoint_url} />}
                  </dl>
                ) : (
                  <p className="text-sm text-muted">
                    {group.cloud_account_status === 'not_attached'
                      ? 'This cloud account exists in TowerCrane but is not attached to this repository, so its Applications fail until an admin attaches it in '
                      : 'There is no cloud account with this name in TowerCrane. Create it in Integrations and attach it to the repository in '}
                    <Link to="/repositories" className="text-primary hover:underline">
                      Repositories
                    </Link>
                    .
                  </p>
                )}
              </CardBody>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle title="Promotion" description="Groups are applied in order: a group starts once the groups it comes after applied cleanly." />
              </CardHeader>
              <CardBody className="space-y-3 text-sm">
                <div>
                  <p className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-muted">After</p>
                  <div className="flex flex-wrap gap-1.5">
                    {group.after.length > 0 ? group.after.map(groupLink) : <span className="text-muted">First group</span>}
                  </div>
                </div>
                <div>
                  <p className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-muted">Before</p>
                  <div className="flex flex-wrap gap-1.5">
                    {nextGroups.length > 0 ? nextGroups.map(groupLink) : <span className="text-muted">No group waits for it</span>}
                  </div>
                </div>
              </CardBody>
            </Card>
          </div>

          <h2 className="mb-3 mt-8 flex items-center gap-2 text-base font-semibold text-ink">
            <FileCode2 className="h-4 w-4 text-primary" aria-hidden="true" /> Manifest
          </h2>
          <CodeBlock content={group.manifest} className="max-h-none" />
        </>
      ) : null}
    </Layout>
  )
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted">{label}</dt>
      <dd className="break-all font-mono text-ink">{value}</dd>
    </div>
  )
}
