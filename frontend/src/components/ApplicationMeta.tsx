import type { MapApplication } from '../api/resourceMap'
import { MetaLine } from './ui/MetaLine'

/** path · auto|manual · OpenTofu[+ Terragrunt] · cloud account · edit: all */
export function ApplicationMeta({ app, showPath = true, className }: { app: MapApplication; showPath?: boolean; className?: string }) {
  return (
    <MetaLine
      className={className}
      items={[
        showPath && app.path,
        app.apply === 'auto' ? 'auto-apply' : 'manual',
        `${app.engine === 'terraform' ? 'Terraform' : 'OpenTofu'}${app.terragrunt ? ' + Terragrunt' : ''}`,
        app.cloud_account,
        app.edit_all && 'edit: all',
      ]}
    />
  )
}
