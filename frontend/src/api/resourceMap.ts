import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'

export type MapNodeKind = 'module' | 'resource' | 'data'
export type MapEdgeKind = 'contains' | 'depends-on'
export type SyncStatus = 'synced' | 'out-of-sync' | 'running' | 'error' | 'destroyed' | 'unknown'

export interface MapNode {
  id: string
  address: string
  label: string
  kind: MapNodeKind
  dir: string
  file_path: string
  parent_id?: string
  start_line?: number
  end_line?: number
  /** Applications (manifest names) whose module tree includes this block. */
  applications: string[]
  /** Some Application's editScope allows changing this block. */
  editable: boolean
  /** HCL source; only when an Application sets display.showSource. */
  source?: string
}

export interface MapEdge {
  from: string
  to: string
  kind: MapEdgeKind
}

/** "attached": the repository may use it; "not_attached": exists in TowerCrane but not attached; "unknown": no such account. */
export type CloudAccountStatus = 'attached' | 'not_attached' | 'unknown'

/**
 * An Application instance declared in .towercrane/: the Application itself,
 * or one instance per group ("app@group").
 */
export interface MapApplication {
  /** Instance name: "app" or "app@group". */
  name: string
  /** The manifest's metadata.name. */
  app: string
  group?: string
  path: string
  file: string
  apply: 'auto' | 'manual'
  /** The group's cloud account (or spec.destination.cloudAccount). */
  cloud_account?: string
  cloud_account_status?: CloudAccountStatus
  /** "job": ephemeral, applied and destroyed by its Job (never by a rollout). */
  lifecycle?: 'job'
  /** Name of the Job that runs it. */
  job?: string
  /** Instances of earlier groups (Group spec.after) applied first. */
  promotion: string[]
  edit_all: boolean
  status: SyncStatus
  /** spec.syncPolicy.dependsOn: applied before this one. */
  depends_on: string[]
  /** Applications that depend on this one. */
  dependents: string[]
  /** Applications using some of the same module directories. */
  shares_modules_with: string[]
  resource_count: number
  engine: 'opentofu' | 'terraform'
  /** source.path is a Terragrunt unit. */
  terragrunt: boolean
  /** Repository with its code (owner/name): spec.source.repo or the configuration repository. */
  source_repo: string
  /** The manifest YAML as it is in the repository. */
  manifest: string
}

/** A Group manifest: where instances are applied, with which cloud account. */
export interface MapGroup {
  name: string
  file: string
  description?: string
  /** Groups whose instances must all succeed first (promotion order). */
  after: string[]
  cloud_account: string
  cloud_account_status: CloudAccountStatus
  /** Details of the cloud account, when it is attached to the repository. */
  account?: {
    name: string
    provider: 'aws'
    region: string
    aws_account_id: string
    endpoint_url?: string
    verified: boolean
  }
  instances: string[]
  labels?: Record<string, string>
  manifest: string
}

export type DiagramColor = 'red' | 'orange' | 'yellow' | 'green' | 'teal' | 'blue' | 'purple' | 'pink' | 'gray'

/** A Diagram manifest (kind: Diagram): how a group is drawn. Visual only. */
export interface DiagramSpec {
  group: string
  sections: { name: string; color?: DiagramColor; apps: string[] }[] | null
  nodes: Record<string, { color?: DiagramColor; label?: string }> | null
  links: { from: string; to: string; label?: string }[] | null
}

/** The Diagram merged on the branch (merging is the approval). */
export interface DiagramView {
  group: string
  file: string
  spec: DiagramSpec | null
}

export interface ResourceMap {
  /** Commit the view was built from (default branch: what TowerCrane last read). */
  commit_sha?: string
  /** When TowerCrane read it from GitHub (every poll interval or on Refresh). */
  read_at?: string
  diagrams: DiagramView[] | null
  manifest_warnings: string[] | null
  applications: MapApplication[] | null
  groups: MapGroup[] | null
  manifest_errors: string[] | null
  nodes: MapNode[] | null
  edges: MapEdge[] | null
  branch: string
}

function authHeaders() {
  return { Authorization: `Bearer ${localStorage.getItem('auth_token')}` }
}

export const resourceMapAPI = {
  async get(repositoryId: string, opts: { branch?: string; app?: string } = {}): Promise<ResourceMap> {
    const params = new URLSearchParams()
    if (opts.branch) params.append('branch', opts.branch)
    if (opts.app) params.append('app', opts.app)
    const response = await axios.get(`${API_URL}/repositories/${repositoryId}/map?${params.toString()}`, {
      headers: authHeaders(),
    })
    return response.data
  },
}
