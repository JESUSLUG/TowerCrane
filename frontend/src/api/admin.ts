import axios from 'axios'
import type { Role, RoleSources } from '../lib/roles'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'

function authHeaders() {
  return { Authorization: `Bearer ${localStorage.getItem('auth_token')}` }
}

export interface TeamUser {
  id: string
  github_login: string
  email: string
  name: string
  avatar_url: string
  role: Role
  role_sources: RoleSources
}

export interface RoleMapping {
  id: string
  github_org: string
  team_slug: string
  role: Role
  created_at: string
}


export function apiError(err: unknown, fallback: string): string {
  return (axios.isAxiosError(err) && err.response?.data?.error) || fallback
}

export const adminAPI = {
  async listUsers(): Promise<TeamUser[]> {
    const res = await axios.get(`${API_URL}/admin/users`, { headers: authHeaders() })
    return res.data.users
  },
  async setUserRole(userId: string, role: Role | ''): Promise<TeamUser> {
    const res = await axios.put(`${API_URL}/admin/users/${userId}/role`, { role }, { headers: authHeaders() })
    return res.data
  },
  async listRoleMappings(): Promise<RoleMapping[]> {
    const res = await axios.get(`${API_URL}/admin/role-mappings`, { headers: authHeaders() })
    return res.data.mappings
  },
  async upsertRoleMapping(team: string, role: Role): Promise<RoleMapping> {
    const res = await axios.put(`${API_URL}/admin/role-mappings`, { team, role }, { headers: authHeaders() })
    return res.data
  },
  async deleteRoleMapping(id: string): Promise<void> {
    await axios.delete(`${API_URL}/admin/role-mappings/${id}`, { headers: authHeaders() })
  },
}


export interface Invitation {
  id: string
  email: string
  role: Role
  created_at: string
  expires_at: string
  accepted_by?: string
  accepted_at?: string
}

export const invitationAPI = {
  async list(): Promise<Invitation[]> {
    const res = await axios.get(`${API_URL}/admin/invitations`, { headers: authHeaders() })
    return res.data.invitations
  },
  async create(email: string, role: Role): Promise<{ invitation: Invitation; invite_url: string }> {
    const res = await axios.post(`${API_URL}/admin/invitations`, { email, role }, { headers: authHeaders() })
    return res.data
  },
  async revoke(id: string): Promise<void> {
    await axios.delete(`${API_URL}/admin/invitations/${id}`, { headers: authHeaders() })
  },
}

export interface Installation {
  installation_id: number
  account_login: string
  account_type: 'Organization' | 'User'
  repository_selection: 'all' | 'selected'
  created_at: string
}

export interface GitHubIntegration {
  app_configured: boolean
  app_slug?: string
  install_url?: string
  webhook_secret: boolean
  /** 0 = polling disabled. */
  poll_interval_seconds: number
  installations: Installation[]
  /** On GitHub but not registered in TowerCrane yet. */
  pending_installations?: Omit<Installation, 'created_at'>[]
  app_error?: string
}

export interface CloudAccount {
  id: string
  name: string
  provider: 'aws'
  aws_account_id: string
  access_key_id: string // masked
  region: string
  endpoint_url?: string
  created_at: string
  verified_at?: string
  verify_error?: string
}

export interface NewCloudAccount {
  name: string
  aws_account_id: string
  access_key_id: string
  secret_access_key: string
  region: string
  endpoint_url?: string
}

export const integrationAPI = {
  async github(): Promise<GitHubIntegration> {
    const res = await axios.get(`${API_URL}/integrations/github`, { headers: authHeaders() })
    return res.data
  },
  async addInstallation(installationId: number): Promise<Installation> {
    const res = await axios.post(
      `${API_URL}/integrations/github/installations`,
      { installation_id: installationId },
      { headers: authHeaders() }
    )
    return res.data
  },
  async removeInstallation(installationId: number): Promise<void> {
    await axios.delete(`${API_URL}/integrations/github/installations/${installationId}`, { headers: authHeaders() })
  },
  async cloudAccounts(): Promise<CloudAccount[]> {
    const res = await axios.get(`${API_URL}/cloud-accounts`, { headers: authHeaders() })
    return res.data.accounts
  },
  async createCloudAccount(data: NewCloudAccount): Promise<CloudAccount> {
    const res = await axios.post(`${API_URL}/cloud-accounts`, { provider: 'aws', ...data }, { headers: authHeaders() })
    return res.data
  },
  async verifyCloudAccount(id: string): Promise<CloudAccount> {
    const res = await axios.post(`${API_URL}/cloud-accounts/${id}/verify`, {}, { headers: authHeaders() })
    return res.data
  },
  async deleteCloudAccount(id: string): Promise<void> {
    await axios.delete(`${API_URL}/cloud-accounts/${id}`, { headers: authHeaders() })
  },
  /** The cloud accounts a repository's manifests may use (replaces the list). */
  async setRepositoryCloudAccounts(repoId: string, cloudAccountIds: string[]): Promise<void> {
    await axios.put(
      `${API_URL}/repositories/${repoId}/cloud-accounts`,
      { cloud_account_ids: cloudAccountIds },
      { headers: authHeaders() }
    )
  },
}

export interface RefreshResult {
  commit_sha: string
  processing: boolean
  message: string
}

export const repoAPI = {
  /** Check GitHub now; force re-plans the current commit even if already handled. */
  async refresh(repoId: string, force = false): Promise<RefreshResult> {
    const res = await axios.post(`${API_URL}/repositories/${repoId}/refresh${force ? '?force=true' : ''}`, {}, { headers: authHeaders() })
    return res.data
  },
}
