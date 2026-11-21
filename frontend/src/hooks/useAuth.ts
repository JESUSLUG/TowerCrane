import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuthStore } from '../store/authStore'

export function useAuth() {
  const navigate = useNavigate()
  const { token, user, setToken, logout } = useAuthStore()

  useEffect(() => {
    // Check if user is authenticated and fetch user data
    if (token && !user) {
      useAuthStore.getState().fetchCurrentUser()
    }
  }, [token, user])

  // state is passed through GitHub's OAuth flow; invitation links use
  // "invite:<token>" so the backend can accept the invitation on sign-in.
  const login = async (state = 'web') => {
    try {
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
