package api

import (
	"context"
	"fmt"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/git"
	"github.com/JESUSLUG/TowerCrane/backend/internal/githubapp"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
	"github.com/google/go-github/v56/github"
)

// GitHubAccess resolves how TowerCrane talks to GitHub for a repository:
// through the GitHub App installation the repository was connected with,
// or, for repositories connected before the app existed, with the OAuth
// token of the user who connected it.
type GitHubAccess struct {
	app       *githubapp.App // nil when no GitHub App is configured
	userStore *db.UserStore
}

func NewGitHubAccess(app *githubapp.App, userStore *db.UserStore) *GitHubAccess {
	return &GitHubAccess{app: app, userStore: userStore}
}

// Token returns a token that can clone/read the repository.
func (g *GitHubAccess) Token(ctx context.Context, repo *models.Repository) (string, error) {
	if repo.InstallationID != 0 {
		if g.app == nil {
			return "", fmt.Errorf("repository %s is connected through a GitHub App, but GITHUB_APP_ID is not configured", repo.FullName)
		}
		return g.app.Token(ctx, repo.InstallationID)
	}
	token, err := g.userStore.GetGitHubToken(repo.ConnectedBy)
	if err != nil || token == "" {
		if repo.Private {
			return "", fmt.Errorf("no GitHub credentials for %s: connect it through the GitHub App", repo.FullName)
		}
		return "", nil // public repository: anonymous access
	}
	return token, nil
}

// Client returns a GitHub API client for the repository.
func (g *GitHubAccess) Client(ctx context.Context, repo *models.Repository) (*git.GitHubClient, error) {
	if repo.InstallationID != 0 && g.app != nil {
		c, err := g.app.Client(ctx, repo.InstallationID)
		if err != nil {
			return nil, err
		}
		return git.WrapClient(c), nil
	}
	token, err := g.Token(ctx, repo)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return git.WrapClient(github.NewClient(nil)), nil
	}
	return git.WrapClient(git.NewGitHubClientFromToken(ctx, token)), nil
}
