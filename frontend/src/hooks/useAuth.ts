import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuthStore } from '../store/authStore'

export const OAUTH_STATE_KEY = 'oauth_state'

export function useAuth() {
  const navigate = useNavigate()
  const { token, user, setToken, logout } = useAuthStore()

  useEffect(() => {
    // Check if user is authenticated and fetch user data
    if (token && !user) {
      useAuthStore.getState().fetchCurrentUser()
    }
  }, [token, user])

  // The OAuth state is "<nonce>~<purpose>". The nonce is kept in this tab and
  // checked when GitHub sends the browser back, so a sign-in nobody started
  // here is refused. Invitation links use "invite:<token>" as the purpose so
  // the backend can accept the invitation on sign-in.
  const login = async (purpose = 'web') => {
    try {
      const nonce = Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) => b.toString(16).padStart(2, '0')).join('')
      sessionStorage.setItem(OAUTH_STATE_KEY, nonce)
      const state = `${nonce}~${purpose}`
      const response = await fetch(`/api/v1/auth/login?state=${encodeURIComponent(state)}`)
      const data = await response.json()
      window.location.href = data.login_url
    } catch (err) {
      console.error('Login failed:', err)
    }
  }

  const handleCallbackToken = (token: string) => {
    setToken(token)
    useAuthStore.getState().fetchCurrentUser().then(() => {
      navigate('/dashboard')
    })
  }

  const handleLogout = () => {
    logout()
    navigate('/')
  }

  return {
    isAuthenticated: !!token && !!user,
    user,
    token,
    login,
    logout: handleLogout,
    handleCallbackToken,
  }
}
