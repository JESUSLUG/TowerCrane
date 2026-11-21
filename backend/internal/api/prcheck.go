package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/git"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
	"github.com/JESUSLUG/TowerCrane/backend/internal/tfmap"
	"github.com/google/go-github/v56/github"
)

// PolicyStatusContext is the commit status TowerCrane reports on pull
// requests. Make it a required status check in the branch protection rules
// so a PR that changes `# towercrane` directives can't be merged until an
// admin approves the change in TowerCrane.
const PolicyStatusContext = "towercrane/policy"

// PRChecker evaluates the TowerCrane policy on pull requests before they
// merge and reports the outcome as a commit status.
type PRChecker struct {
	inflightMu sync.Mutex
	inflight   map[string]bool // "<repo id>#<number>@<sha>" being evaluated

	ops          *OperationHandler
	repoStore    *db.RepositoryStore
	integrations *db.IntegrationStore
	gh           *GitHubAccess
}

func NewPRChecker(ops *OperationHandler, repoStore *db.RepositoryStore, integrations *db.IntegrationStore, gh *GitHubAccess) *PRChecker {
	return &PRChecker{ops: ops, repoStore: repoStore, integrations: integrations, gh: gh, inflight: map[string]bool{}}
}

// prOutcome is the aggregated policy result for a pull request.
type prOutcome struct {
	state       string // success, pending, failure, error
	description string
}

// Check evaluates a pull request and posts the result as a commit status.
func (p *PRChecker) Check(repo *models.Repository, number int, headSHA string) {
	// Skip if the same PR head is already being evaluated (e.g. a webhook
	// and a poll arriving together).
	key := fmt.Sprintf("%s#%d@%s", repo.ID, number, headSHA)
	p.inflightMu.Lock()
	if p.inflight[key] {
		p.inflightMu.Unlock()
		return
	}
	p.inflight[key] = true
	p.inflightMu.Unlock()
	defer func() {
		p.inflightMu.Lock()
		delete(p.inflight, key)
		p.inflightMu.Unlock()
	}()

	ctx := context.Background()
	p.post(ctx, repo, headSHA, prOutcome{"pending", "Evaluating TowerCrane policy…"})

	sha, outcome := p.evaluate(ctx, repo, number, headSHA)
	if sha == "" {
		sha = headSHA
	}
	p.post(ctx, repo, sha, outcome)

	if err := p.integrations.UpsertPRCheck(&db.PRCheck{
		RepositoryID: repo.ID, PRNumber: number, HeadSHA: sha, State: outcome.state, Description: outcome.description,
	}); err != nil {
		log.Printf("pr-check: failed to store check for %s#%d: %v", repo.FullName, number, err)
	}
}

// RecheckRepository re-evaluates every open PR TowerCrane is tracking for a
// repository.
func (p *PRChecker) RecheckRepository(repoID string) {
	repo, err := p.repoStore.GetRepositoryByID(repoID)
	if err != nil {
		return
	}
	checks, err := p.integrations.ListPRChecks(repoID)
	if err != nil {
		log.Printf("pr-check: failed to list checks for %s: %v", repo.FullName, err)
		return
	}
	for _, c := range checks {
		p.Check(repo, c.PRNumber, c.HeadSHA)
	}
}

// Forget stops tracking a closed pull request.
func (p *PRChecker) Forget(repo *models.Repository, number int) {
	if err := p.integrations.DeletePRCheck(repo.ID, number); err != nil {
		log.Printf("pr-check: failed to forget %s#%d: %v", repo.FullName, number, err)
	}
}

func (p *PRChecker) evaluate(ctx context.Context, repo *models.Repository, number int, headSHA string) (string, prOutcome) {
	fail := func(err error) (string, prOutcome) {
		log.Printf("pr-check: %s#%d: %v", repo.FullName, number, err)
		return "", prOutcome{"error", "TowerCrane could not evaluate the policy; see the TowerCrane logs"}
	}

	unlock := git.LockWorkspace(repo.FullName)
	defer unlock()

	token, err := p.gh.Token(ctx, repo)
	if err != nil {
		return fail(err)
	}
	branch := repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	dir, err := git.EnsureWorkspace(repo.FullName, repo.URL, branch, token)
	if err != nil {
		return fail(err)
	}
	sha, err := git.FetchPullRequest(dir, token, number)
	if err != nil {
		return fail(err)
	}
	if headSHA != "" && sha != headSHA {
		// The PR moved on since the event; a newer event will evaluate the
		// newer head, so report on what we actually evaluated.
		log.Printf("pr-check: %s#%d head is %s, event said %s", repo.FullName, number, sha, headSHA)
	}
	base, err := git.MergeBase(dir, "origin/"+branch, sha)
	if err != nil {
		return fail(err)
	}

	diffs, err := git.DiffFiles(dir, base, sha)
	if err != nil {
		return fail(err)
	}
	changed := make([]string, len(diffs))
	for i, d := range diffs {
		changed[i] = d.Path
	}
	oldFiles, err := readRepoFiles(dir, base)
	if err != nil {
		return fail(err)
	}
	newFiles, err := readRepoFiles(dir, sha)
	if err != nil {
		return fail(err)
	}

	layout := p.ops.projects.Layout(repo.ID)
	before := manifest.LoadIn(oldFiles, layout)
	after := manifest.LoadIn(newFiles, layout)
	if len(after.Errors) > 0 {
		return sha, prOutcome{"failure", "Invalid Application manifest: " + after.Errors[0].Error()}
	}

	// The PR's own manifests apply: merging is the approval (who may change
	// .towercrane/ is up to GitHub's branch protection and CODEOWNERS).
	var violations, policyChanges int
	for _, app := range after.Apps {
		if prev := before.ByName(app.Metadata.Name); prev == nil || prev.Hash != app.Hash {
			policyChanges++
		}
		violations += len(tfmap.DiffGate(app, oldFiles, newFiles, changed))
	}

	switch {
	case violations > 0:
		return sha, prOutcome{"failure", fmt.Sprintf("%d change(s) outside the Applications' editScope", violations)}
	case policyChanges > 0:
		return sha, prOutcome{"success", fmt.Sprintf("TowerCrane policy OK; changes %d Application manifest(s): merging applies them", policyChanges)}
	default:
		return sha, prOutcome{"success", "TowerCrane policy OK"}
	}
}

// post reports a commit status on GitHub.
func (p *PRChecker) post(ctx context.Context, repo *models.Repository, sha string, o prOutcome) {
	client, err := p.gh.Client(ctx, repo)
	if err != nil {
		log.Printf("pr-check: no GitHub client for %s: %v", repo.FullName, err)
		return
	}
	desc := o.description
	if len(desc) > 140 { // GitHub's limit
		desc = desc[:137] + "..."
	}
	status := &github.RepoStatus{
		State:       github.String(o.state),
		Context:     github.String(PolicyStatusContext),
		Description: github.String(desc),
	}
	if front := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/"); front != "" {
		status.TargetURL = github.String(front + path.Join("/repositories", repo.ID, "operations"))
	}
	if _, _, err := client.Raw().Repositories.CreateStatus(ctx, repo.Owner, repo.Name, sha, status); err != nil {
		log.Printf("pr-check: failed to post status on %s@%s: %v", repo.FullName, sha, err)
	}
}
