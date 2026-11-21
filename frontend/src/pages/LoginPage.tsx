import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Mail, ArrowLeft } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { useAuthStore } from '../store/authStore'
import { AuthDivider, AuthShell } from '../components/AuthShell'
import { Button } from '../components/ui/Button'
import { Alert } from '../components/ui/Alert'
import { Field, Input } from '../components/ui/Field'
import { GithubIcon } from '../components/ui/GithubIcon'

// Sign-in errors the backend sends back from GitHub (?error=<code>). Only
// these known messages are shown, never text from the URL itself.
const SIGN_IN_ERRORS: Record<string, string> = {
  github_failed: 'GitHub authentication failed. Please try again.',
  state_invalid: 'Sign-in expired, please try again.',
  invitation_invalid: 'This invitation is invalid, expired or was already used.',
  invitation_email: 'This invitation is for another email. Sign in with a GitHub account that has that email verified.',
  invitation_used: 'This invitation was already used.',
  not_member: 'Your GitHub account is not a member of an organization connected to TowerCrane. Ask an admin for an invitation.',
  account_failed: 'Failed to create your account.',
  signin_failed: 'Failed to sign you in.',
}

export function LoginPage() {
  const navigate = useNavigate()
  const { login } = useAuth()
  const { startSession } = useAuthStore()
  const [searchParams] = useSearchParams()
  const [showLocalLogin, setShowLocalLogin] = useState(false)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')
  const errorCode = searchParams.get('error')
  const githubError = errorCode ? SIGN_IN_ERRORS[errorCode] ?? 'Sign-in with GitHub failed.' : ''

  // First run: no accounts exist yet, so send the operator to the setup page.
  useEffect(() => {
    fetch('/api/v1/setup/status')
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (data?.needs_setup) navigate('/setup', { replace: true })
      })
      .catch(() => undefined)
  }, [navigate])

  const resetLocalForm = () => {
    setError('')
    setEmail('')
    setPassword('')
  }

  const handleLocalLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setIsLoading(true)

    try {
      const response = await fetch('/api/v1/auth/login-local', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ email, password }),
      })

      if (!response.ok) {
        setError('Invalid email or password')
        return
      }

      const data = await response.json()
      await startSession(data.token)
      navigate('/dashboard')
    } catch (err) {
      setError('Login failed. Please try again.')
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <AuthShell title="TowerCrane" subtitle="Infrastructure Management & GitOps">
      {!showLocalLogin ? (
        <>
          <div className="space-y-4">
            {githubError && <Alert>{githubError}</Alert>}
            <p className="text-center text-sm text-muted">
              Sign in with GitHub if you belong to an organization connected to TowerCrane, or with the email account an
              admin invited you with.
            </p>
            <Button onClick={() => login()} variant="primary" size="lg" className="w-full">
              <GithubIcon className="h-5 w-5" />
              Sign in with GitHub
            </Button>
          </div>

          <AuthDivider label="Or" />

          <Button
            variant="secondary"
            size="lg"
            className="w-full"
            onClick={() => {
              setShowLocalLogin(true)
              resetLocalForm()
            }}
          >
            <Mail className="h-4 w-4" />
            Sign in with Email
          </Button>
        </>
      ) : (
        <>
          <form onSubmit={handleLocalLogin} className="space-y-4">
            {error && <Alert>{error}</Alert>}
            <Field label="Email">
              <Input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="admin@example.com"
                autoComplete="email"
                required
                autoFocus
              />
            </Field>
            <Field label="Password">
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
              />
            </Field>
            <Button type="submit" size="lg" className="w-full" loading={isLoading}>
              {isLoading ? 'Signing in...' : 'Sign in'}
            </Button>
          </form>

          <p className="mt-6 text-center text-xs text-muted">No account? Ask a TowerCrane admin for an invitation.</p>

          <div className="mt-4 border-t border-line pt-4 text-center">
            <button
              type="button"
              onClick={() => {
                setShowLocalLogin(false)
                resetLocalForm()
              }}
              className="inline-flex items-center gap-1.5 text-sm font-medium text-muted transition hover:text-primary"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              Back to GitHub sign-in
            </button>
          </div>
        </>
      )}
    </AuthShell>
  )
}
