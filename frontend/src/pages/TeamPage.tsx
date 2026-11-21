import { FormEvent, useEffect, useState } from 'react'
import { Navigate } from 'react-router-dom'
import { Copy, KeyRound, Mail, Plus, Shield, Trash2, Users } from 'lucide-react'
import { Layout } from '../components/Layout'
import { GithubIcon } from '../components/ui/GithubIcon'
import { Card, CardBody, CardHeader, CardTitle } from '../components/ui/Card'
import { Button } from '../components/ui/Button'
import { Badge, type Tone } from '../components/ui/Badge'
import { Alert } from '../components/ui/Alert'
import { Spinner } from '../components/ui/Spinner'
import { EmptyState } from '../components/ui/EmptyState'
import { PageHeader } from '../components/ui/PageHeader'
import { Avatar } from '../components/ui/Avatar'
import { Input, Select } from '../components/ui/Field'
import { useAuth } from '../hooks/useAuth'
import { adminAPI, apiError, invitationAPI, type Invitation, type RoleMapping, type TeamUser } from '../api/admin'
import { atLeast, roleDescriptions, roleLabels, type Role } from '../lib/roles'

const roles: Role[] = ['member', 'approver', 'admin']

const roleTone: Record<Role, Tone> = {
  member: 'neutral',
  approver: 'primary',
  admin: 'solid',
}

