import { FormEvent, useCallback, useEffect, useState } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { Building2, Cloud, ExternalLink, Plus, RefreshCw, ShieldCheck, Trash2, User as UserIcon } from 'lucide-react'
import { Layout } from '../components/Layout'
import { Card, CardBody, CardHeader, CardTitle } from '../components/ui/Card'
import { Button, ButtonLink } from '../components/ui/Button'
import { Alert } from '../components/ui/Alert'
import { Spinner } from '../components/ui/Spinner'
import { EmptyState } from '../components/ui/EmptyState'
import { PageHeader } from '../components/ui/PageHeader'
import { Tabs } from '../components/ui/Tabs'
import { StatusBadge } from '../components/ui/StatusBadge'
import { MetaLine } from '../components/ui/MetaLine'
import { Field, Input } from '../components/ui/Field'
import { GithubIcon } from '../components/ui/GithubIcon'
import { useAuth } from '../hooks/useAuth'
import { atLeast } from '../lib/roles'
import {
  apiError,
  integrationAPI,
  type CloudAccount,
  type GitHubIntegration,
  type NewCloudAccount,
} from '../api/admin'

type Tab = 'github' | 'cloud'

export function IntegrationsPage() {
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const tab: Tab = params.get('tab') === 'cloud' ? 'cloud' : 'github'

  if (user && !atLeast(user.role, 'admin')) {
    return <Navigate to="/dashboard" replace />
  }

  return (
    <Layout>
      <PageHeader title="Integrations" description="Connect GitHub and the cloud accounts TowerCrane applies to." />

      <Tabs
        className="mb-6"
        aria-label="Integrations"
        value={tab}
        onChange={(key) => setParams({ tab: key })}
        items={[
          { key: 'github', label: 'GitHub', icon: GithubIcon },
          { key: 'cloud', label: 'Cloud providers', icon: Cloud },
        ]}
      />

      {tab === 'github' ? <GitHubTab /> : <CloudTab />}
    </Layout>
  )
}

// --- GitHub ---------------------------------------------------------------------

