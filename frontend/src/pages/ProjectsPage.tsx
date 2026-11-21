import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import axios from 'axios'
import { Cloud, FolderGit2, GitBranch, Plus, Settings2 } from 'lucide-react'
import { Layout } from '../components/Layout'
import { Card, CardBody, CardHeader, CardTitle } from '../components/ui/Card'
import { Button } from '../components/ui/Button'
import { Alert } from '../components/ui/Alert'
import { Spinner } from '../components/ui/Spinner'
import { EmptyState } from '../components/ui/EmptyState'
import { PageHeader } from '../components/ui/PageHeader'
import { Field, Input, Select } from '../components/ui/Field'
import { ChipPicker } from '../components/ChipPicker'
import { StatusBadge } from '../components/ui/StatusBadge'
import { projectAPI, type Project, type ProjectRepository } from '../api/projects'
import { apiError, integrationAPI, type CloudAccount } from '../api/admin'
import { useAuth } from '../hooks/useAuth'
import { atLeast } from '../lib/roles'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'

/** Connected repositories (the inventory projects pick from). */
export async function listConnectedRepositories(): Promise<ProjectRepository[]> {
  const res = await axios.get(`${API_URL}/repositories`, {
    headers: { Authorization: `Bearer ${localStorage.getItem('auth_token')}` },
  })
  return res.data.repositories || []
}

/**
 * Projects: where TowerCrane works. Each one reads its manifests from a
 * configuration repository and runs code from it and from its source
 * repositories.
 */
