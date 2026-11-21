import { useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { AuthShell } from '../components/AuthShell'
import { Spinner } from '../components/ui/Spinner'

export function AuthCallback() {
  const [searchParams] = useSearchParams()
  const { handleCallbackToken } = useAuth()

  useEffect(() => {
    const token = searchParams.get('token')
    if (token) {
      handleCallbackToken(token)
    }
  }, [searchParams, handleCallbackToken])

  return (
    <AuthShell title="Completing sign-in">
      <Spinner label="Redirecting..." />
    </AuthShell>
  )
}
