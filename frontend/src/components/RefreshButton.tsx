import { useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { Button } from './ui/Button'
import { apiError, repoAPI } from '../api/admin'
import { cn } from '../lib/utils'

interface RefreshButtonProps {
  repoId: string
  /** Called after a refresh (new commit being processed, or the current one re-read) to reload the view. */
  onProcessing?: () => void
  size?: 'sm' | 'md'
}

/**
 * "Refresh": asks TowerCrane to check GitHub right now instead
 * of waiting for the next poll. When already up to date it offers to plan
 * the current commit again ("hard refresh").
 */
export function RefreshButton({ repoId, onProcessing, size = 'md' }: RefreshButtonProps) {
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [upToDate, setUpToDate] = useState(false)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    if (!message) return
    const t = setTimeout(() => setMessage(null), 8000)
    return () => clearTimeout(t)
  }, [message])

  const run = async (force: boolean) => {
    setBusy(true)
    setFailed(false)
    try {
      const res = await repoAPI.refresh(repoId, force)
      setMessage(`${res.message} (${res.commit_sha.slice(0, 7)})`)
      setUpToDate(!res.processing)
      onProcessing?.()
    } catch (err) {
      setFailed(true)
      setUpToDate(false)
      setMessage(apiError(err, 'Refresh failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button variant="secondary" size={size} onClick={() => run(false)} loading={busy} title="Check GitHub for new commits now">
        {!busy && <RefreshCw className="h-4 w-4" />}
        Refresh
      </Button>
      {message && (
        <span role="status" className={cn('text-xs', failed ? 'text-danger' : 'text-muted')}>
          {message}
          {upToDate && !busy && (
            <>
              {' · '}
              <button type="button" onClick={() => run(true)} className="font-semibold text-primary hover:text-primary-hover">
                Plan again
              </button>
            </>
          )}
        </span>
      )}
    </div>
  )
}