function GitHubTab() {
  const [data, setData] = useState<GitHubIntegration | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    integrationAPI
      .github()
      .then(setData)
      .catch((err) => setError(apiError(err, 'Failed to load the GitHub integration')))
  }, [])
  useEffect(load, [load])

  const [registering, setRegistering] = useState<number | null>(null)
  const register = async (id: number) => {
    setRegistering(id)
    setError(null)
    try {
      await integrationAPI.addInstallation(id)
      load()
    } catch (err) {
      setError(apiError(err, 'Failed to connect the installation'))
    } finally {
      setRegistering(null)
    }
  }

  const remove = async (id: number) => {
    setError(null)
    try {
      await integrationAPI.removeInstallation(id)
      load()
    } catch (err) {
      setError(apiError(err, 'Failed to remove the installation'))
    }
  }

  if (!data) return error ? <Alert>{error}</Alert> : <Spinner label="Loading..." />

  return (
    <div className="space-y-6">
      {error && <Alert>{error}</Alert>}

      {!data.app_configured ? (
        <Alert tone="attention">
          <p className="font-semibold">The TowerCrane GitHub App is not configured on the server</p>
          <p className="mt-1">
            Create a GitHub App (GitHub → Settings → Developer settings → GitHub Apps), then set{' '}
            <code className="font-mono">GITHUB_APP_ID</code>, <code className="font-mono">GITHUB_APP_SLUG</code>,{' '}
            <code className="font-mono">GITHUB_APP_PRIVATE_KEY_PATH</code> and{' '}
            <code className="font-mono">GITHUB_WEBHOOK_SECRET</code> in the backend and restart it. See docs/GITHUB_APP_SETUP.md.
          </p>
        </Alert>
      ) : (
        <Card>
          <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <CardTitle
              title="Connected accounts"
              description="Install the app on an organization (or your user account) and choose all repositories or only some. Members of connected organizations can sign in to TowerCrane."
            />
            {data.install_url && (
              <ButtonLink href={data.install_url} variant="primary" className="flex-shrink-0">
                <GithubIcon className="h-4 w-4" />
                Connect organization or repos
              </ButtonLink>
            )}
          </CardHeader>
          <CardBody>
            <p className="mb-4 text-xs text-muted">
              {data.poll_interval_seconds > 0
                ? `TowerCrane checks GitHub for new commits and pull requests every ${formatInterval(data.poll_interval_seconds)}. `
                : 'Polling is disabled (TOWERCRANE_POLL_INTERVAL=off). '}
              {data.webhook_secret
                ? 'Webhooks are enabled, so changes are picked up instantly.'
                : 'Webhooks are not configured (optional): set GITHUB_WEBHOOK_SECRET once TowerCrane has a public URL to react instantly.'}
            </p>
            {data.poll_interval_seconds === 0 && !data.webhook_secret && (
              <Alert className="mb-4">
                Neither polling nor webhooks are enabled: TowerCrane won't notice new commits or pull requests.
              </Alert>
            )}
            {data.app_error && <Alert className="mb-4">{data.app_error}</Alert>}
            {(data.pending_installations?.length ?? 0) > 0 && (
              <Alert tone="attention" className="mb-4">
                <p className="mb-2">The app is installed on these accounts but TowerCrane hasn't connected them yet:</p>
                <ul className="space-y-2">
                  {data.pending_installations!.map((inst) => (
                    <li key={inst.installation_id} className="flex items-center justify-between gap-3">
                      <span className="text-ink">
                        <span className="font-semibold">{inst.account_login}</span>{' '}
                        <span className="text-xs">
                          ({inst.account_type}, {inst.repository_selection === 'all' ? 'all repositories' : 'selected repositories'})
                        </span>
                      </span>
                      <Button size="sm" loading={registering === inst.installation_id} onClick={() => register(inst.installation_id)}>
                        Connect
                      </Button>
                    </li>
                  ))}
                </ul>
              </Alert>
            )}
            {data.installations.length === 0 ? (
              <EmptyState
                icon={Building2}
                title="No GitHub accounts connected"
                description="Use “Connect organization or repos” to install the TowerCrane app."
              />
            ) : (
              <ul className="divide-y divide-line rounded-lg border border-line">
                {data.installations.map((inst) => (
                  <li key={inst.installation_id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                    <div className="flex items-center gap-3">
                      <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary-soft text-primary">
                        {inst.account_type === 'Organization' ? <Building2 className="h-4 w-4" /> : <UserIcon className="h-4 w-4" />}
                      </span>
                      <div>
                        <p className="font-medium text-ink">{inst.account_login}</p>
                        <MetaLine
                          items={[
                            inst.account_type,
                            inst.repository_selection === 'all' ? 'all repositories' : 'selected repositories',
                          ]}
                        />
                      </div>
                    </div>
                    <div className="flex gap-2">
                      {data.app_slug && (
                        <ButtonLink
                          variant="ghost"
                          size="sm"
                          href={
                            inst.account_type === 'Organization'
                              ? `https://github.com/organizations/${inst.account_login}/settings/installations/${inst.installation_id}`
                              : `https://github.com/settings/installations/${inst.installation_id}`
                          }
                        >
                          <ExternalLink className="h-4 w-4" /> Change repos
                        </ButtonLink>
                      )}
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => remove(inst.installation_id)}
                        aria-label={`Disconnect ${inst.account_login}`}
                        title="Disconnect"
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </CardBody>
        </Card>
      )}

      <Alert tone="info">
        <p className="flex items-center gap-1.5 font-semibold">
          <ShieldCheck className="h-4 w-4" /> Block merges until policy changes are approved
        </p>
        <p className="mt-1">
          TowerCrane reports a <code className="font-mono">towercrane/policy</code> status on every pull request. In the
          repository's branch protection rules for the default branch, enable “Require status checks to pass” and select{' '}
          <code className="font-mono">towercrane/policy</code>.
        </p>
      </Alert>
    </div>
  )
}

function formatInterval(seconds: number): string {
  if (seconds % 60 === 0) {
    const m = seconds / 60
    return m === 1 ? 'minute' : `${m} minutes`
  }
  return `${seconds} seconds`
}

/** GitHub sends admins here (the app's Setup URL) after installing the app. */
export function GitHubSetupCallback() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const id = Number(params.get('installation_id'))
    if (!id) {
      navigate('/integrations', { replace: true })
      return
    }
    integrationAPI
      .addInstallation(id)
      .then(() => navigate('/integrations', { replace: true }))
      .catch((err) => setError(apiError(err, 'Failed to register the installation')))
  }, [params, navigate])

  return <Layout>{error ? <Alert>{error}</Alert> : <Spinner label="Connecting GitHub..." />}</Layout>
}

// --- Cloud providers --------------------------------------------------------------

const emptyAccount: NewCloudAccount = {
  name: '',
  aws_account_id: '',
  access_key_id: '',
  secret_access_key: '',
  region: 'us-east-1',
  endpoint_url: '',
}

function CloudTab() {
  const [accounts, setAccounts] = useState<CloudAccount[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [form, setForm] = useState<NewCloudAccount>(emptyAccount)
  const [showForm, setShowForm] = useState(false)
  const [saving, setSaving] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)

  const load = useCallback(() => {
    integrationAPI
      .cloudAccounts()
      .then(setAccounts)
      .catch((err) => setError(apiError(err, 'Failed to load cloud accounts')))
  }, [])
  useEffect(load, [load])

  const set = (key: keyof NewCloudAccount) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [key]: e.target.value }))

  const create = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError(null)
    try {
      await integrationAPI.createCloudAccount(form)
      setForm(emptyAccount)
      setShowForm(false)
      load()
    } catch (err) {
      setError(apiError(err, 'Failed to save the cloud account'))
    } finally {
      setSaving(false)
    }
  }

  const verify = async (id: string) => {
    setBusy(id)
    try {
      const updated = await integrationAPI.verifyCloudAccount(id)
      setAccounts((list) => list?.map((a) => (a.id === id ? updated : a)) ?? null)
    } catch (err) {
      setError(apiError(err, 'Failed to verify the account'))
    } finally {
      setBusy(null)
    }
  }

  const remove = async (id: string) => {
    setBusy(id)
    try {
      await integrationAPI.deleteCloudAccount(id)
      setAccounts((list) => list?.filter((a) => a.id !== id) ?? null)
    } catch (err) {
      setError(apiError(err, 'Failed to delete the account'))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="space-y-6">
      {error && <Alert>{error}</Alert>}

      <Card>
        <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <CardTitle
            title="AWS accounts"
            description="Access keys are encrypted at rest and only injected into tofu for repositories that use the account. IAM role support is coming later."
          />
          {!showForm && (
            <Button onClick={() => setShowForm(true)} className="flex-shrink-0">
              <Plus className="h-4 w-4" /> Add AWS account
            </Button>
          )}
        </CardHeader>

        {showForm && (
          <CardBody className="border-b border-line bg-canvas">
            <form onSubmit={create} className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <Field label="Name">
                <Input value={form.name} onChange={set('name')} placeholder="floci-dev" required />
              </Field>
              <Field label="AWS account ID" hint="12 digits. Floci uses 000000000000.">
                <Input mono value={form.aws_account_id} onChange={set('aws_account_id')} pattern="\d{12}" required />
              </Field>
              <Field label="Access key ID">
                <Input mono value={form.access_key_id} onChange={set('access_key_id')} required autoComplete="off" />
              </Field>
              <Field label="Secret access key">
                <Input
                  mono
                  type="password"
                  value={form.secret_access_key}
                  onChange={set('secret_access_key')}
                  required
                  autoComplete="new-password"
                />
              </Field>
              <Field label="Region">
                <Input mono value={form.region} onChange={set('region')} required />
              </Field>
              <Field label="Endpoint URL (optional)" hint="For Floci: http://localhost:4566. Leave empty for real AWS.">
                <Input mono value={form.endpoint_url} onChange={set('endpoint_url')} />
              </Field>
              <div className="flex gap-2 md:col-span-2">
                <Button type="submit" loading={saving}>
                  Save and verify
                </Button>
                <Button variant="ghost" onClick={() => setShowForm(false)}>
                  Cancel
                </Button>
              </div>
            </form>
          </CardBody>
        )}

        <CardBody>
          {accounts === null ? (
            <Spinner />
          ) : accounts.length === 0 ? (
            <EmptyState icon={Cloud} title="No cloud accounts" description="Add an AWS account so TowerCrane can plan and apply." />
          ) : (
            <ul className="divide-y divide-line rounded-lg border border-line">
              {accounts.map((a) => (
                <li key={a.id} className="flex flex-col gap-3 px-4 py-3 md:flex-row md:items-center md:justify-between">
                  <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium text-ink">{a.name}</span>
                      <StatusBadge {...(a.verified_at ? { state: 'ok', label: 'Verified' } : { state: 'error', label: 'Not verified' })} />
                    </div>
                    <MetaLine items={['AWS', a.aws_account_id, a.access_key_id, a.region, a.endpoint_url]} />
                    {a.verify_error && <p className="break-words text-xs text-danger">{a.verify_error}</p>}
                  </div>
                  <div className="flex flex-shrink-0 gap-2">
                    <Button variant="secondary" size="sm" onClick={() => verify(a.id)} loading={busy === a.id}>
                      {busy !== a.id && <RefreshCw className="h-4 w-4" />} Verify
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => remove(a.id)}
                      disabled={busy === a.id}
                      aria-label={`Delete ${a.name}`}
                      title="Delete"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardBody>
      </Card>
    </div>
  )
}
