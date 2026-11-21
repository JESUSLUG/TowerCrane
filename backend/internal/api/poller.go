package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
	"github.com/google/go-github/v56/github"
)

// DefaultPollInterval is how often TowerCrane checks GitHub when
// TOWERCRANE_POLL_INTERVAL is not set.
const DefaultPollInterval = 3 * time.Minute

// PollInterval reads TOWERCRANE_POLL_INTERVAL (a Go duration such as "3m" or
// "90s"; "0" or "off" disables polling). Values below 30s are raised to 30s
// to stay well within GitHub's API rate limits.
func PollInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("TOWERCRANE_POLL_INTERVAL"))
	switch raw {
	case "":
		return DefaultPollInterval
	case "0", "off", "false":
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		log.Printf("invalid TOWERCRANE_POLL_INTERVAL %q, using %s", raw, DefaultPollInterval)
		return DefaultPollInterval
	}
	if d < 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// PRChecksEnabled reports whether TowerCrane evaluates open pull requests
// and reports the towercrane/policy status on them (TOWERCRANE_PR_CHECKS).
// Off by default: TowerCrane then only reacts to changes once they are
// merged into the default branch.
func PRChecksEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TOWERCRANE_PR_CHECKS"))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// Poller makes TowerCrane work without webhooks: it
// periodically asks GitHub for new default-branch commits and open pull
// requests, and handles them exactly like the matching webhook would.
// Webhooks remain useful to react instantly; the poller then only catches
// whatever a lost delivery missed. Both paths deduplicate, so nothing is
// processed twice.
type Poller struct {
	interval     time.Duration
	repoStore    *db.RepositoryStore
	integrations *db.IntegrationStore
	gh           *GitHubAccess
	hooks        *WebhookHandler
	prs          *PRChecker
}

func NewPoller(interval time.Duration, repoStore *db.RepositoryStore, integrations *db.IntegrationStore,
	gh *GitHubAccess, hooks *WebhookHandler, prs *PRChecker) *Poller {
	return &Poller{interval: interval, repoStore: repoStore, integrations: integrations, gh: gh, hooks: hooks, prs: prs}
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	if p.interval <= 0 {
		log.Printf("GitHub polling disabled (TOWERCRANE_POLL_INTERVAL)")
		return
	}
	log.Printf("Polling GitHub every %s for new commits and pull requests", p.interval)

	// Let the server finish starting before the first poll.
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		p.pollOnce(ctx)
		timer.Reset(p.interval)
	}
}

func (p *Poller) pollOnce(ctx context.Context) {
	repos, err := p.repoStore.ListRepositories()
	if err != nil {
		log.Printf("poller: failed to list repositories: %v", err)
		return
	}
	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		p.pollRepo(ctx, repo)
	}
}

func (p *Poller) pollRepo(ctx context.Context, repo *models.Repository) {
	client, err := p.gh.Client(ctx, repo)
	if err != nil {
		log.Printf("poller: %s: %v", repo.FullName, err)
		return
	}
	gh := client.Raw()

	branch := repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}

	// New commits on the default branch -> same as a push webhook.
	sha, err := p.headCommit(ctx, gh, repo, branch)
	if err != nil {
		log.Printf("poller: %s: %v", repo.FullName, err)
	} else {
		initialized, err := p.repoStore.InitLastSeen(repo.ID, sha)
		switch {
		case err != nil:
			log.Printf("poller: %s: %v", repo.FullName, err)
		case initialized:
			// First time we look at this repository: take the current
			// commit as the starting point instead of reacting to it.
			p.hooks.snapshots.readSnapshot(ctx, p.gh, repo, sha)
		default:
			if p.hooks.snapshots.Get(repo.ID) == nil {
				// First poll since TowerCrane started: read the commit (this
				// also syncs the repository's Project manifest).
				p.hooks.snapshots.readSnapshot(ctx, p.gh, repo, sha)
			}
			go p.hooks.onDefaultBranchCommit(repo, sha)
		}
	}

	// Open pull requests -> same as pull_request webhooks (opt-in).
	if PRChecksEnabled() {
		p.pollPullRequests(ctx, gh, repo, branch)
	}
}

