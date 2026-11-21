// Package githubapp authenticates TowerCrane as a GitHub App: it signs app
// JWTs with the app's private key and exchanges them for short-lived
// installation tokens, which are then used to read repositories, clone
// them and post commit statuses. No personal user token is involved.
//
// Configuration (environment):
//
//	GITHUB_APP_ID                 numeric app id
//	GITHUB_APP_SLUG               app slug, used to build the install URL
//	GITHUB_APP_PRIVATE_KEY        PEM private key (literal "\n" are accepted), or
//	GITHUB_APP_PRIVATE_KEY_PATH   path to the PEM file
//
// Required app permissions: Contents (read), Metadata (read), Commit
// statuses (read & write), Pull requests (read), Members (read, org).
// Subscribed events: Push, Pull request, Installation.
package githubapp

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v56/github"
	"golang.org/x/oauth2"
)

// App is a configured GitHub App.
type App struct {
	ID   int64
	Slug string
	key  *rsa.PrivateKey

	mu     sync.Mutex
	tokens map[int64]*github.InstallationToken
}

// FromEnv loads the app configuration. It returns (nil, nil) when the app is
// simply not configured, and an error when it is configured incorrectly.
func FromEnv() (*App, error) {
	idStr := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	pemData := os.Getenv("GITHUB_APP_PRIVATE_KEY")
	if path := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH"); pemData == "" && path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading GITHUB_APP_PRIVATE_KEY_PATH: %w", err)
		}
		pemData = string(b)
	}
	if idStr == "" && pemData == "" {
		return nil, nil
	}
	if idStr == "" || pemData == "" {
		return nil, fmt.Errorf("GITHUB_APP_ID and GITHUB_APP_PRIVATE_KEY(_PATH) must both be set")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(strings.ReplaceAll(pemData, `\n`, "\n")))
	if err != nil {
		return nil, fmt.Errorf("invalid GitHub App private key: %w", err)
	}

	return &App{
		ID:     id,
		Slug:   strings.TrimSpace(os.Getenv("GITHUB_APP_SLUG")),
		key:    key,
		tokens: map[int64]*github.InstallationToken{},
	}, nil
}

// InstallURL is where an admin installs the app on an org or account and
// picks which repositories it may access. GitHub sends them back to the
// app's "Setup URL" with ?installation_id=...&state=...
func (a *App) InstallURL(state string) string {
	if a.Slug == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/apps/%s/installations/new?state=%s", a.Slug, url.QueryEscape(state))
}

// appJWT signs a short-lived JWT identifying the app itself.
func (a *App) appJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)), // clock drift
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    strconv.FormatInt(a.ID, 10),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(a.key)
}

// appClient is authenticated as the app (only for /app endpoints).
func (a *App) appClient(ctx context.Context) (*github.Client, error) {
	token, err := a.appJWT()
	if err != nil {
		return nil, err
	}
	return github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))), nil
}

// GetInstallation verifies an installation exists for this app.
func (a *App) GetInstallation(ctx context.Context, installationID int64) (*github.Installation, error) {
	c, err := a.appClient(ctx)
	if err != nil {
		return nil, err
	}
	inst, _, err := c.Apps.GetInstallation(ctx, installationID)
	if err != nil {
		return nil, fmt.Errorf("installation %d not found for this GitHub App: %w", installationID, err)
	}
	return inst, nil
}

// Token returns a valid installation access token, reusing a cached one
// until five minutes before it expires.
func (a *App) Token(ctx context.Context, installationID int64) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if t, ok := a.tokens[installationID]; ok && time.Until(t.GetExpiresAt().Time) > 5*time.Minute {
		return t.GetToken(), nil
	}

	c, err := a.appClient(ctx)
	if err != nil {
		return "", err
	}
	t, _, err := c.Apps.CreateInstallationToken(ctx, installationID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create installation token: %w", err)
	}
	a.tokens[installationID] = t
	return t.GetToken(), nil
}

// Client returns a GitHub API client authenticated as an installation.
func (a *App) Client(ctx context.Context, installationID int64) (*github.Client, error) {
	token, err := a.Token(ctx, installationID)
	if err != nil {
		return nil, err
	}
	return github.NewClient(&http.Client{Transport: &oauth2.Transport{
		Source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
	}}), nil
}

// ListRepos lists every repository an installation can access.
func (a *App) ListRepos(ctx context.Context, installationID int64) ([]*github.Repository, error) {
	c, err := a.Client(ctx, installationID)
	if err != nil {
		return nil, err
	}
	opts := &github.ListOptions{PerPage: 100}
	var out []*github.Repository
	for {
		page, resp, err := c.Apps.ListRepos(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list installation repositories: %w", err)
		}
		out = append(out, page.Repositories...)
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// Forget drops a cached token (e.g. after the installation was removed).
func (a *App) Forget(installationID int64) {
	a.mu.Lock()
	delete(a.tokens, installationID)
	a.mu.Unlock()
}

// ListInstallations lists every account the app is installed on.
func (a *App) ListInstallations(ctx context.Context) ([]*github.Installation, error) {
	c, err := a.appClient(ctx)
	if err != nil {
		return nil, err
	}
	opts := &github.ListOptions{PerPage: 100}
	var out []*github.Installation
	for {
		page, resp, err := c.Apps.ListInstallations(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list app installations: %w", err)
		}
		out = append(out, page...)
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}
