import { BrowserRouter as Router, Routes, Route, Navigate, useParams } from 'react-router-dom'
import { useAuth } from './hooks/useAuth'
import { useAuthStore } from './store/authStore'
import { Spinner } from './components/ui/Spinner'
import { LoginPage } from './pages/LoginPage'
import { AuthCallback } from './pages/AuthCallback'
import { DashboardPage } from './pages/DashboardPage'
import { RepositoriesPage } from './pages/RepositoriesPage'
import { OperationsPage } from './pages/OperationsPage'
import { TeamPage } from './pages/TeamPage'
import { SetupPage } from './pages/SetupPage'
import { InvitePage } from './pages/InvitePage'
import { IntegrationsPage, GitHubSetupCallback } from './pages/IntegrationsPage'
import { ApplicationsPage } from './pages/ApplicationsPage'
import { ApplicationPage } from './pages/ApplicationPage'
import { GroupPage } from './pages/GroupPage'
import { ProjectsPage } from './pages/ProjectsPage'
import { NotFoundPage } from './pages/NotFoundPage'

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, token, user } = useAuth()
  const failed = useAuthStore((s) => s.error)
  // With a token but no user yet, the user is still loading: wait instead of
  // redirecting, or deep links (e.g. GitHub's redirect back with
  // ?installation_id=...) would lose their URL.
  if (token && !user && !failed) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-canvas">
        <Spinner label="Loading..." />
      </div>
    )
  }
  return isAuthenticated ? <>{children}</> : <Navigate to="/" replace />
}

function AppContent() {
  const { isAuthenticated } = useAuth()

  return (
    <Routes>
      <Route path="/auth/callback" element={<AuthCallback />} />
      <Route path="/setup" element={<SetupPage />} />
      <Route path="/invite/:token" element={<InvitePage />} />
      <Route path="/" element={isAuthenticated ? <Navigate to="/dashboard" replace /> : <LoginPage />} />
      <Route
        path="/dashboard"
        element={
          <ProtectedRoute>
            <DashboardPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/repositories"
        element={
          <ProtectedRoute>
            <RepositoriesPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/projects/:repo_id/operations"
        element={
          <ProtectedRoute>
            <OperationsPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/team"
        element={
          <ProtectedRoute>
            <TeamPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/integrations"
        element={
          <ProtectedRoute>
            <IntegrationsPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/integrations/github/callback"
        element={
          <ProtectedRoute>
            <GitHubSetupCallback />
          </ProtectedRoute>
        }
      />
      <Route
        path="/projects/:repo_id"
        element={
          <ProtectedRoute>
            <ApplicationsPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/projects/:repo_id/groups/:group_name"
        element={
          <ProtectedRoute>
            <GroupPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/projects/:repo_id/apps/:app_name"
        element={
          <ProtectedRoute>
            <ApplicationPage />
          </ProtectedRoute>
        }
      />
      <Route
        path="/projects"
        element={
          <ProtectedRoute>
            <ProjectsPage />
          </ProtectedRoute>
        }
      />
      {/* Before projects existed, a repository's pages lived under /repositories/:id. */}
      <Route path="/repositories/:repo_id/*" element={<RepositoryRedirect />} />
      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}

function App() {
  return (
    <Router>
      <AppContent />
    </Router>
  )
}

export default App

/** /repositories/:id/... (before projects) -> /projects/:id/... */
function RepositoryRedirect() {
  const { repo_id = '', '*': rest = '' } = useParams()
  return <Navigate to={`/projects/${repo_id}${rest ? `/${rest}` : ''}`} replace />
}
