import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { OAUTH_STATE_KEY, useAuth } from '../hooks/useAuth'
import { AuthShell } from '../components/AuthShell'
import { Spinner } from '../components/ui/Spinner'

export function AuthCallback() {
  const { handleCallbackToken } = useAuth()
  const [refused, setRefused] = useState(false)

  useEffect(() => {
    // The token comes in the fragment (#token=…&state=…), which never
    // leaves the browser. Clear it from the address bar right away.
    const params = new URLSearchParams(window.location.hash.slice(1))
    window.history.replaceState(null, '', window.location.pathname)
    const token = params.get('token')
    if (!token) return
    let expected: string | null = null
    try {
      expected = sessionStorage.getItem(OAUTH_STATE_KEY)
      sessionStorage.removeItem(OAUTH_STATE_KEY)
    } catch {
      /* storage unavailable: refuse below */
    }
    // Only accept a sign-in this tab started.
    if (!expected || params.get('state') !== expected) {
      setRefused(true)
      return
    }
    handleCallbackToken(token)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (refused) {
    return (
      <AuthShell title="Sign-in not completed">
        <p className="text-sm text-muted">This sign-in wasn't started from this browser tab, so it was ignored.</p>
        <Link to="/" className="mt-4 inline-block text-sm font-medium text-primary">Back to sign in</Link>
      </AuthShell>
    )
  }

  return (
    <AuthShell title="Completing sign-in">
      <Spinner label="Redirecting..." />
    </AuthShell>
  )
}
