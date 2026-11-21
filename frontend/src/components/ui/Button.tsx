import { AnchorHTMLAttributes, ButtonHTMLAttributes, forwardRef } from 'react'
import { Link } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { cn } from '../../lib/utils'

/**
 * primary   – the main action of a screen (purple)
 * attention – a person decides: "Yes, apply", "Approve" (yellow, dark text)
 * secondary – everything else
 * ghost     – low-emphasis, toolbars, icon buttons
 * danger    – irreversible: destroy, delete
 */
export type ButtonVariant = 'primary' | 'attention' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md' | 'lg'

const variantClasses: Record<ButtonVariant, string> = {
  primary: 'bg-primary text-white hover:bg-primary-hover',
  attention: 'bg-attention text-ink hover:bg-attention-hover',
  secondary: 'border border-line-strong bg-surface text-ink hover:bg-subtle',
  ghost: 'bg-transparent text-muted hover:bg-subtle hover:text-ink',
  danger: 'bg-danger text-white hover:bg-danger-hover',
}

const sizeClasses: Record<ButtonSize, string> = {
  sm: 'h-8 px-3 text-xs gap-1.5',
  md: 'h-10 px-4 text-sm gap-2',
  lg: 'h-11 px-5 text-base gap-2',
}

/** Class names of a button, for elements that must look like one. */
export function buttonClass(variant: ButtonVariant = 'primary', size: ButtonSize = 'md', className?: string) {
  return cn(
    'inline-flex items-center justify-center whitespace-nowrap rounded-control font-semibold transition-colors duration-150',
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 focus-visible:ring-offset-surface',
    'disabled:cursor-not-allowed disabled:opacity-50',
    variantClasses[variant],
    sizeClasses[size],
    className
  )
}

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  loading?: boolean
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ variant = 'primary', size = 'md', loading, disabled, className, children, type = 'button', ...props }, ref) => (
    <button
      ref={ref}
      type={type}
      disabled={disabled || loading}
      className={buttonClass(variant, size, className)}
      {...props}
    >
      {loading && <Loader2 className="h-4 w-4 animate-spin" />}
      {children}
    </button>
  )
)
Button.displayName = 'Button'

interface ButtonLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  /** Internal route. Use `href` instead for external links. */
  to?: string
  variant?: ButtonVariant
  size?: ButtonSize
}

/** A link styled as a button (never nest a <button> inside an <a>). */
export function ButtonLink({ to, href, variant = 'secondary', size = 'md', className, children, ...props }: ButtonLinkProps) {
  const cls = buttonClass(variant, size, className)
  if (to) {
    return (
      <Link to={to} className={cls} {...props}>
        {children}
      </Link>
    )
  }
  const external = href?.startsWith('http')
  return (
    <a href={href} className={cls} {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})} {...props}>
      {children}
    </a>
  )
}