export function TeamPage() {
  const { user } = useAuth()
  const [users, setUsers] = useState<TeamUser[]>([])
  const [mappings, setMappings] = useState<RoleMapping[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [savingUser, setSavingUser] = useState<string | null>(null)

  const [team, setTeam] = useState('')
  const [teamRole, setTeamRole] = useState<Role>('approver')
  const [savingMapping, setSavingMapping] = useState(false)

  const [invitations, setInvitations] = useState<Invitation[]>([])
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState<Role>('member')
  const [inviting, setInviting] = useState(false)
  const [inviteLink, setInviteLink] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    Promise.all([adminAPI.listUsers(), adminAPI.listRoleMappings(), invitationAPI.list()])
      .then(([u, m, i]) => {
        setUsers(u)
        setMappings(m)
        setInvitations(i)
      })
      .catch((err) => setError(apiError(err, 'Failed to load the team')))
      .finally(() => setLoading(false))
  }, [])

  if (user && !atLeast(user.role, 'admin')) {
    return <Navigate to="/dashboard" replace />
  }

  const changeRole = async (target: TeamUser, role: Role | '') => {
    setSavingUser(target.id)
    setError(null)
    try {
      const updated = await adminAPI.setUserRole(target.id, role)
      setUsers((list) => list.map((u) => (u.id === updated.id ? { ...u, ...updated } : u)))
    } catch (err) {
      setError(apiError(err, 'Failed to update the role'))
    } finally {
      setSavingUser(null)
    }
  }

  const addMapping = async (e: FormEvent) => {
    e.preventDefault()
    setSavingMapping(true)
    setError(null)
    try {
      const m = await adminAPI.upsertRoleMapping(team, teamRole)
      setMappings((list) =>
        [...list.filter((x) => x.id !== m.id), m].sort((a, b) =>
          `${a.github_org}/${a.team_slug}`.localeCompare(`${b.github_org}/${b.team_slug}`)
        )
      )
      setTeam('')
    } catch (err) {
      setError(apiError(err, 'Failed to save the team mapping'))
    } finally {
      setSavingMapping(false)
    }
  }

  const invite = async (e: FormEvent) => {
    e.preventDefault()
    setInviting(true)
    setError(null)
    setCopied(false)
    try {
      const res = await invitationAPI.create(inviteEmail, inviteRole)
      setInviteLink(res.invite_url)
      setInvitations((list) => [res.invitation, ...list])
      setInviteEmail('')
    } catch (err) {
      setError(apiError(err, 'Failed to create the invitation'))
    } finally {
      setInviting(false)
    }
  }

  const revoke = async (inv: Invitation) => {
    setError(null)
    try {
      await invitationAPI.revoke(inv.id)
      setInvitations((list) => list.filter((x) => x.id !== inv.id))
    } catch (err) {
      setError(apiError(err, 'Failed to revoke the invitation'))
    }
  }

  const copyLink = async () => {
    if (!inviteLink) return
    try {
      await navigator.clipboard.writeText(inviteLink)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  const pendingInvitations = invitations.filter((i) => !i.accepted_at && new Date(i.expires_at).getTime() > Date.now())

  const removeMapping = async (m: RoleMapping) => {
    setError(null)
    try {
      await adminAPI.deleteRoleMapping(m.id)
      setMappings((list) => list.filter((x) => x.id !== m.id))
    } catch (err) {
      setError(apiError(err, 'Failed to delete the team mapping'))
    }
  }

  const roleOptions = roles.map((r) => (
    <option key={r} value={r}>
      {roleLabels[r]}
    </option>
  ))

  return (
    <Layout>
      <PageHeader
        title="Team"
        description="A user's role is the highest one granted by TowerCrane, a GitHub team, or the server's TOWERCRANE_ADMINS list."
      />

      {error && <Alert className="mb-6">{error}</Alert>}

      <div className="mb-6 grid grid-cols-1 gap-3 md:grid-cols-3">
        {roles.map((r) => (
          <div key={r} className="rounded-xl border border-line bg-surface px-4 py-3">
            <Badge tone={roleTone[r]}>{roleLabels[r]}</Badge>
            <p className="mt-2 text-xs text-muted">{roleDescriptions[r]}</p>
          </div>
        ))}
      </div>

      {loading ? (
        <Spinner label="Loading team..." />
      ) : (
        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle title="Users" />
            </CardHeader>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-line bg-canvas text-left text-xs uppercase tracking-wide text-muted">
                    <th className="px-6 py-3 font-medium">User</th>
                    <th className="px-6 py-3 font-medium">Effective role</th>
                    <th className="px-6 py-3 font-medium">Granted by</th>
                    <th className="px-6 py-3 font-medium">TowerCrane role</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((u) => (
                    <tr key={u.id} className="border-b border-line last:border-0">
                      <td className="px-6 py-3">
                        <div className="flex items-center gap-2.5">
                          <Avatar src={u.avatar_url} name={u.name || u.github_login || u.email} className="h-7 w-7" />
                          <div className="min-w-0">
                            <p className="truncate font-medium text-ink">{u.name || u.github_login || u.email}</p>
                            <p className="truncate text-xs text-muted">{u.github_login ? `@${u.github_login}` : u.email}</p>
                          </div>
                        </div>
                      </td>
                      <td className="px-6 py-3">
                        <Badge tone={roleTone[u.role]}>{roleLabels[u.role]}</Badge>
                      </td>
                      <td className="px-6 py-3">
                        <div className="flex flex-wrap gap-1">
                          {u.role_sources.bootstrap && (
                            <Badge tone="neutral" title="Listed in TOWERCRANE_ADMINS on the server">
                              <KeyRound className="h-3 w-3" /> Server config
                            </Badge>
                          )}
                          {u.role_sources.github && (
                            <Badge tone="neutral" title="Granted by a GitHub team mapping">
                              <GithubIcon className="h-3 w-3" /> {roleLabels[u.role_sources.github]}
                            </Badge>
                          )}
                          {u.role_sources.local && (
                            <Badge tone="neutral" title="Assigned in TowerCrane">
                              <Shield className="h-3 w-3" /> {roleLabels[u.role_sources.local]}
                            </Badge>
                          )}
                          {!u.role_sources.bootstrap && !u.role_sources.github && !u.role_sources.local && (
                            <span className="text-xs text-muted">Default</span>
                          )}
                        </div>
                      </td>
                      <td className="px-6 py-3">
                        <Select
                          aria-label={`TowerCrane role of ${u.name || u.github_login || u.email}`}
                          className="w-36"
                          value={u.role_sources.local}
                          disabled={savingUser === u.id}
                          onChange={(e) => changeRole(u, e.target.value as Role | '')}
                        >
                          <option value="">None</option>
                          {roleOptions}
                        </Select>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle
                icon={<Mail className="h-5 w-5 text-primary" />}
                title="Invitations"
                description="People outside your connected GitHub organizations can only join with an invitation. Links are single-use and expire after 7 days."
              />
            </CardHeader>
            <CardBody className="space-y-4">
              <form onSubmit={invite} className="flex flex-col gap-2 sm:flex-row">
                <Input
                  type="email"
                  aria-label="Email to invite"
                  value={inviteEmail}
                  onChange={(e) => setInviteEmail(e.target.value)}
                  placeholder="name@company.com"
                  required
                  className="flex-1"
                />
                <Select aria-label="Role" className="sm:w-36" value={inviteRole} onChange={(e) => setInviteRole(e.target.value as Role)}>
                  {roleOptions}
                </Select>
                <Button type="submit" loading={inviting}>
                  {!inviting && <Plus className="h-4 w-4" />}
                  Create invite link
                </Button>
              </form>

              {inviteLink && (
                <Alert tone="success">
                  <p className="mb-2">Send this link to the invitee. It is shown only once.</p>
                  <div className="flex items-center gap-2">
                    <code className="min-w-0 flex-1 truncate rounded-md bg-surface px-2 py-1.5 font-mono text-xs text-ink">
                      {inviteLink}
                    </code>
                    <Button size="sm" variant="secondary" onClick={copyLink}>
                      <Copy className="h-4 w-4" /> {copied ? 'Copied' : 'Copy'}
                    </Button>
                  </div>
                </Alert>
              )}

              {pendingInvitations.length > 0 && (
                <ul className="divide-y divide-line rounded-lg border border-line">
                  {pendingInvitations.map((inv) => (
                    <li key={inv.id} className="flex items-center justify-between gap-3 px-4 py-2.5">
                      <div className="min-w-0">
                        <p className="truncate text-sm text-ink">{inv.email}</p>
                        <p className="text-xs text-muted">Expires {new Date(inv.expires_at).toLocaleDateString()}</p>
                      </div>
                      <div className="flex items-center gap-2">
                        <Badge tone={roleTone[inv.role]}>{roleLabels[inv.role]}</Badge>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => revoke(inv)}
                          aria-label={`Revoke invitation for ${inv.email}`}
                          title="Revoke invitation"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </CardBody>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle
                icon={<GithubIcon className="h-5 w-5" />}
                title="GitHub team mappings"
                description="Members of these teams get the role when they sign in with GitHub. Removing a mapping takes effect immediately."
              />
            </CardHeader>
            <CardBody className="space-y-4">
              <form onSubmit={addMapping} className="flex flex-col gap-2 sm:flex-row">
                <Input
                  aria-label="GitHub team"
                  value={team}
                  onChange={(e) => setTeam(e.target.value)}
                  placeholder="org/team-slug, e.g. acme/infra-admins"
                  mono
                  className="flex-1"
                />
                <Select aria-label="Role" className="sm:w-36" value={teamRole} onChange={(e) => setTeamRole(e.target.value as Role)}>
                  {roleOptions}
                </Select>
                <Button type="submit" loading={savingMapping} disabled={!team.trim()}>
                  {!savingMapping && <Plus className="h-4 w-4" />}
                  Add mapping
                </Button>
              </form>

              {mappings.length === 0 ? (
                <EmptyState
                  icon={Users}
                  title="No team mappings"
                  description="Map a GitHub team to a role so its members get it automatically. Tip: use the same team you list in CODEOWNERS."
                />
              ) : (
                <ul className="divide-y divide-line rounded-lg border border-line">
                  {mappings.map((m) => (
                    <li key={m.id} className="flex items-center justify-between gap-3 px-4 py-2.5">
                      <span className="font-mono text-sm text-ink">
                        @{m.github_org}/{m.team_slug}
                      </span>
                      <div className="flex items-center gap-2">
                        <Badge tone={roleTone[m.role]}>{roleLabels[m.role]}</Badge>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => removeMapping(m)}
                          aria-label={`Remove mapping @${m.github_org}/${m.team_slug}`}
                          title="Remove mapping"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </CardBody>
          </Card>
        </div>
      )}
    </Layout>
  )
}
