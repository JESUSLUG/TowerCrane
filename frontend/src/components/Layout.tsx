import { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { LayoutDashboard, GitBranch, LogOut, Boxes, Users, Plug, FolderGit2 } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { cn } from '../lib/utils'
import { atLeast, roleLabels } from '../lib/roles'
import { Avatar } from './ui/Avatar'

const navItems = [
  { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/projects', label: 'Projects', icon: FolderGit2 },
  { to: '/repositories', label: 'Repositories', icon: GitBranch },
  { to: '/team', label: 'Team', icon: Users, adminOnly: true },
  { to: '/integrations', label: 'Integrations', icon: Plug, adminOnly: true },
]

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'flex h-10 items-center gap-2.5 whitespace-nowrap rounded-lg px-3 text-sm transition-colors',
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary',
    isActive ? 'bg-primary-soft font-semibold text-primary-ink' : 'font-medium text-ink hover:bg-subtle'
  )

function Logo() {
  return (
    <Link to="/dashboard" className="flex items-center gap-2 px-1">
      <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary">
        <Boxes className="h-4 w-4 text-white" />
      </span>
      <span className="text-base font-bold tracking-tight text-ink">TowerCrane</span>
    </Link>
  )
}

/** App shell: a sidebar on desktop, a top bar with scrolling nav on small screens. */
export function Layout({ children }: { children: ReactNode }) {
  const { user, logout } = useAuth()
  const visibleNav = navItems.filter((item) => !item.adminOnly || atLeast(user?.role, 'admin'))
  const displayName = user ? user.name || user.github_login || user.email : ''

  return (
    <div className="min-h-screen bg-canvas text-ink">
      {/* Desktop sidebar */}
      <aside className="fixed inset-y-0 left-0 z-20 hidden w-60 flex-col border-r border-line bg-surface px-3 py-5 lg:flex">
        <Logo />
        <nav aria-label="Main" className="mt-8 flex flex-col gap-1">
          {visibleNav.map(({ to, label, icon: Icon }) => (
            <NavLink key={to} to={to} className={navLinkClass}>
              <Icon className="h-4 w-4" />
              {label}
            </NavLink>
          ))}
        </nav>
        {user && (
          <div className="mt-auto flex items-center gap-2.5 border-t border-line px-1 pt-4">
            <Avatar src={user.avatar_url} name={displayName} />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium text-ink">{displayName}</p>
              {user.role && <p className="text-xs text-muted">{roleLabels[user.role]}</p>}
            </div>
            <button
              type="button"
              onClick={logout}
              aria-label="Log out"
              title="Log out"
              className="rounded-lg p-2 text-muted transition hover:bg-danger-soft hover:text-danger"
            >
              <LogOut className="h-4 w-4" />
            </button>
          </div>
        )}
      </aside>

      {/* Mobile top bar */}
      <header className="sticky top-0 z-20 border-b border-line bg-surface lg:hidden">
        <div className="flex items-center justify-between px-4 py-3">
          <Logo />
          <button
            type="button"
            onClick={logout}
            aria-label="Log out"
            className="rounded-lg p-2 text-muted transition hover:bg-danger-soft hover:text-danger"
          >
            <LogOut className="h-4 w-4" />
          </button>
        </div>
        <nav aria-label="Main" className="flex gap-1 overflow-x-auto px-3 pb-2">
          {visibleNav.map(({ to, label, icon: Icon }) => (
            <NavLink key={to} to={to} className={navLinkClass}>
              <Icon className="h-4 w-4" />
              {label}
            </NavLink>
          ))}
        </nav>
      </header>

      <main className="lg:pl-60">
        <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6 lg:px-10">{children}</div>
      </main>
    </div>
  )
}
