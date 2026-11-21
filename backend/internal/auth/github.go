package auth

import (
	"strings"
	"context"
	"database/sql"
	"fmt"
	"os"

	ghapi "github.com/google/go-github/v56/github"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type GitHubOAuth struct {
	config *oauth2.Config
	org    string
}

func NewGitHubOAuth() *GitHubOAuth {
	return &GitHubOAuth{
		config: &oauth2.Config{
			ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
			ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("GITHUB_REDIRECT_URI"),
			Scopes: []string{
				"read:user",
				"user:email",
				"read:org",
				// "repo" is required to list and read the contents of private
				// repositories (needed to connect them and build the resource map).
				"repo",
			},
			Endpoint: github.Endpoint,
		},
		org: os.Getenv("GITHUB_ORG"),
	}
}

func (g *GitHubOAuth) GetAuthURL(state string) string {
	return g.config.AuthCodeURL(state)
}

// ExchangeCode exchanges the OAuth code for a GitHub access token and the
// authenticated user's profile. The access token is returned separately so
// callers can persist it (it must never be embedded in our own JWT, since
// that JWT is stored client-side).
func (g *GitHubOAuth) ExchangeCode(ctx context.Context, code string) (*User, string, error) {
	token, err := g.config.Exchange(ctx, code)
	if err != nil {
		return nil, "", fmt.Errorf("failed to exchange code: %w", err)
	}

	client := ghapi.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(token)))

	// Get authenticated user
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return nil, "", fmt.Errorf("failed to get user: %w", err)
	}

	// Who may sign in (connected orgs, invitations...) is decided by the
	// caller, which knows TowerCrane's installations and invitations.

	return &User{
		GitHubID:    sql.NullInt64{Int64: user.GetID(), Valid: true},
		GitHubLogin: sql.NullString{String: user.GetLogin(), Valid: true},
		Email:       user.GetEmail(),
		Name:        user.GetName(),
		AvatarURL:   sql.NullString{String: user.GetAvatarURL(), Valid: true},
	}, token.AccessToken, nil
}

func (g *GitHubOAuth) ValidateOrgMembership(ctx context.Context, token *oauth2.Token, org string) (bool, error) {
	if org == "" {
		org = g.org
	}

	client := ghapi.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(token)))
	isMember, _, err := client.Organizations.IsMember(ctx, org, "")
	if err != nil {
		return false, err
	}

	return isMember, nil
}

// UserTeams returns every GitHub team the token's user belongs to, as
// lower-case "org/team-slug" strings (needs the read:org scope).
func UserTeams(ctx context.Context, accessToken string) ([]string, error) {
	client := ghapi.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken})))
	opts := &ghapi.ListOptions{PerPage: 100}
	var out []string
	for {
		teams, resp, err := client.Teams.ListUserTeams(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list user teams: %w", err)
		}
		for _, t := range teams {
			out = append(out, strings.ToLower(t.GetOrganization().GetLogin()+"/"+t.GetSlug()))
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// VerifiedEmails lists the email addresses GitHub has verified for the user
// behind token (needs the user:email scope).
func VerifiedEmails(ctx context.Context, token string) ([]string, error) {
	client := ghapi.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})))
	var out []string
	opts := &ghapi.ListOptions{PerPage: 100}
	for {
		emails, resp, err := client.Users.ListEmails(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, e := range emails {
			if e.GetVerified() {
				out = append(out, e.GetEmail())
			}
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}
