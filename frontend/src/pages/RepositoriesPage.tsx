import { useState, useEffect, useMemo } from 'react'
import axios from 'axios'
import { Link } from 'react-router-dom'
import { Lock, Globe, ExternalLink, Search, GitBranch, FolderGit2 } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { Layout } from '../components/Layout'
import { Card } from '../components/ui/Card'
import { Button, ButtonLink } from '../components/ui/Button'
import { Alert } from '../components/ui/Alert'
import { Spinner } from '../components/ui/Spinner'
import { EmptyState } from '../components/ui/EmptyState'
import { PageHeader } from '../components/ui/PageHeader'
import { Tabs } from '../components/ui/Tabs'
import { MetaLine } from '../components/ui/MetaLine'
import { inputClass } from '../components/ui/Field'
import { cn } from '../lib/utils'
import { atLeast } from '../lib/roles'
import { apiError } from '../api/admin'
import { projectAPI, type Project } from '../api/projects'
import { RefreshButton } from '../components/RefreshButton'

interface Repository {
  id?: string
  name: string
  full_name: string
  url: string
  owner: string
  description: string
  private: boolean
  default_branch: string
  installation_id?: number
  account?: string
  /** Cloud accounts the repository's manifests may use. */
  cloud_account_ids?: string[]
}

type RepoTab = 'available' | 'connected'