export function ProjectsPage() {
  const { user } = useAuth()
  const isAdmin = atLeast(user?.role, 'admin')
  const navigate = useNavigate()
  const [projects, setProjects] = useState<Project[] | null>(null)
  const [repos, setRepos] = useState<ProjectRepository[]>([])
  const [accounts, setAccounts] = useState<CloudAccount[]>([])
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    projectAPI
      .list()
      .then(setProjects)
      .catch((err) => setError(apiError(err, 'Failed to load projects')))
    listConnectedRepositories().then(setRepos).catch(() => undefined)
    integrationAPI.cloudAccounts().then(setAccounts).catch(() => undefined)
  }, [])

  const accountName = (id: string) => accounts.find((a) => a.id === id)?.name ?? id

  return (
    <Layout>
      <PageHeader
        title="Projects"
        description={
          <>
            A repository with a <code className="font-mono">kind: Project</code> manifest (e.g. <code className="font-mono">project.yaml</code>)
            becomes a project as soon as it's connected: it names the code repositories and requests cloud accounts, which an admin
            authorizes once.
          </>
        }
        actions={
          isAdmin && (
            <Button variant="ghost" onClick={() => setCreating(!creating)} title="For a repository without a Project manifest">
              <Plus className="h-4 w-4" /> Create by hand
            </Button>
          )
        }
      />
      {error && <Alert className="mb-6">{error}</Alert>}

      {creating && isAdmin && (
        <NewProjectForm
          repos={repos}
          accounts={accounts}
          takenConfigIds={(projects ?? []).map((p) => p.id)}
          onCancel={() => setCreating(false)}
          onCreated={(p) => navigate(`/projects/${p.id}`)}
        />
      )}

      {!projects && !error ? (
        <Spinner label="Loading projects..." />
      ) : projects && projects.length === 0 ? (
        <EmptyState
          icon={FolderGit2}
          title="No projects yet"
          description={
            isAdmin
              ? 'Add a project.yaml (kind: Project) to the repository with your manifests and connect it in Repositories.'
              : 'Ask a TowerCrane admin to connect a repository with a project.yaml.'
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {projects?.map((p) => (
            <Link
              key={p.id}
              to={`/projects/${p.id}`}
              className="group block rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
            >
              <Card className="h-full transition group-hover:border-primary/40 group-hover:shadow-pop">
                <CardBody className="space-y-3">
                  <p className="flex items-center gap-2 text-base font-semibold text-ink group-hover:text-primary">
                    <FolderGit2 className="h-4 w-4 text-primary" aria-hidden="true" /> {p.name}
                  </p>
                  {needsAttention(p, accounts) && (
                    <StatusBadge state="attention" label={needsAttention(p, accounts)!} />
                  )}
                  <dl className="space-y-1.5 text-xs">
                    <div className="flex items-start gap-1.5">
                      <Settings2 className="mt-0.5 h-3.5 w-3.5 flex-shrink-0 text-muted" aria-label="Configuration" />
                      <dd className="font-mono text-ink">{p.config_repository.full_name}</dd>
                    </div>
                    {p.source_repositories.length > 0 && (
                      <div className="flex items-start gap-1.5">
                        <GitBranch className="mt-0.5 h-3.5 w-3.5 flex-shrink-0 text-muted" aria-label="Code" />
                        <dd className="font-mono text-muted">{p.source_repositories.map((r) => r.full_name).join(', ')}</dd>
                      </div>
                    )}
                    {p.cloud_account_ids.length > 0 && (
                      <div className="flex items-start gap-1.5">
                        <Cloud className="mt-0.5 h-3.5 w-3.5 flex-shrink-0 text-muted" aria-label="Cloud accounts" />
                        <dd className="font-mono text-muted">{p.cloud_account_ids.map(accountName).join(', ')}</dd>
                      </div>
                    )}
                  </dl>
                </CardBody>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </Layout>
  )
}

/** What a project still needs from an admin, or null. */
function needsAttention(p: Project, accounts: CloudAccount[]): string | null {
  const pending = p.requested_cloud_accounts.filter((name) => {
    const a = accounts.find((x) => x.name === name)
    return !a || !p.cloud_account_ids.includes(a.id)
  })
  if (pending.length > 0) return `${pending.length} cloud account(s) to authorize`
  if (p.missing_sources.length > 0) return `${p.missing_sources.length} repository(ies) to connect`
  return null
}

function NewProjectForm({
  repos,
  accounts,
  takenConfigIds,
  onCancel,
  onCreated,
}: {
  repos: ProjectRepository[]
  accounts: CloudAccount[]
  takenConfigIds: string[]
  onCancel: () => void
  onCreated: (p: Project) => void
}) {
  const free = repos.filter((r) => !takenConfigIds.includes(r.id))
  const [name, setName] = useState('')
  const [configId, setConfigId] = useState('')
  const [sources, setSources] = useState<string[]>([])
  const [cloud, setCloud] = useState<string[]>([])
  const [manifestsDir, setManifestsDir] = useState('.towercrane')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async () => {
    setSaving(true)
    setError(null)
    try {
      onCreated(
        await projectAPI.create({
          name,
          config_repository_id: configId,
          source_repository_ids: sources.filter((s) => s !== configId),
          cloud_account_ids: cloud,
          manifests_dir: manifestsDir,
        })
      )
    } catch (err) {
      setError(apiError(err, 'Failed to create the project'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card className="mb-6" highlight="primary">
      <CardHeader>
        <CardTitle
          title="New project"
          description="Pick the repository with the manifests, the repositories with the code they run, and the cloud accounts they may use."
        />
      </CardHeader>
      <CardBody className="space-y-5">
        {error && <Alert>{error}</Alert>}
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Field label="Name" hint="Defaults to the configuration repository's name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="platform" />
          </Field>
          <Field label="Configuration repository" hint="Holds the manifests: groups, Applications, Jobs">
            <Select value={configId} onChange={(e) => setConfigId(e.target.value)}>
              <option value="">Choose a connected repository</option>
              {free.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.full_name}
                </option>
              ))}
            </Select>
          </Field>
        </div>
        <Field
          label="Manifests folder"
          className="max-w-md"
          hint="Where the manifests live in the configuration repository. Use / when the whole repository is configuration."
        >
          <Input mono value={manifestsDir} onChange={(e) => setManifestsDir(e.target.value)} placeholder=".towercrane" />
        </Field>
        <div>
          <p className="mb-1.5 text-sm font-medium text-ink">Code repositories</p>
          <p className="mb-2 text-xs text-muted">
            Applications take code from them with <code className="font-mono">spec.source.repo</code> (the configuration repository is always one).
          </p>
          <ChipPicker
            options={repos.filter((r) => r.id !== configId).map((r) => ({ id: r.id, label: r.full_name }))}
            selected={sources}
            onChange={setSources}
            empty="Connect more repositories in Repositories"
          />
        </div>
        <div>
          <p className="mb-1.5 text-sm font-medium text-ink">Cloud accounts</p>
          <ChipPicker
            options={accounts.map((a) => ({ id: a.id, label: a.name }))}
            selected={cloud}
            onChange={setCloud}
            empty="Add cloud accounts in Integrations"
          />
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="primary" onClick={submit} loading={saving} disabled={!configId}>
            Create project
          </Button>
        </div>
      </CardBody>
    </Card>
  )
}
