import { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'

interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  /** Badges or metadata shown next to / under the title. */
  meta?: ReactNode
  actions?: ReactNode
  back?: { to: string; label: string }
  mono?: boolean
}

/** The top of every page: optional back link, title, description and actions. */
export function PageHeader({ title, description, meta, actions, back, mono }: PageHeaderProps) {
  return (
    <div className="mb-6">
      {back && (
        <Link
          to={back.to}
          className="mb-3 inline-flex items-center gap-1.5 text-sm font-medium text-muted transition hover:text-primary"
        >
          <ArrowLeft className="h-4 w-4" />
          {back.label}
        </Link>
      )}
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0 space-y-1.5">
          <h1 className={mono ? 'break-all font-mono text-2xl font-bold text-ink' : 'text-2xl font-bold tracking-tight text-ink'}>
            {title}
          </h1>
          {meta && <div className="flex flex-wrap items-center gap-2 text-sm">{meta}</div>}
          {description && <p className="max-w-3xl text-sm text-muted">{description}</p>}
        </div>
        {actions && <div className="flex flex-shrink-0 flex-wrap items-center gap-2">{actions}</div>}
      </div>
    </div>
  )
}