export function RepositoriesPage() {
  const { token, user } = useAuth()
  const isAdmin = atLeast(user?.role, 'admin')
  const [availableRepos, setAvailableRepos] = useState<Repository[]>([])
  const [connectedRepos, setConnectedRepos] = useState<Repository[]>([])
  const [loading, setLoading] = useState(false)
  const [activeTab, setActiveTab] = useState<RepoTab>('connected')
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [connectingKey, setConnectingKey] = useState<string | null>(null)
  const [projects, setProjects] = useState<Project[]>([])

  useEffect(() => {
    projectAPI.list().then(setProjects).catch(() => undefined)
  }, [])

  useEffect(() => {
    if (activeTab === 'available') {
      fetchAvailableRepositories()
    } else {
      fetchConnectedRepositories()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTab, token])

  const fetchAvailableRepositories = async () => {
    setLoading(true)
    setError(null)
    try {
      const response = await axios.get('/api/v1/repositories/available', {
        headers: { Authorization: `Bearer ${token}` },
      })
      setAvailableRepos(response.data.repositories || [])
    } catch (err) {
      setError(apiError(err, 'Failed to fetch repositories'))
    } finally {
      setLoading(false)
    }
  }

  const fetchConnectedRepositories = async () => {
    setLoading(true)
    setError(null)
    try {
      const response = await axios.get('/api/v1/repositories', {
        headers: { Authorization: `Bearer ${token}` },
      })
      setConnectedRepos(response.data.repositories || [])
    } catch (err) {
      setError('Failed to fetch connected repositories')
      console.error(err)
    } finally {
      setLoading(false)
    }
  }

  const connectRepository = async (owner: string, name: string, installationId?: number) => {
    const key = `${owner}/${name}`
    setConnectingKey(key)
    try {
      await axios.post(
        '/api/v1/repositories/connect',
        { owner, name, installation_id: installationId ?? 0 },
        {
          headers: { Authorization: `Bearer ${token}` },
        }
      )
      fetchConnectedRepositories()
      setAvailableRepos((repos) => repos.filter((r) => r.full_name !== key))
    } catch (err) {
      setError(apiError(err, 'Failed to connect repository'))
    } finally {
      setConnectingKey(null)
    }
  }

  const repos = activeTab === 'available' ? availableRepos : connectedRepos
  const filteredRepos = useMemo(() => {
    if (!search.trim()) return repos
    const query = search.toLowerCase()
    return repos.filter(
      (repo) =>
        repo.name.toLowerCase().includes(query) ||
        repo.full_name.toLowerCase().includes(query) ||
        repo.description?.toLowerCase().includes(query)
    )
  }, [repos, search])

  const tabs = [
    { key: 'connected' as const, label: 'Connected', count: connectedRepos.length },
    ...(isAdmin ? [{ key: 'available' as const, label: 'Connect new' }] : []),
  ]

  return (
    <Layout>
      <PageHeader
        title="Repositories"
        description="The repositories TowerCrane can read: projects pick their manifests and code from here. Only admins can connect new ones."
      />

      {error && <Alert className="mb-6">{error}</Alert>}

      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <Tabs aria-label="Repository lists" items={tabs} value={activeTab} onChange={setActiveTab} className="flex-1" />
        <label className="relative w-full sm:w-72">
          <span className="sr-only">Search repositories</span>
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search repositories..."
            className={cn(inputClass, 'pl-9')}
          />
        </label>
      </div>

      {loading ? (
        <Spinner label="Loading repositories..." />
      ) : filteredRepos.length === 0 ? (
        <EmptyState
          icon={GitBranch}
          title={
            search
              ? 'No repositories match your search'
              : activeTab === 'available'
                ? 'No available repositories'
                : 'No connected repositories yet'
          }
          description={
            activeTab === 'available'
              ? 'Repositories the TowerCrane GitHub App can access show up here. Connect GitHub from Integrations.'
              : isAdmin
                ? 'Connect a repository from the "Connect new" tab to get started.'
                : 'Ask a TowerCrane admin to connect a repository.'
          }
        />
      ) : (
        <Card>
          <ul className="divide-y divide-line">
            {filteredRepos.map((repo) => (
              <li
                key={repo.id ?? repo.full_name}
                className="flex flex-col justify-between gap-4 px-5 py-4 transition hover:bg-canvas lg:flex-row lg:items-center"
              >
                <div className="min-w-0 space-y-1">
                  <h3 className="truncate text-base font-semibold text-ink">{repo.full_name}</h3>
                  <MetaLine
                    items={[
                      <span key="vis" className="inline-flex items-center gap-1">
                        {repo.private ? <Lock className="h-3 w-3" /> : <Globe className="h-3 w-3" />}
                        {repo.private ? 'private' : 'public'}
                      </span>,
                      repo.default_branch,
                      repo.account && `via ${repo.account}`,
                    ]}
                  />
                  {repo.description && <p className="line-clamp-2 text-sm text-muted">{repo.description}</p>}
                  {activeTab === 'connected' && repo.id && <RepoUsage repoId={repo.id} projects={projects} />}
                </div>

                <div className="flex flex-shrink-0 flex-wrap items-center gap-2">
                  {activeTab === 'available' ? (
                    <Button
                      variant="primary"
                      onClick={() => connectRepository(repo.owner, repo.name, repo.installation_id)}
                      loading={connectingKey === repo.full_name}
                    >
                      Connect
                    </Button>
                  ) : (
                    <>
                      {repo.id && <RefreshButton repoId={repo.id} size="sm" />}
                      <ButtonLink href={repo.url} variant="ghost" size="sm" aria-label={`Open ${repo.full_name} on GitHub`}>
                        <ExternalLink className="h-4 w-4" />
                        GitHub
                      </ButtonLink>
                    </>
                  )}
                </div>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </Layout>
  )
}

/** Which projects use a repository, and how (manifests or code). */
function RepoUsage({ repoId, projects }: { repoId: string; projects: Project[] }) {
  const uses = projects.flatMap((p) =>
    p.id === repoId
      ? [{ p, role: 'manifests' }]
      : p.source_repositories.some((r) => r.id === repoId)
        ? [{ p, role: 'code' }]
        : []
  )
  return (
    <div className="flex flex-wrap items-center gap-1.5 pt-1 text-xs">
      <FolderGit2 className="h-3.5 w-3.5 text-muted" aria-hidden="true" />
      {uses.length === 0 ? (
        <span className="text-muted">Not used by any project yet</span>
      ) : (
        uses.map(({ p, role }) => (
          <Link
            key={p.id + role}
            to={`/projects/${p.id}`}
            className="rounded-full bg-primary-soft px-2.5 py-0.5 font-medium text-primary-ink hover:bg-primary hover:text-white"
          >
            {p.name} · {role}
          </Link>
        ))
      )}
    </div>
  )
}
