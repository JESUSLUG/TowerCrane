import { Compass, Home } from 'lucide-react'
import { ButtonLink } from '../components/ui/Button'

export function NotFoundPage() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 bg-canvas px-4 text-center">
      <div className="rounded-full bg-primary-soft p-4">
        <Compass className="h-10 w-10 text-primary" />
      </div>
      <h1 className="text-4xl font-bold text-ink">404</h1>
      <p className="max-w-sm text-muted">We couldn&apos;t find the page you&apos;re looking for.</p>
      <ButtonLink to="/dashboard" variant="primary">
        <Home className="h-4 w-4" />
        Back to Dashboard
      </ButtonLink>
    </div>
  )
}
