import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'

function authHeaders() {
  return { Authorization: `Bearer ${localStorage.getItem('auth_token')}` }
}

export type RolloutItemStatus =
  | 'waiting'
  | 'planning'
  | 'awaiting_yes'
  | 'applying'
  | 'applied'
  | 'no_changes'
  | 'blocked'
  | 'rejected'
  | 'failed'
  | 'discarded'
  | 'skipped'
  | 'superseded'
  /** Not planned: none of its files changed since it was last applied. */
  | 'unchanged'

export interface RolloutItem {
  rollout_id: string
  application: string
  position: number
  depends_on: string[]
  status: RolloutItemStatus
  paused: boolean
  plan_op_id?: string
  apply_op_id?: string
  message?: string
  updated_at: string
}

/** What TowerCrane does with one commit: every Application, in dependsOn order. */
export interface Rollout {
  id: string
  repository_id: string
  commit_sha: string
  status: 'running' | 'done' | 'superseded'
  created_at: string
  updated_at: string
  items: RolloutItem[]
}

export const rolloutAPI = {
  async list(repoId: string): Promise<Rollout[]> {
    const res = await axios.get(`${API_URL}/repositories/${repoId}/rollouts`, { headers: authHeaders() })
    return res.data.rollouts
  },
  async reorder(rolloutId: string, order: string[]): Promise<Rollout> {
    const res = await axios.put(`${API_URL}/rollouts/${rolloutId}/order`, { order }, { headers: authHeaders() })
    return res.data
  },
  async action(rolloutId: string, application: string, action: 'pause' | 'resume' | 'retry'): Promise<Rollout> {
    const res = await axios.post(`${API_URL}/rollouts/${rolloutId}/items/${application}/${action}`, {}, { headers: authHeaders() })
    return res.data
  },
}
