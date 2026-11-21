package api

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v56/github"
	"golang.org/x/oauth2"
	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/git"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
)

type RepositoryHandler struct {
	repoStore    *db.RepositoryStore
	userStore    *db.UserStore
	integrations *db.IntegrationStore
	gh           *GitHubAccess
	// projects gives a repository's manifests folder (protection check).
	projects *db.ProjectStore
	// onConnected runs after a repository is connected (ProjectSync).
	onConnected func(*models.Repository)
}

func NewRepositoryHandler(repoStore *db.RepositoryStore, userStore *db.UserStore, integrations *db.IntegrationStore, gh *GitHubAccess) *RepositoryHandler {
	return &RepositoryHandler{
		repoStore:    repoStore,
		userStore:    userStore,
		integrations: integrations,
		gh:           gh,
	}
}

// githubTokenForUser resolves the caller's stored GitHub OAuth token. Local
// (email/password) accounts never have one, which is reported as a clear
// error rather than silently calling GitHub with an invalid token.
func (h *RepositoryHandler) githubTokenForUser(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return "", false
	}

	token, err := h.userStore.GetGitHubToken(userID.(string))
	if err != nil || token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "no GitHub account connected to this user; sign in with GitHub to browse and connect repositories",
		})
		return "", false
	}

	return token, true
}

type ConnectRepoRequest struct {
	Owner string `json:"owner" binding:"required"`
	Name  string `json:"name" binding:"required"`
	// InstallationID connects the repository through the GitHub App
	// installation that can access it (recommended). 0 = use the admin's
	// own GitHub OAuth token (legacy).
	InstallationID int64 `json:"installation_id"`
}

type RepositoryResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	URL           string `json:"url"`
	Owner         string `json:"owner"`
	Description   string `json:"description"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	// InstallationID is the GitHub App installation the repository is (or
	// would be) accessed through; 0 for OAuth-connected repositories.
	InstallationID int64 `json:"installation_id,omitempty"`
	// Account is the installation's org/user login.
	Account string `json:"account,omitempty"`
	// CloudAccountIDs are the cloud accounts the repository's manifests may
	// use (by name).
	CloudAccountIDs []string `json:"cloud_account_ids"`
}

type BranchResponse struct {
	Name      string `json:"name"`
	Commit    string `json:"commit"`
	IsDefault bool   `json:"is_default"`
}

func (h *RepositoryHandler) ListAvailableRepositories(c *gin.Context) {
	installations, err := h.integrations.ListInstallations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list GitHub App installations"})
		return
	}
	if h.gh.app != nil {
		if len(installations) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No GitHub account is connected yet. Go to Integrations → GitHub and connect one."})
			return
		}
		h.listInstallationRepositories(c, installations)
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	githubLogin, _ := c.Get("github_login")

	token, ok := h.githubTokenForUser(c)
	if !ok {
		return
	}

	oauthToken := &oauth2.Token{
		AccessToken: token,
		TokenType:   "Bearer",
	}

	gitClient, err := git.NewGitHubClient(context.Background(), oauthToken)
	if err != nil {
		log.Printf("Failed to create GitHub client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create GitHub client"})
		return
	}

	userRepos, err := gitClient.ListRepositories(context.Background())
	if err != nil {
		log.Printf("Failed to list GitHub repos: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list repositories"})
		return
	}

	var results []RepositoryResponse
	for _, repo := range userRepos {
		results = append(results, RepositoryResponse{
			ID:            repo.FullName,
			Name:          repo.Name,
			FullName:      repo.FullName,
			URL:           repo.URL,
			Owner:         repo.Owner,
			Description:   repo.Description,
			Private:       repo.Private,
			DefaultBranch: repo.DefaultBranch,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"repositories": results,
		"user_id":      userID,
		"github_login": githubLogin,
	})
}

// listInstallationRepositories lists every repository the TowerCrane GitHub
// App can access, across all installations TowerCrane knows about.
func (h *RepositoryHandler) listInstallationRepositories(c *gin.Context, installations []db.Installation) {
	ctx := context.Background()
	results := []RepositoryResponse{}
	var failures []string
	for _, inst := range installations {
		repos, err := h.gh.app.ListRepos(ctx, inst.InstallationID)
		if err != nil {
			log.Printf("Failed to list repositories of installation %d (%s): %v", inst.InstallationID, inst.AccountLogin, err)
			failures = append(failures, inst.AccountLogin)
			continue
		}
		for _, r := range repos {
			results = append(results, RepositoryResponse{
				ID:             r.GetFullName(),
				Name:           r.GetName(),
				FullName:       r.GetFullName(),
				URL:            r.GetHTMLURL(),
				Owner:          r.GetOwner().GetLogin(),
				Description:    r.GetDescription(),
				Private:        r.GetPrivate(),
				DefaultBranch:  r.GetDefaultBranch(),
				InstallationID: inst.InstallationID,
				Account:        inst.AccountLogin,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"repositories": results, "source": "github_app", "failed_installations": failures})
}

func (h *RepositoryHandler) ConnectRepository(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req ConnectRepoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var gitClient *github.Client
	if req.InstallationID != 0 {
		if h.gh.app == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "the GitHub App is not configured on the server"})
			return
		}
		client, err := h.gh.app.Client(context.Background(), req.InstallationID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to authenticate as the GitHub App installation: " + err.Error()})
			return
		}
		gitClient = client
	} else {
		token, ok := h.githubTokenForUser(c)
		if !ok {
			return
		}
		gitClient = git.NewGitHubClientFromToken(context.Background(), token)
	}

	// Get repo info from GitHub (with an installation client this also
	// proves the installation can access the repository).
	repoInfo, _, err := gitClient.Repositories.Get(context.Background(), req.Owner, req.Name)
	if err != nil {
		log.Printf("Failed to get repo from GitHub: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch repository from GitHub"})
		return
	}

	// Create repository record
	repo := &models.Repository{
		Name:          repoInfo.GetName(),
		FullName:      repoInfo.GetFullName(),
		URL:           repoInfo.GetHTMLURL(),
		Owner:         repoInfo.GetOwner().GetLogin(),
		Description:   repoInfo.GetDescription(),
		Private:       repoInfo.GetPrivate(),
		GitHubID:      repoInfo.GetID(),
		DefaultBranch:  repoInfo.GetDefaultBranch(),
		ConnectedBy:    userID.(string),
		InstallationID: req.InstallationID,
	}

	savedRepo, err := h.repoStore.CreateRepository(repo)
	if err != nil {
		log.Printf("Failed to save repository: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save repository"})
		return
	}
	// A repository with a Project manifest becomes a project right away.
	if h.onConnected != nil {
		go h.onConnected(savedRepo)
	}

	c.JSON(http.StatusCreated, RepositoryResponse{
		ID:            savedRepo.ID,
		Name:          savedRepo.Name,
		FullName:      savedRepo.FullName,
		URL:           savedRepo.URL,
		Owner:         savedRepo.Owner,
		Description:   savedRepo.Description,
		Private:       savedRepo.Private,
		DefaultBranch: savedRepo.DefaultBranch,
	})
}

func (h *RepositoryHandler) ListConnectedRepositories(c *gin.Context) {
	repos, err := h.repoStore.ListRepositories()
	if err != nil {
		log.Printf("Failed to list repositories: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list repositories"})
		return
	}

	attached, err := h.integrations.RepositoryCloudAccountIDs()
	if err != nil {
		log.Printf("Failed to list repository cloud accounts: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list repositories"})
		return
	}

	results := []RepositoryResponse{}
	for _, repo := range repos {
		ids := attached[repo.ID]
		if ids == nil {
			ids = []string{}
		}
		results = append(results, RepositoryResponse{
			ID:             repo.ID,
			Name:           repo.Name,
			FullName:       repo.FullName,
			URL:            repo.URL,
			Owner:          repo.Owner,
			Description:    repo.Description,
			Private:        repo.Private,
			DefaultBranch:  repo.DefaultBranch,
			InstallationID:  repo.InstallationID,
			CloudAccountIDs: ids,
		})
	}

	c.JSON(http.StatusOK, gin.H{"repositories": results})
}

func (h *RepositoryHandler) GetRepositoryBranches(c *gin.Context) {
	repoID := c.Param("repo_id")

	branches, err := h.repoStore.ListBranches(repoID)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("Failed to list branches: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list branches"})
		return
	}

	var results []BranchResponse
	for _, branch := range branches {
		results = append(results, BranchResponse{
			Name:      branch.Name,
			Commit:    branch.Commit,
			IsDefault: branch.IsDefault,
		})
	}

	c.JSON(http.StatusOK, gin.H{"branches": results})
}

func (h *RepositoryHandler) SyncRepositoryData(c *gin.Context) {
	repoID := c.Param("repo_id")

	repo, err := h.repoStore.GetRepositoryByID(repoID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}

	// Sync branches and files
	gitClient, err := h.gh.Client(context.Background(), repo)
	if err != nil {
		log.Printf("Failed to create GitHub client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create GitHub client"})
		return
	}

	branches, err := gitClient.ListBranches(context.Background(), repo.Owner, repo.Name)
	if err != nil {
		log.Printf("Failed to sync branches: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync branches"})
		return
	}

	for _, branch := range branches {
		branchModel := &models.RepositoryBranch{
			RepositoryID: repo.ID,
			Name:         branch.Name,
			Commit:       branch.Commit,
			IsDefault:    branch.Name == repo.DefaultBranch,
		}
		h.repoStore.CreateBranch(branchModel)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Repository synced successfully",
		"branches": len(branches),
	})
}
