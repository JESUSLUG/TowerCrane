import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'

function authHeaders() {
  return { Authorization: `Bearer ${localStorage.getItem('auth_token')}` }
}

export interface JobRun {
  id: string
  job: string
  application: string
  action: 'apply' | 'destroy'
  /** schedule, after (destroy.after), manual, catch-up (late, after TowerCrane was down) */
  trigger: string
  status: 'running' | 'success' | 'failed' | 'skipped'
  operation_id?: string
  message?: string
  created_at: string
  finished_at?: string
}

/** A Job (kind: Job): scheduled/on-demand apply and destroy of an ephemeral Application. */
export interface Job {
  name: string
  file: string
  /** Application instance ("app@group"). */
  application: string
  group?: string
  description?: string
  timezone: string
  apply_schedule?: string
  destroy_schedule?: string
  destroy_after?: string
  manual: boolean
  /** Its resources exist (last successful apply/destroy was an apply). */
  up: boolean
  up_since?: string
  next_apply?: string
  next_destroy?: string
  running: boolean
  runs: JobRun[]
}

export const jobAPI = {
  async list(repoId: string): Promise<Job[]> {
    const res = await axios.get(`${API_URL}/repositories/${repoId}/jobs`, { headers: authHeaders() })
    return res.data.jobs
  },
  async run(repoId: string, job: string, action: 'apply' | 'destroy'): Promise<JobRun> {
    const res = await axios.post(
      `${API_URL}/repositories/${repoId}/jobs/${encodeURIComponent(job)}/run`,
      { action },
      { headers: authHeaders() }
    )
    return res.data
  },
}
