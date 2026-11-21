package api

import (
	"context"
	"database/sql"

	"github.com/gin-gonic/gin"
	"github.com/JESUSLUG/TowerCrane/backend/internal/auth"
	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/githubapp"
)

func SetupRoutes(router *gin.Engine, database *sql.DB, app *githubapp.App) {
	userStore := db.NewUserStore(database)
	repoStore := db.NewRepositoryStore(database)
	operationStore := db.NewOperationStore(database)
	integrationStore := db.NewIntegrationStore(database)

	gh := NewGitHubAccess(app, userStore)

	authHandler := NewAuthHandler(userStore, integrationStore, gh)
	onboardingHandler := NewOnboardingHandler(userStore, integrationStore)
	repoHandler := NewRepositoryHandler(repoStore, userStore, integrationStore, gh)
	operationHandler := NewOperationHandler(operationStore, repoStore, userStore, integrationStore, gh)
	prChecker := NewPRChecker(operationHandler, repoStore, integrationStore, gh)
	rolloutStore := db.NewRolloutStore(database)
	rollouts := NewRolloutEngine(operationHandler, rolloutStore, repoStore)
	rolloutHandler := NewRolloutHandler(rolloutStore, rollouts)
	adminHandler := NewAdminHandler(userStore)
	integrationsHandler := NewIntegrationsHandler(integrationStore, repoStore, gh)
	// What TowerCrane last read of every repository (see snapshots.go).
	snapshots := NewSnapshotStore()
	projectStore := db.NewProjectStore(database, repoStore)
	mapHandler := NewMapHandler(repoStore, userStore, operationStore, integrationStore, gh, snapshots, projectStore)
	operationHandler.projects = projectStore
	repoHandler.projects = projectStore
	// Projects come from Project manifests (kind: Project).
	projectSync := NewProjectSync(projectStore, repoStore, snapshots, gh)
	snapshots.onNew = projectSync.Sync
	repoHandler.onConnected = projectSync.Connected
	projectHandler := NewProjectHandler(projectStore, repoStore, integrationStore)
	webhookHandler := NewWebhookHandler(repoStore, userStore, integrationStore, operationHandler, rollouts, prChecker, gh, snapshots, projectStore)

	// Polling makes webhooks optional; with both, webhooks react instantly
	// and polling catches anything a lost delivery missed.
	poller := NewPoller(PollInterval(), repoStore, integrationStore, gh, webhookHandler, prChecker)
	go poller.Run(context.Background())

	// Jobs: scheduled apply/destroy of ephemeral Applications.
	jobRunner := NewJobRunner(operationHandler, db.NewJobStore(database), repoStore, snapshots, gh)
	jobRunner.Start(context.Background())

	api := router.Group("/api/v1")
	{
		// Health check
		api.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":  "ok",
				"version": "0.1.0",
			})
		})

		// Auth routes (public)
		authRoutes := api.Group("/auth")
		{
			authRoutes.GET("/login", authHandler.LoginWithGitHub)
			authRoutes.GET("/callback", authHandler.GitHubCallback)
			authRoutes.POST("/login-local", authHandler.LoginLocal)
			authRoutes.POST("/logout", authHandler.Logout)
		}

		// First-run setup and invitations (public; protected by the one-time
		// setup token / the invitation token)
		api.GET("/setup/status", onboardingHandler.SetupStatus)
		api.POST("/setup", onboardingHandler.Setup)
		api.GET("/invitations/:token", onboardingHandler.GetInvitation)
		api.POST("/invitations/:token/accept", onboardingHandler.AcceptInvitation)

		// GitHub webhooks (public; authenticated via HMAC signature instead
		// of a user session - see internal/api/webhook.go)
		api.POST("/webhooks/github", webhookHandler.HandleGitHubPush)

		// Protected routes. Roles (see internal/auth/roles.go):
		//   member   - view everything, run plans
		//   approver - member + confirm applies
		//   admin    - approver + connect repos, approve policy changes,
		//              destroy, manage users and team mappings
		protected := api.Group("")
		protected.Use(AuthMiddleware(), RoleMiddleware(userStore))
		{
			member := protected
			approver := protected.Group("", RequireRole(auth.RoleApprover))
			admin := protected.Group("", RequireRole(auth.RoleAdmin))

			// User routes
			member.GET("/user", authHandler.GetCurrentUser)

			// Repository routes
			member.GET("/repositories", repoHandler.ListConnectedRepositories)
			member.GET("/repositories/:repo_id/branches", repoHandler.GetRepositoryBranches)
			member.GET("/repositories/:repo_id/map", mapHandler.GetResourceMap)
			member.POST("/repositories/:repo_id/refresh", refreshHandler(repoStore, poller))
			// Projects: configuration repository + code repositories.
			member.GET("/projects", projectHandler.List)
			member.GET("/projects/:project_id", projectHandler.Get)
			admin.POST("/projects", projectHandler.Create)
			admin.PUT("/projects/:project_id", projectHandler.Update)
			admin.DELETE("/projects/:project_id", projectHandler.Delete)
			member.GET("/repositories/:repo_id/jobs", jobRunner.List)
			approver.POST("/repositories/:repo_id/jobs/:job/run", jobRunner.RunNow)
			admin.GET("/repositories/available", repoHandler.ListAvailableRepositories)
			admin.POST("/repositories/connect", repoHandler.ConnectRepository)
			admin.POST("/repositories/:repo_id/sync", repoHandler.SyncRepositoryData)

			// Operations routes
			member.POST("/repositories/:repo_id/operations/plan", operationHandler.ExecutePlan)
			approver.POST("/repositories/:repo_id/operations/apply", operationHandler.ExecuteApply)
			admin.POST("/repositories/:repo_id/operations/destroy", operationHandler.ExecuteDestroy)
			member.GET("/repositories/:repo_id/operations", operationHandler.ListOperations)
			member.GET("/repositories/:repo_id/protection", repoHandler.GetProtection)
			member.GET("/operations/:operation_id", operationHandler.GetOperation)
			approver.POST("/operations/:operation_id/confirm", operationHandler.ConfirmPlan)
			approver.POST("/operations/:operation_id/discard", operationHandler.DiscardPlan)

			// Rollouts: the Applications of a commit, in dependsOn order
			member.GET("/repositories/:repo_id/rollouts", rolloutHandler.List)
			admin.PUT("/rollouts/:rollout_id/order", rolloutHandler.Reorder)
			admin.POST("/rollouts/:rollout_id/items/:application/pause", rolloutHandler.Pause)
			admin.POST("/rollouts/:rollout_id/items/:application/resume", rolloutHandler.Resume)
			admin.POST("/rollouts/:rollout_id/items/:application/retry", rolloutHandler.Retry)


			// Admin: users and GitHub team -> role mappings
			admin.GET("/admin/users", adminHandler.ListUsers)
			admin.PUT("/admin/users/:user_id/role", adminHandler.SetUserRole)
			admin.GET("/admin/role-mappings", adminHandler.ListRoleMappings)
			admin.PUT("/admin/role-mappings", adminHandler.UpsertRoleMapping)
			admin.DELETE("/admin/role-mappings/:mapping_id", adminHandler.DeleteRoleMapping)
			admin.GET("/admin/invitations", onboardingHandler.ListInvitations)
			admin.POST("/admin/invitations", onboardingHandler.CreateInvitation)
			admin.DELETE("/admin/invitations/:invitation_id", onboardingHandler.RevokeInvitation)

			// Integrations: GitHub App installations and cloud accounts
			admin.GET("/integrations/github", integrationsHandler.GetGitHub)
			admin.POST("/integrations/github/installations", integrationsHandler.AddInstallation)
			admin.DELETE("/integrations/github/installations/:installation_id", integrationsHandler.RemoveInstallation)
			member.GET("/cloud-accounts", integrationsHandler.ListCloudAccounts)
			admin.POST("/cloud-accounts", integrationsHandler.CreateCloudAccount)
			admin.POST("/cloud-accounts/:account_id/verify", integrationsHandler.VerifyCloudAccount)
			admin.DELETE("/cloud-accounts/:account_id", integrationsHandler.DeleteCloudAccount)
			admin.PUT("/repositories/:repo_id/cloud-accounts", integrationsHandler.SetRepositoryCloudAccounts)

		}
	}
}

// refreshHandler checks a repository for new commits right away;
// ?force=true plans the current commit again even if it was handled.
func refreshHandler(repoStore *db.RepositoryStore, poller *Poller) gin.HandlerFunc {
	return func(c *gin.Context) {
		repo, err := repoStore.GetRepositoryByID(c.Param("repo_id"))
		if err != nil {
			c.JSON(404, gin.H{"error": "repository not found"})
			return
		}
		res, err := poller.Refresh(c.Request.Context(), repo, c.Query("force") == "true")
		if err != nil {
			c.JSON(502, gin.H{"error": "failed to check GitHub: " + err.Error()})
			return
		}
		c.JSON(200, res)
	}
}
