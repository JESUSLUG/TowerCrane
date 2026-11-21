package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v56/github"
	"golang.org/x/oauth2"
)

type GitHubClient struct {
	client *github.Client
	org    string
}

func NewGitHubClient(ctx context.Context, token *oauth2.Token) (*GitHubClient, error) {
	if token == nil {
		return nil, fmt.Errorf("token is required")
	}

	tokenSource := oauth2.StaticTokenSource(token)
	httpClient := oauth2.NewClient(ctx, tokenSource)
	client := github.NewClient(httpClient)

	return &GitHubClient{
		client: client,
		org:    os.Getenv("GITHUB_ORG"),
	}, nil
}

func NewGitHubClientFromToken(ctx context.Context, tokenString string) *github.Client {
	token := &oauth2.Token{
		AccessToken: tokenString,
		TokenType:   "Bearer",
	}
	tokenSource := oauth2.StaticTokenSource(token)
	httpClient := oauth2.NewClient(ctx, tokenSource)
	return github.NewClient(httpClient)
}

type RepositoryInfo struct {
	ID              int64
	Name            string
	FullName        string
	URL             string
	Owner           string
	Description     string
	Private         bool
	DefaultBranch   string
}

type BranchInfo struct {
	Name   string
	Commit string
}

type FileInfo struct {
	Path    string
	Name    string
	Type    string
	Commit  string
	Content string
	Size    int
}

func (g *GitHubClient) ListRepositories(ctx context.Context) ([]*RepositoryInfo, error) {
	var repos []*RepositoryInfo
	opts := &github.RepositoryListOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		userRepos, resp, err := g.client.Repositories.List(ctx, "", opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list repositories: %w", err)
		}

		for _, repo := range userRepos {
			repos = append(repos, &RepositoryInfo{
				ID:            repo.GetID(),
				Name:          repo.GetName(),
				FullName:      repo.GetFullName(),
				URL:           repo.GetHTMLURL(),
				Owner:         repo.GetOwner().GetLogin(),
				Description:   repo.GetDescription(),
				Private:       repo.GetPrivate(),
				DefaultBranch: repo.GetDefaultBranch(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return repos, nil
}

func (g *GitHubClient) GetRepository(ctx context.Context, owner, name string) (*RepositoryInfo, error) {
	repo, _, err := g.client.Repositories.Get(ctx, owner, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository: %w", err)
	}

	return &RepositoryInfo{
		ID:            repo.GetID(),
		Name:          repo.GetName(),
		FullName:      repo.GetFullName(),
		URL:           repo.GetHTMLURL(),
		Owner:         repo.GetOwner().GetLogin(),
		Description:   repo.GetDescription(),
		Private:       repo.GetPrivate(),
		DefaultBranch: repo.GetDefaultBranch(),
	}, nil
}

// GetAllTerraformFiles fetches the full content of every .tf and .hcl file
// (Terragrunt) and every YAML file (TowerCrane manifests live where the
// project says; the caller picks them) in the repository at the given ref using the Git Trees API (one recursive
// request instead of walking directories one by one), returning a map of
// repository-relative path -> raw file content.
func (g *GitHubClient) GetAllTerraformFiles(ctx context.Context, owner, name, ref string) (map[string]string, error) {
	tree, _, err := g.client.Git.GetTree(ctx, owner, name, ref, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository tree: %w", err)
	}

	files := make(map[string]string)
	for _, entry := range tree.Entries {
		p := entry.GetPath()
		isManifest := strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")
		if entry.GetType() != "blob" || !(strings.HasSuffix(p, ".tf") || strings.HasSuffix(p, ".hcl") || isManifest) {
			continue
		}

		blob, _, err := g.client.Git.GetBlobRaw(ctx, owner, name, entry.GetSHA())
		if err != nil {
			return nil, fmt.Errorf("failed to get blob for %s: %w", entry.GetPath(), err)
		}
		files[entry.GetPath()] = string(blob)
	}

	return files, nil
}

func (g *GitHubClient) ListBranches(ctx context.Context, owner, name string) ([]*BranchInfo, error) {
	var branches []*BranchInfo
	opts := &github.BranchListOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		branchList, resp, err := g.client.Repositories.ListBranches(ctx, owner, name, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list branches: %w", err)
		}

		for _, branch := range branchList {
			branches = append(branches, &BranchInfo{
				Name:   branch.GetName(),
				Commit: branch.GetCommit().GetSHA(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return branches, nil
}

func (g *GitHubClient) ListFiles(ctx context.Context, owner, name, branch, path string) ([]*FileInfo, error) {
	if path == "" {
		path = "."
	}

	_, dirContents, _, err := g.client.Repositories.GetContents(ctx, owner, name, path, &github.RepositoryContentGetOptions{
		Ref: branch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get contents: %w", err)
	}

	var files []*FileInfo
	for _, content := range dirContents {
		fileType := "dir"
		if content.GetType() == "file" {
			fileType = "file"
		}

		files = append(files, &FileInfo{
			Path:   content.GetPath(),
			Name:   content.GetName(),
			Type:   fileType,
			Commit: content.GetSHA(),
			Size:   content.GetSize(),
		})
	}

	return files, nil
}

func (g *GitHubClient) GetFileContent(ctx context.Context, owner, name, branch, path string) (string, error) {
	content, _, _, err := g.client.Repositories.GetContents(ctx, owner, name, path, &github.RepositoryContentGetOptions{
		Ref: branch,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get file content: %w", err)
	}

	return content.GetContent()
}

// WrapClient adapts an already-authenticated go-github client (e.g. a GitHub
// App installation client) to GitHubClient.
func WrapClient(c *github.Client) *GitHubClient {
	return &GitHubClient{client: c, org: os.Getenv("GITHUB_ORG")}
}

// Raw exposes the underlying go-github client.
func (g *GitHubClient) Raw() *github.Client { return g.client }
