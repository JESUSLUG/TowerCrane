import { ReactNode, useState } from 'react'
import { Check, PlayCircle, Rocket, Siren, ThumbsUp, Trash2, X } from 'lucide-react'
import { operationAPI } from '../api/operations'
import { apiError } from '../api/admin'
import type { Operation } from '../store/operationStore'
import { Card, CardBody, CardHeader, CardTitle } from './ui/Card'
import { Button } from './ui/Button'
import { Badge } from './ui/Badge'
import { Field, Input } from './ui/Field'
import { MetaLine } from './ui/MetaLine'
import { CodeBlock, PlanSummary } from './ui/CodeBlock'
import { InstanceName } from './InstanceName'
import { cn } from '../lib/utils'

/**
 * Plans waiting for an approver's "yes". Confirming applies exactly the plan
 * shown; discarding drops it.
 */
export function AwaitingYesCard({
  plans,
  canApply,
  isAdmin,
  onChanged,
  onError,
}: {
  plans: Operation[]
  canApply: boolean
  isAdmin: boolean
  onChanged: (created?: Operation) => void
  onError: (msg: string | null) => void
}) {
  const [busy, setBusy] = useState<string | null>(null)
  if (plans.length === 0) return null

  const act = async (op: Operation, kind: 'confirm' | 'discard') => {
    setBusy(op.id + kind)
    onError(null)
    try {
      if (kind === 'confirm') onChanged(await operationAPI.confirmPlan(op.id))
      else {
        await operationAPI.discardPlan(op.id)
        onChanged()
      }
    } catch (err) {
      onError(apiError(err, kind === 'confirm' ? 'Failed to confirm the plan' : 'Failed to discard the plan'))
    } finally {
      setBusy(null)
    }
  }

  return (
    <Card highlight="attention" className="mb-6">
      <CardHeader>
        <CardTitle
          icon={<ThumbsUp className="h-5 w-5 text-attention-ink" />}
          title="Waiting for your yes"
          description="Confirming applies exactly the plan shown (not a new one). It fails safely if the branch moved on since."
        />
      </CardHeader>
      <CardBody className="space-y-3">
        {plans.map((op) => {
          const allowed = op.emergency ? isAdmin : canApply
          return (
            <div key={op.id} className="space-y-3 rounded-lg border border-line p-4">
              <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <div className="min-w-0 space-y-1.5">
                  <div className="flex flex-wrap items-center gap-2">
                    <InstanceName name={op.application} className="font-mono text-sm font-semibold text-ink" />
                    {op.emergency && (
                      <Badge tone="danger">
                        <Siren className="h-3 w-3" /> Emergency
                      </Badge>
                    )}
                    <MetaLine items={[op.working_dir, op.commit_sha?.slice(0, 7)]} />
                  </div>
                  {op.policy_note && <p className="text-xs text-muted">{op.policy_note}</p>}
                </div>
                <PlanSummary plan={op.plan} />
              </div>
              {op.plan && (
                <details className="group">
                  <summary className="cursor-pointer text-sm font-medium text-primary hover:text-primary-hover">Show plan</summary>
                  <CodeBlock diff content={op.plan} className="mt-2 max-h-96" />
                </details>
              )}
              <div className="flex justify-end gap-2">
                {allowed ? (
                  <>
                    <Button size="sm" variant="secondary" disabled={busy !== null} onClick={() => act(op, 'discard')}>
                      <X className="h-4 w-4" /> Discard
                    </Button>
                    <Button size="sm" variant="attention" loading={busy === op.id + 'confirm'} disabled={busy !== null} onClick={() => act(op, 'confirm')}>
                      <Check className="h-4 w-4" /> Yes, apply
                    </Button>
                  </>
                ) : (
                  <Badge tone="attention">{op.emergency ? 'Needs an admin' : 'Needs an approver'}</Badge>
                )}
              </div>
            </div>
          )
        })}
      </CardBody>
    </Card>
  )
}

type OpKind = 'plan' | 'apply' | 'destroy'

/**
 * Plan / Apply / Destroy for one Application, plus the admins' emergency
 * mode (skips the policy; apply and destroy need the name typed again).
 */
