import { Link } from 'react-router-dom'
import { Cloud, Layers } from 'lucide-react'
import type { CloudAccountStatus, MapApplication, MapGroup } from '../api/resourceMap'
import { syncStatus, type StatusInfo } from '../lib/status'
import { Card, CardBody } from './ui/Card'
import { MetaLine } from './ui/MetaLine'
import { StatusBadge, StatusDot } from './ui/StatusBadge'

/** Whether a repository can use the cloud account a manifest names. */
export const cloudAccountStatus: Record<CloudAccountStatus, StatusInfo> = {
  attached: { state: 'ok', label: 'Attached' },
  not_attached: { state: 'attention', label: 'Not attached to this repository' },
  unknown: { state: 'error', label: 'Unknown cloud account' },
}

/** "aws · us-east-1 · 000000000000 · http://localhost:4566" */
export function accountDetails(group: MapGroup) {
  const a = group.account
  if (!a) return null
  return <MetaLine items={[a.provider, a.region, a.aws_account_id, a.endpoint_url]} />
}

/** "Cloud provider: floci-example" plus its details and status. */
export function GroupCloudLine({ group }: { group: MapGroup }) {
  return (
    <div className="space-y-1">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Cloud className="h-4 w-4 text-muted" aria-hidden="true" />
        <span className="text-muted">Cloud provider</span>
        <span className="font-mono font-semibold text-ink">{group.cloud_account}</span>
        {group.cloud_account_status !== 'attached' && <StatusBadge {...cloudAccountStatus[group.cloud_account_status]} />}
      </div>
      {accountDetails(group)}
    </div>
  )
}

/** A group on the repository page: its cloud account and its Applications. */
export function GroupCard({ repoId, group, apps }: { repoId: string; group: MapGroup; apps: MapApplication[] }) {
  return (
    <Link
      to={`/projects/${repoId}/groups/${encodeURIComponent(group.name)}`}
      className="group block rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
    >
      <Card className="h-full transition group-hover:border-primary/40 group-hover:shadow-pop">
        <CardBody className="space-y-3">
          <div className="flex items-start justify-between gap-2">
            <p className="flex min-w-0 items-center gap-2 truncate text-base font-semibold text-ink group-hover:text-primary">
              <Layers className="h-4 w-4 flex-shrink-0 text-primary" aria-hidden="true" />
              {group.name}
            </p>
          </div>
          {group.description && <p className="text-sm text-muted">{group.description}</p>}
          <GroupCloudLine group={group} />
          <div className="space-y-2 border-t border-line pt-3">
            <ul className="flex flex-wrap gap-x-3 gap-y-1.5">
              {apps.map((a) => (
                <li key={a.name} className="flex items-center gap-1.5 font-mono text-xs text-ink">
                  <StatusDot {...syncStatus[a.status ?? 'unknown']} />
                  {a.app}
                </li>
              ))}
              {apps.length === 0 && <li className="text-xs text-muted">No Applications yet</li>}
            </ul>
            {group.after.length > 0 && <p className="text-xs text-muted">promoted after {group.after.join(', ')}</p>}
          </div>
        </CardBody>
      </Card>
    </Link>
  )
}
