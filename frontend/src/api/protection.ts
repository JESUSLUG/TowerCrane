import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'

export interface ProtectionCheck {
  id: 'branch_protected' | 'requires_pr' | 'codeowners' | 'code_owner_review'
  label: string
  state: 'ok' | 'missing' | 'unknown'
  detail: string
}

/** How well GitHub protects a repository's TowerCrane policy (.towercrane/). */
export interface ProtectionReport {
  branch: string
  level: 'ok' | 'warning' | 'unknown'
  checks: ProtectionCheck[]
}

export const protectionAPI = {
  async get(repoId: string): Promise<ProtectionReport> {
    const res = await axios.get(`${API_URL}/repositories/${repoId}/protection`, {
      headers: { Authorization: `Bearer ${localStorage.getItem('auth_token')}` },
    })
    return res.data
  },
}
