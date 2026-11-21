import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Cloud, GitBranch, ShieldCheck } from 'lucide-react'
import { projectAPI, type Project } from '../api/projects'
import { apiError, integrationAPI, type CloudAccount } from '../api/admin'
import { Button } from './ui/Button'
import { Alert } from './ui/Alert'

/**
 * What a project defined by a Project manifest still needs: cloud accounts
 * it requests that an admin hasn't authorized, and code repositories it
 * lists that aren't connected.
 */
export function ProjectNotices({ project, isAdmin, onChange }: { project: Project; isAdmin: boolean; onChange: (p: Project) => void }) {
  const [accounts, setAccounts] = useState<CloudAccount[]>([])
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    integrationAPI.cloudAccounts().then(setAccounts).catch(() => undefined)
  }, [])

  const byName = new Map(accounts.map((a) => [a.name, a]))
  const pending = project.requested_cloud_accounts.filter((name) => {
    const a = byName.get(name)
    return !a || !project.cloud_account_ids.includes(a.id)
  })
  if (pending.length === 0 && project.missing_sources.length === 0 && !error) return null

  const authorize = async (account: CloudAccount) => {
    setBusy(account.id)
    setError(null)
    try {
      onChange(await projectAPI.update(project.id, { cloud_account_ids: [...project.cloud_account_ids, account.id] }))
    } catch (err) {
      setError(apiError(err, 'Failed to authorize the cloud account'))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="mb-6 space-y-3">
      {error && <Alert>{error}</Alert>}
      {pending.map((name) => {
        const account = byName.get(name)
        return (
          <Alert key={name} tone="attention">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span className="flex items-center gap-2">
                <Cloud className="h-4 w-4 flex-shrink-0" aria-hidden="true" />
                {account ? (
                  <span>
                    <code className="font-mono font-semibold">{project.manifest_file}</code> asks for the cloud account{' '}
                    <code className="font-mono font-semibold">{name}</code>. Its Applications can't use it until an admin authorizes it.
                  </span>
                ) : (
                  <span>
                    <code className="font-mono font-semibold">{project.manifest_file}</code> asks for{' '}
                    <code className="font-mono font-semibold">{name}</code>, which doesn't exist in TowerCrane:{' '}
                    {isAdmin ? (
                      <Link to="/integrations?tab=cloud" className="underline">
                        create it in Integrations
                      </Link>
                    ) : (
                      'an admin must create it'
                    )}
                    .
                  </span>
                )}
              </span>
              {account && isAdmin && (
                <Button size="sm" variant="attention" loading={busy === account.id} onClick={() => authorize(account)}>
                  <ShieldCheck className="h-4 w-4" /> Authorize {name}
                </Button>
              )}
            </div>
          </Alert>
        )
      })}
      {project.missing_sources.length > 0 && (
        <Alert tone="attention">
          <span className="flex items-center gap-2">
            <GitBranch className="h-4 w-4 flex-shrink-0" aria-hidden="true" />
            <span>
              Code repositories listed in <code className="font-mono font-semibold">{project.manifest_file}</code> aren't connected:{' '}
              <code className="font-mono font-semibold">{project.missing_sources.join(', ')}</code>.{' '}
              {isAdmin ? (
                <Link to="/repositories" className="underline">
                  Connect them in Repositories
                </Link>
              ) : (
                'An admin must connect them'
              )}
              .
            </span>
          </span>
        </Alert>
      )}
    </div>
  )
}
