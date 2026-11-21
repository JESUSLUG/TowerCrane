import { HTMLAttributes } from 'react'
import { AlertTriangle, CheckCircle2, Info, ShieldAlert } from 'lucide-react'
import { cn } from '../../lib/utils'

type AlertTone = 'danger' | 'attention' | 'info' | 'success'

const tones: Record<AlertTone, { box: string; icon: typeof Info }> = {
  danger: { box: 'border-danger/30 bg-danger-soft text-danger-ink', icon: AlertTriangle },
  attention: { box: 'border-attention-line bg-attention-soft text-attention-ink', icon: ShieldAlert },
  info: { box: 'border-primary/20 bg-primary-soft text-primary-ink', icon: Info },
  success: { box: 'border-success/30 bg-success-soft text-success-ink', icon: CheckCircle2 },
}

interface AlertProps extends HTMLAttributes<HTMLDivElement> {
  tone?: AlertTone
}

export function Alert({ tone = 'danger', className, children, ...props }: AlertProps) {
  const { box, icon: Icon } = tones[tone]
  return (
    <div
      role={tone === 'danger' ? 'alert' : 'status'}
      className={cn('flex items-start gap-3 rounded-control border px-4 py-3 text-sm', box, className)}
      {...props}
    >
      <Icon className="mt-0.5 h-4 w-4 flex-shrink-0" aria-hidden="true" />
      <div className="min-w-0 break-words">{children}</div>
    </div>
  )
}
