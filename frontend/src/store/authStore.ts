import { clearResourceMapCache } from '../hooks/useResourceMap'
import { create } from 'zustand'
import axios from 'axios'
import type { Role, RoleSources } from '../lib/roles'

export interface User {
  id: string
  github_id: number
  github_login: string
  email: string
  name: string
  avatar_url: string
  role: Role
  role_sources: RoleSources
}

interface AuthState {
  user: User | null
  token: string | null
  isLoading: boolean
  error: string | null
  
  // Actions
  setToken: (token: string | null) => void
  setUser: (user: User | null) => void
  setLoading: (loading: boolean) => void
  setError: (error: string | null) => void
  logout: () => void
  fetchCurrentUser: () => Promise<void>
  /** Stores a new session token and loads the full user (including role). */
  startSession: (token: string) => Promise<void>
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  token: localStorage.getItem('auth_token'),
  isLoading: false,
  error: null,

  setToken: (token) => {
    if (token) {
      localStorage.setItem('auth_token', token)
      axios.defaults.headers.common['Authorization'] = `Bearer ${token}`
    } else {
      localStorage.removeItem('auth_token')
      delete axios.defaults.headers.common['Authorization']
    }
    set({ token })
  },

  setUser: (user) => set({ user }),

  setLoading: (loading) => set({ isLoading: loading }),

  setError: (error) => set({ error }),

  logout: () => {
    // End the session on the server too (the token stops working everywhere).
    const token = localStorage.getItem('auth_token')
    if (token) {
      fetch('/api/v1/auth/logout', { method: 'POST', headers: { Authorization: `Bearer ${token}` } }).catch(() => {})
    }
    localStorage.removeItem('auth_token')
    clearResourceMapCache()
    delete axios.defaults.headers.common['Authorization']
    set({ token: null, user: null })
  },

  startSession: async (token) => {
    useAuthStore.getState().setToken(token)
    await useAuthStore.getState().fetchCurrentUser()
  },

  fetchCurrentUser: async () => {
    const { token, setUser, setError } = useAuthStore.getState()
    if (!token) return

    try {
      const response = await axios.get('/api/v1/user', {
        headers: { Authorization: `Bearer ${token}` },
      })
      setUser(response.data)
    } catch (err) {
      setError('Failed to fetch user')
      console.error(err)
    }
  },
}))
