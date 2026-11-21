import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Trash2 } from 'lucide-react'
import { projectAPI, type Project, type ProjectRepository } from '../api/projects'
import { apiError, integrationAPI, type CloudAccount } from '../api/admin'
import { listConnectedRepositories } from '../pages/ProjectsPage'
import { Card, CardBody, CardHeader, CardTitle } from './ui/Card'
import { Button } from './ui/Button'
import { Alert } from './ui/Alert'
import { Field, Input } from './ui/Field'
import { ChipPicker } from './ChipPicker'

/** Admins: rename a project, change its code repositories and cloud accounts, or delete it. */
export function ProjectSettings({ project, onSaved, onClose }: { project: Project; onSaved: (p: Project) => void; onClose: () => void }) {
  const navigate = useNavigate()
  const [repos, setRepos] = useState<ProjectRepository[]>([])
  const [accounts, setAccounts] = useState<CloudAccount[]>([])
  const [name, setName] = useState(project.name)
  const [sources, setSources] = useState(project.source_repositories.map((r) => r.id))
  const [cloud, setCloud] = useState(project.cloud_account_ids)
  const [manifestsDir, setManifestsDir] = useState(project.manifests_dir || '/')
  const [saving, setSaving] = useState(false)
  const managed = Boolean(project.manifest_file)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    listConnectedRepositories()
      .then(setRepos)
      .catch(() => undefined)
    integrationAPI
      .cloudAccounts()
      .then(setAccounts)
      .catch(() => undefined)
  }, [])

  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      onSaved(
        await projectAPI.update(
          project.id,
          managed
            ? { cloud_account_ids: cloud } // the rest comes from the Project manifest
            : { name, source_repository_ids: sources, cloud_account_ids: cloud, manifests_dir: manifestsDir },
        ),
      )
    } catch (err) {
      setError(apiError(err, 'Failed to save the project'))
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!confirm(`Delete project ${project.name}? Its repositories stay connected and nothing in the cloud is touched.`)) return
    try {
      await projectAPI.remove(project.id)
      navigate('/projects')
    } catch (err) {
      setError(apiError(err, 'Failed to delete the project'))
    }
  }

  return (
    <Card className="mb-6" highlight="primary">
      <CardHeader>
        <CardTitle
          title="Project settings"
          description={
            <>
              Manifests come from <code className="font-mono">{project.config_repository.full_name}</code>. Applications can only run code
              from the repositories and use the cloud accounts chosen here.
            </>
          }
        />
      </CardHeader>
      <CardBody className="space-y-5">
        {error && <Alert>{error}</Alert>}
        {managed && (
          <Alert tone="info">
            Defined by <code className="font-mono">{project.manifest_file}</code>: its name, folder and code repositories change with a pull
            request there. Here you authorize which cloud accounts it may use.
          </Alert>
        )}
        {!managed && (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label="Manifests folder" hint="/ = the whole repository is configuration">
              <Input mono value={manifestsDir} onChange={(e) => setManifestsDir(e.target.value)} />
            </Field>
          </div>
        )}
        {!managed && (
          <div>
            <p className="mb-2 text-sm font-medium text-ink">Code repositories</p>
            <ChipPicker
              options={repos.filter((r) => r.id !== project.id).map((r) => ({ id: r.id, label: r.full_name }))}
              selected={sources}
              onChange={setSources}
              empty="Connect more repositories in Repositories"
            />
          </div>
        )}
        <div>
          <p className="mb-2 text-sm font-medium text-ink">{managed ? 'Authorized cloud accounts' : 'Cloud accounts'}</p>
          <ChipPicker
            options={accounts.map((a) => ({ id: a.id, label: a.name }))}
            selected={cloud}
            onChange={setCloud}
            empty="Add cloud accounts in Integrations"
          />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2">
          {managed ? (
            <span className="text-xs text-muted">To remove the project, delete {project.manifest_file}.</span>
          ) : (
            <Button variant="ghost" className="text-danger hover:bg-danger-soft hover:text-danger" onClick={remove}>
              <Trash2 className="h-4 w-4" /> Delete project
            </Button>
          )}
          <div className="flex gap-2">
            <Button variant="ghost" onClick={onClose}>
              Close
            </Button>
            <Button variant="primary" onClick={save} loading={saving}>
              Save
            </Button>
          </div>
        </div>
      </CardBody>
    </Card>
  )
}
