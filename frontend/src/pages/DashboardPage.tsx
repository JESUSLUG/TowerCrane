import { Link } from 'react-router-dom'
import { GitBranch, Boxes, Settings2, ArrowRight } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { Layout } from '../components/Layout'
import { Card, CardBody, CardHeader, CardTitle } from '../components/ui/Card'
import { PageHeader } from '../components/ui/PageHeader'
import { cn } from '../lib/utils'

const quickLinks = [
  {
    title: 'Projects',
    description: 'Groups, services, Jobs and rollouts of your infrastructure',
    icon: Boxes,
    cta: 'Open',
    to: '/projects',
  },
  {
    title: 'Repositories',
    description: 'The Git repositories projects read from',
    icon: GitBranch,
    cta: 'Manage',
    to: '/repositories',
  },
  {
    title: 'Operations',
    description: 'Review and execute operations',
    icon: Settings2,
    cta: 'Coming soon',
    to: null,
  },
]

export function DashboardPage() {
  const { user } = useAuth()

  return (
    <Layout>
      <PageHeader
        title={`Welcome back, ${user?.name || user?.github_login || ''}`}
        description="Manage your infrastructure with GitOps"
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        {quickLinks.map((link) => {
          const Icon = link.icon
          const body = (
            <CardBody>
              <div className="mb-4 inline-flex rounded-lg bg-primary-soft p-2.5 text-primary">
                <Icon className="h-5 w-5" />
              </div>
              <h2 className="text-base font-semibold text-ink">{link.title}</h2>
              <p className="mb-4 mt-1 text-sm text-muted">{link.description}</p>
              <span
                className={cn(
                  'flex items-center gap-1 text-sm font-semibold',
                  link.to ? 'text-primary' : 'text-muted'
                )}
              >
                {link.cta}
                {link.to && <ArrowRight className="h-3.5 w-3.5 transition group-hover:translate-x-0.5" />}
              </span>
            </CardBody>
          )
          return link.to ? (
            <Link
              key={link.title}
              to={link.to}
              className="group rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
            >
              <Card className="h-full transition group-hover:border-primary/40">{body}</Card>
            </Link>
          ) : (
            <Card key={link.title} className="opacity-70">
              {body}
            </Card>
          )
        })}
      </div>

      {user && (
        <Card className="mt-8">
          <CardHeader>
            <CardTitle title="Your account" />
          </CardHeader>
          <CardBody>
            <dl className="grid grid-cols-1 gap-4 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-muted">GitHub login</dt>
                <dd className="font-semibold text-ink">{user.github_login || '—'}</dd>
              </div>
              <div>
                <dt className="text-muted">Email</dt>
                <dd className="font-semibold text-ink">{user.email || '—'}</dd>
              </div>
              <div>
                <dt className="text-muted">Full name</dt>
                <dd className="font-semibold text-ink">{user.name || '—'}</dd>
              </div>
              <div>
                <dt className="text-muted">User ID</dt>
                <dd className="font-mono text-xs font-semibold text-ink">{user.id}</dd>
              </div>
            </dl>
          </CardBody>
        </Card>
      )}
    </Layout>
  )
}
