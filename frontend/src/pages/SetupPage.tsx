import { FormEvent, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import axios from 'axios'
import { AuthShell } from '../components/AuthShell'
import { Field, Input } from '../components/ui/Field'
import { Button } from '../components/ui/Button'
import { Alert } from '../components/ui/Alert'
import { useAuthStore } from '../store/authStore'
import { apiError } from '../api/admin'

/** First-run screen: creates the initial admin with the setup token from the backend logs. */
export function SetupPage() {
  const navigate = useNavigate()
  const { startSession } = useAuthStore()
  const [setupToken, setSetupToken] = useState('')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    axios
      .get('/api/v1/setup/status')
      .then((r) => {
        if (!r.data.needs_setup) navigate('/', { replace: true })
      })
      .catch(() => undefined)
  }, [navigate])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (password !== confirm) {
      setError('Passwords do not match')
      return
    }
    setSaving(true)
    setError(null)
    try {
      const res = await axios.post('/api/v1/setup', { setup_token: setupToken.trim(), name, email, password })
      await startSession(res.data.token)
      navigate('/integrations')
    } catch (err) {
      setError(apiError(err, 'Setup failed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <AuthShell title="Welcome to TowerCrane" subtitle="Create the first admin account">
      <form onSubmit={submit} className="space-y-4">
        {error && <Alert>{error}</Alert>}
        <Field label="Setup token" hint="Printed in the backend logs the first time TowerCrane starts.">
          <Input mono value={setupToken} onChange={(e) => setSetupToken(e.target.value)} required autoFocus />
        </Field>
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" />
        </Field>
        <Field label="Email">
          <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" required />
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
        <Field label="Confirm password">
          <Input
            type="password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
            required
            minLength={12}
          />
        </Field>
        <Button type="submit" size="lg" className="w-full" loading={saving}>
          Create admin account
        </Button>
      </form>
    </AuthShell>
  )
}
