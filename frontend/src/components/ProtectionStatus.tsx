import { useEffect, useState } from 'react'
import { CheckCircle2, ChevronDown, CircleHelp, ShieldAlert, ShieldCheck, XCircle } from 'lucide-react'
import { protectionAPI, type ProtectionReport } from '../api/protection'
import { cn } from '../lib/utils'

/**
 * TowerCrane applies whatever is merged: who may change the manifests is
 * decided in GitHub. This shows whether that is locked down (branch
 * protection, pull requests, CODEOWNERS) and warns when it isn't.
 */
export function ProtectionStatus({ repoId }: { repoId: string }) {
  const [report, setReport] = useState<ProtectionReport | null>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    protectionAPI
      .get(repoId)
      .then((r) => {
        setReport(r)
        setOpen(r.level === 'warning')
      })
      .catch(() => setReport(null))
  }, [repoId])

  if (!report) return null
  const warning = report.level === 'warning'
  const Icon = warning ? ShieldAlert : report.level === 'ok' ? ShieldCheck : CircleHelp

  return (
    <div
      className={cn(
        'mb-6 rounded-xl border',
        warning ? 'border-attention-line bg-attention-soft/60' : 'border-line bg-surface'
      )}
    >
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="flex w-full items-center gap-3 px-4 py-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
      >
        <Icon className={cn('h-5 w-5 flex-shrink-0', warning ? 'text-attention-ink' : report.level === 'ok' ? 'text-success' : 'text-muted')} />
        <span className="min-w-0 flex-1 text-sm">
          <span className="font-semibold text-ink">
            {warning
              ? 'The TowerCrane policy of this repository is not protected in GitHub'
              : report.level === 'ok'
                ? `The manifests are protected on ${report.branch}`
                : 'TowerCrane could not check every GitHub protection'}
          </span>
          <span className="block text-xs text-muted">
            TowerCrane applies what is merged on {report.branch}: GitHub decides who may change the manifests.
          </span>
        </span>
        <ChevronDown className={cn('h-4 w-4 flex-shrink-0 text-muted transition-transform', open && 'rotate-180')} />
      </button>
      {open && (
        <ul className="space-y-2 border-t border-line px-4 py-3">
          {report.checks.map((c) => (
            <li key={c.id} className="flex items-start gap-2 text-sm">
              {c.state === 'ok' ? (
                <CheckCircle2 className="mt-0.5 h-4 w-4 flex-shrink-0 text-success" aria-label="OK" />
              ) : c.state === 'missing' ? (
                <XCircle className="mt-0.5 h-4 w-4 flex-shrink-0 text-danger" aria-label="Missing" />
              ) : (
                <CircleHelp className="mt-0.5 h-4 w-4 flex-shrink-0 text-muted" aria-label="Unknown" />
              )}
              <span>
                <span className="font-medium text-ink">{c.label}</span>
                <span className="block text-xs text-muted">{c.detail}</span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
