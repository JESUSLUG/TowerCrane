import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'

function authHeaders() {
  return { Authorization: `Bearer ${localStorage.getItem('auth_token')}` }
}

/** A connected repository, as projects reference it. */
export interface ProjectRepository {
  id: string
  name: string
  full_name: string
  url: string
  default_branch: string
  private: boolean
}

/**
 * Where TowerCrane works: a configuration repository (.towercrane/: groups,
 * Applications, Jobs) plus the repositories holding the code they run
 * (spec.source.repo). Its id is the configuration repository's id.
 */
export interface Project {
  id: string
  name: string
  created_at: string
  /** Folder of the configuration repository with the manifests ("" = the whole repository). */
  manifests_dir: string
  /** The Project manifest (kind: Project) defining it; absent for projects created by hand. */
  manifest_file?: string
  /** Cloud accounts the Project manifest asks for (names); usable once an admin authorizes them. */
  requested_cloud_accounts: string[]
  /** Code repositories the manifest lists that aren't connected in TowerCrane. */
  missing_sources: string[]
  config_repository: ProjectRepository
  source_repositories: ProjectRepository[]
  cloud_account_ids: string[]
}

export interface NewProject {
  name: string
  config_repository_id: string
  source_repository_ids: string[]
  cloud_account_ids: string[]
  /** ".towercrane" (default) or "/" for the whole repository. */
  manifests_dir?: string
}

export const projectAPI = {
  async list(): Promise<Project[]> {
    const res = await axios.get(`${API_URL}/projects`, { headers: authHeaders() })
    return res.data.projects
  },
  async get(id: string): Promise<Project> {
    const res = await axios.get(`${API_URL}/projects/${id}`, { headers: authHeaders() })
    return res.data
  },
  async create(p: NewProject): Promise<Project> {
    const res = await axios.post(`${API_URL}/projects`, p, { headers: authHeaders() })
    return res.data
  },
  async update(
    id: string,
    changes: Partial<{ name: string; source_repository_ids: string[]; cloud_account_ids: string[]; manifests_dir: string }>
  ): Promise<Project> {
    const res = await axios.put(`${API_URL}/projects/${id}`, changes, { headers: authHeaders() })
    return res.data
  },
  async remove(id: string): Promise<void> {
    await axios.delete(`${API_URL}/projects/${id}`, { headers: authHeaders() })
  },
}
