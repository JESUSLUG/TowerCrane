import { FormEvent, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import axios from 'axios'
import { AuthDivider, AuthShell } from '../components/AuthShell'
import { Field, Input } from '../components/ui/Field'
import { Button } from '../components/ui/Button'
import { Alert } from '../components/ui/Alert'
import { Badge } from '../components/ui/Badge'
import { Spinner } from '../components/ui/Spinner'
import { GithubIcon } from '../components/ui/GithubIcon'
import { useAuth } from '../hooks/useAuth'
import { useAuthStore } from '../store/authStore'
import { apiError } from '../api/admin'
import { roleLabels, type Role } from '../lib/roles'

interface InvitationInfo {
  email: string
  role: Role
  expires_at: string
}

/** Accept an invitation: with GitHub, or by creating an email/password account. */
export function InvitePage() {
  const { token = '' } = useParams<{ token: string }>()
  const navigate = useNavigate()
  const { login } = useAuth()
  const { startSession } = useAuthStore()
  const [info, setInfo] = useState<InvitationInfo | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    axios
      .get(`/api/v1/invitations/${token}`)
      .then((r) => setInfo(r.data))
      .catch((err) => setLoadError(apiError(err, 'This invitation is not valid')))
  }, [token])

  const accept = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError(null)
    try {
      const res = await axios.post(`/api/v1/invitations/${token}/accept`, { name, password })
      await startSession(res.data.token)
      navigate('/dashboard')
    } catch (err) {
      setError(apiError(err, 'Failed to accept the invitation'))
    } finally {
      setSaving(false)
    }
  }

  if (loadError) {
    return (
      <AuthShell title="Invitation unavailable">
        <Alert>{loadError}</Alert>
      </AuthShell>
    )
  }
  if (!info) {
    return (
      <AuthShell title="Loading invitation">
        <Spinner />
      </AuthShell>
    )
  }

  return (
    <AuthShell title="You're invited to TowerCrane">
      <div className="mb-6 flex flex-col items-center gap-2 text-sm">
        <span className="font-mono text-ink">{info.email}</span>
        <Badge tone="primary">{roleLabels[info.role]}</Badge>
      </div>

      <Button variant="primary" size="lg" className="w-full" onClick={() => login(`invite:${token}`)}>
        <GithubIcon className="h-5 w-5" />
        Accept with GitHub
      </Button>

      <AuthDivider label="or create a password" />

      <form onSubmit={accept} className="space-y-4">
        {error && <Alert>{error}</Alert>}
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" />
        </Field>
        <Field label="Password" hint="At least 12 characters.">
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required
            minLength={12}
          />
        </Field>
        <Button type="submit" variant="secondary" size="lg" className="w-full" loading={saving}>
          Create account
        </Button>
      </form>
    </AuthShell>
  )
}