export function RunOperationCard({
  repoId,
  appName,
  canApply,
  isAdmin,
  onStarted,
  onError,
  children,
}: {
  repoId: string
  /** Instance name ("ecr@sandbox"); "" disables the buttons. */
  appName: string
  canApply: boolean
  isAdmin: boolean
  onStarted: (op: Operation) => void
  onError: (msg: string | null) => void
  /** Extra fields above the buttons (e.g. an Application selector). */
  children?: ReactNode
}) {
  const [executing, setExecuting] = useState<OpKind | null>(null)
  const [emergency, setEmergency] = useState(false)
  const [emergencyConfirm, setEmergencyConfirm] = useState('')
  const emergencyReady = emergencyConfirm.trim() === appName

  const run = async (kind: OpKind) => {
    if (!appName) return
    if (kind === 'destroy' && !emergency && !confirm(`Destroy every resource of ${appName}? This cannot be undone.`)) {
      return
    }
    setExecuting(kind)
    onError(null)
    try {
      const request = {
        repository_id: repoId,
        application: appName,
        emergency,
        confirm: emergency ? emergencyConfirm.trim() : undefined,
      }
      const execute = kind === 'plan' ? operationAPI.executePlan : kind === 'apply' ? operationAPI.executeApply : operationAPI.executeDestroy
      onStarted(await execute(request))
      if (emergency) setEmergencyConfirm('')
    } catch (err) {
      onError(apiError(err, `Failed to execute ${kind}`))
    } finally {
      setExecuting(null)
    }
  }

  return (
    <Card highlight={emergency ? 'danger' : undefined} className="mb-6">
      <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <CardTitle title="Run an operation" />
        {isAdmin && (
          <label
            className={cn(
              'flex cursor-pointer items-center gap-2 rounded-control border px-3 py-1.5 text-sm font-semibold transition',
              emergency ? 'border-danger/50 bg-danger-soft text-danger-ink' : 'border-line-strong text-muted hover:text-ink'
            )}
          >
            <input
              type="checkbox"
              className="accent-danger"
              checked={emergency}
              onChange={(e) => {
                setEmergency(e.target.checked)
                setEmergencyConfirm('')
              }}
            />
            <Siren className="h-4 w-4" /> Emergency mode
          </label>
        )}
      </CardHeader>
      <CardBody className="space-y-5">
        {children}

        {emergency && (
          <div className="space-y-3 rounded-lg border border-danger/30 bg-danger-soft p-4 text-sm text-danger-ink">
            <p className="flex items-start gap-2">
              <Siren className="mt-0.5 h-4 w-4 flex-shrink-0" />
              Emergency mode skips the TowerCrane policy (editScope, pending manifest approvals, the “yes” step). It is only
              available to admins, only on declared Applications, and every operation is recorded as an emergency.
            </p>
            <Field label={`To apply or destroy, type ${appName}`}>
              <Input mono value={emergencyConfirm} onChange={(e) => setEmergencyConfirm(e.target.value)} />
            </Field>
          </div>
        )}

        <div className="flex flex-col gap-3 sm:flex-row">
          <Button
            onClick={() => run('plan')}
            disabled={executing !== null || !appName}
            loading={executing === 'plan'}
            variant="primary"
            className="sm:flex-1"
          >
            <PlayCircle className="h-4 w-4" />
            Plan
          </Button>
          <Button
            onClick={() => run('apply')}
            disabled={executing !== null || !appName || !canApply || (emergency && !emergencyReady)}
            title={canApply ? undefined : 'Only approvers and admins can apply'}
            loading={executing === 'apply'}
            variant={emergency ? 'danger' : 'secondary'}
            className="sm:flex-1"
          >
            <Rocket className="h-4 w-4" />
            {emergency ? 'Emergency apply' : 'Apply'}
          </Button>
          <Button
            onClick={() => run('destroy')}
            disabled={executing !== null || !appName || !isAdmin || (emergency && !emergencyReady)}
            title={isAdmin ? undefined : 'Only admins can destroy'}
            loading={executing === 'destroy'}
            variant={emergency ? 'danger' : 'ghost'}
            className={cn('sm:flex-1', !emergency && 'text-danger hover:bg-danger-soft hover:text-danger')}
          >
            <Trash2 className="h-4 w-4" />
            {emergency ? 'Emergency destroy' : 'Destroy'}
          </Button>
        </div>
      </CardBody>
    </Card>
  )
}