func (p *Poller) pollPullRequests(ctx context.Context, gh *github.Client, repo *models.Repository, branch string) {
	tracked := map[int]db.PRCheck{}
	if checks, err := p.integrations.ListPRChecks(repo.ID); err == nil {
		for _, c := range checks {
			tracked[c.PRNumber] = c
		}
	}

	open := map[int]bool{}
	opts := &github.PullRequestListOptions{State: "open", Base: branch, ListOptions: github.ListOptions{PerPage: 100}}
	for {
		pulls, resp, err := gh.PullRequests.List(ctx, repo.Owner, repo.Name, opts)
		if err != nil {
			log.Printf("poller: %s: failed to list pull requests: %v", repo.FullName, err)
			return
		}
		for _, pr := range pulls {
			number, head := pr.GetNumber(), pr.GetHead().GetSHA()
			open[number] = true
			if c, ok := tracked[number]; ok && c.HeadSHA == head && c.State != "error" {
				continue // already evaluated this exact head
			}
			go p.prs.Check(repo, number, head)
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	// Stop tracking PRs that were closed, merged or retargeted.
	for number := range tracked {
		if !open[number] {
			p.prs.Forget(repo, number)
		}
	}
}

func (p *Poller) headCommit(ctx context.Context, gh *github.Client, repo *models.Repository, branch string) (string, error) {
	b, _, err := gh.Repositories.GetBranch(ctx, repo.Owner, repo.Name, branch, 1)
	if err != nil {
		return "", fmt.Errorf("failed to read branch %s: %w", branch, err)
	}
	sha := b.GetCommit().GetSHA()
	if sha == "" {
		return "", fmt.Errorf("branch %s has no commit", branch)
	}
	return sha, nil
}

// RefreshResult is what a manual refresh found.
type RefreshResult struct {
	CommitSHA string `json:"commit_sha"`
	// Processing is true when TowerCrane started planning the commit.
	Processing bool `json:"processing"`
	// Message explains the outcome for the UI.
	Message string `json:"message"`
}

// Refresh checks a repository right now instead of waiting for the next poll
// ("Refresh"). A commit TowerCrane hasn't handled yet is processed
// like a push; with force, the current commit is planned again even if it
// was already handled ("Hard refresh").
func (p *Poller) Refresh(ctx context.Context, repo *models.Repository, force bool) (*RefreshResult, error) {
	// A project checks its configuration repository and every source of
	// code; any other repository, just itself.
	repos := []*models.Repository{repo}
	if sources, err := p.hooks.projects.Sources(repo.ID); err == nil {
		repos = append(repos, sources...)
	}
	claimed := map[*models.Repository]string{}
	var head string
	for _, r := range repos {
		client, err := p.gh.Client(ctx, r)
		if err != nil {
			return nil, err
		}
		branch := r.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		sha, err := p.headCommit(ctx, client.Raw(), r, branch)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.FullName, err)
		}
		if r.ID == repo.ID {
			head = sha
		}
		ok, err := p.hooks.claimCommit(r, sha)
		if err != nil {
			return nil, err
		}
		if ok {
			claimed[r] = sha
		} else if _, err := p.hooks.snapshots.readSnapshot(ctx, p.gh, r, sha); err != nil {
			// Nothing new, but make sure the pages show this commit (e.g.
			// the first Refresh after TowerCrane started).
			return nil, err
		}
	}

	isProject, _ := p.hooks.projects.IsProject(repo.ID)
	switch {
	case len(claimed) > 0:
		go p.hooks.onClaimedCommits(claimed, "")
		names := make([]string, 0, len(claimed))
		for r := range claimed {
			names = append(names, r.FullName)
		}
		sort.Strings(names)
		return &RefreshResult{CommitSHA: head, Processing: true, Message: "New commits on " + strings.Join(names, ", ") + ": planning what they affect"}, nil
	case force && isProject:
		go p.hooks.onClaimedCommits(nil, repo.ID) // hard refresh: plan everything again
		return &RefreshResult{CommitSHA: head, Processing: true, Message: "Planning every Application of " + repo.FullName + " again"}, nil
	default:
		return &RefreshResult{CommitSHA: head, Message: "Already up to date"}, nil
	}
}
