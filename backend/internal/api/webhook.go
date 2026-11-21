package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/git"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v56/github"
)

// WebhookHandler receives GitHub webhook deliveries (from the GitHub App, or
// from a repository webhook for repositories connected without the app):
//
//   - push to the default branch: plan every environment and auto-apply the
//     ones that are syncPolicy.apply: auto when the plan passes the policy.
//   - pull_request: evaluate the TowerCrane policy before merge and report
//     it as the `towercrane/policy` commit status.
//   - installation: forget installations that were uninstalled on GitHub.
//
// Every delivery must be signed with GITHUB_WEBHOOK_SECRET; without a
// secret configured, the webhook refuses everything.
type WebhookHandler struct {
	repoStore    *db.RepositoryStore
	userStore    *db.UserStore
	integrations *db.IntegrationStore
	ops          *OperationHandler
	rollouts     *RolloutEngine
	prs          *PRChecker
	gh           *GitHubAccess
	snapshots    *SnapshotStore
	projects     *db.ProjectStore
	secret       string
}

func NewWebhookHandler(repoStore *db.RepositoryStore, userStore *db.UserStore, integrations *db.IntegrationStore,
	ops *OperationHandler, rollouts *RolloutEngine, prs *PRChecker, gh *GitHubAccess, snapshots *SnapshotStore,
	projects *db.ProjectStore) *WebhookHandler {
	return &WebhookHandler{
		projects:     projects,
		snapshots:    snapshots,
		rollouts:     rollouts,
		repoStore:    repoStore,
		userStore:    userStore,
		integrations: integrations,
		ops:          ops,
		prs:          prs,
		gh:           gh,
		secret:       os.Getenv("GITHUB_WEBHOOK_SECRET"),
	}
}

type githubPushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
}

type githubPullRequestPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
}

type githubInstallationPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

func (h *WebhookHandler) HandleGitHubPush(c *gin.Context) {
	if h.secret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GITHUB_WEBHOOK_SECRET is not configured"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 25<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	if !validSignature(h.secret, body, c.GetHeader("X-Hub-Signature-256")) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}

	switch c.GetHeader("X-GitHub-Event") {
	case "push":
		h.handlePush(c, body)
	case "pull_request":
		if !PRChecksEnabled() {
			c.JSON(http.StatusOK, gin.H{"message": "pull request checks are disabled (TOWERCRANE_PR_CHECKS)"})
			return
		}
		h.handlePullRequest(c, body)
	case "installation":
		h.handleInstallation(c, body)
	case "ping":
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	default:
		c.JSON(http.StatusOK, gin.H{"message": "ignored event"})
	}
}

func (h *WebhookHandler) handlePush(c *gin.Context, body []byte) {
	var payload githubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	repo, err := h.repoStore.GetRepositoryByFullName(payload.Repository.FullName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "repository not connected to TowerCrane, ignoring"})
		return
	}

	defaultBranch := repo.DefaultBranch
	if payload.Repository.DefaultBranch != "" {
		defaultBranch = payload.Repository.DefaultBranch
	}
	if payload.Ref != "refs/heads/"+defaultBranch {
		c.JSON(http.StatusOK, gin.H{"message": "not the default branch, ignoring"})
		return
	}

	go h.onDefaultBranchCommit(repo, payload.After)

	c.JSON(http.StatusAccepted, gin.H{"message": "processing push"})
}

func (h *WebhookHandler) handlePullRequest(c *gin.Context, body []byte) {
	var payload githubPullRequestPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	repo, err := h.repoStore.GetRepositoryByFullName(payload.Repository.FullName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "repository not connected to TowerCrane, ignoring"})
		return
	}

	defaultBranch := repo.DefaultBranch
	if payload.Repository.DefaultBranch != "" {
		defaultBranch = payload.Repository.DefaultBranch
	}

	switch payload.Action {
	case "opened", "synchronize", "reopened", "edited", "ready_for_review":
		if payload.PullRequest.Base.Ref != defaultBranch {
			// Only PRs into the branch TowerCrane applies from matter. If the
			// base was edited away from it, stop tracking the PR.
			h.prs.Forget(repo, payload.Number)
			c.JSON(http.StatusOK, gin.H{"message": "PR does not target the default branch, ignoring"})
			return
		}
		go h.prs.Check(repo, payload.Number, payload.PullRequest.Head.SHA)
		c.JSON(http.StatusAccepted, gin.H{"message": "evaluating pull request"})
	case "closed":
		h.prs.Forget(repo, payload.Number)
		c.JSON(http.StatusOK, gin.H{"message": "stopped tracking pull request"})
	default:
		c.JSON(http.StatusOK, gin.H{"message": "ignored pull_request action"})
	}
}

func (h *WebhookHandler) handleInstallation(c *gin.Context, body []byte) {
	var payload githubInstallationPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if payload.Action == "deleted" {
		if err := h.integrations.DeleteInstallation(payload.Installation.ID); err != nil {
			log.Printf("webhook: failed to delete installation %d: %v", payload.Installation.ID, err)
		}
		if err := h.repoStore.ClearInstallation(payload.Installation.ID); err != nil {
			log.Printf("webhook: failed to detach repositories from installation %d: %v", payload.Installation.ID, err)
		}
		if h.gh.app != nil {
			h.gh.app.Forget(payload.Installation.ID)
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func validSignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return false
	}
	expected := make([]byte, sha256.Size)
	if _, err := hex.Decode(expected, []byte(header[len(prefix):])); err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), expected)
}

// onDefaultBranchCommit reacts to a new commit on a repository's default
// branch, from a push webhook or from polling. Each commit is processed only
// once: every project that uses the repository (as its configuration or as
// a source of code) rolls out.
func (h *WebhookHandler) onDefaultBranchCommit(repo *models.Repository, sha string) {
	if claimed, err := h.claimCommit(repo, sha); err != nil || !claimed {
		return // already handled (webhook and poller both saw it)
	}
	h.onClaimedCommit(repo, sha)
}

// onClaimedCommit keeps the commit's files and rolls out the projects that
// use the repository.
func (h *WebhookHandler) onClaimedCommit(repo *models.Repository, sha string) {
	h.onClaimedCommits(map[*models.Repository]string{repo: sha}, "")
}

// onClaimedCommits keeps the new commits' files and rolls out every project
// that uses any of those repositories, once. forceProject (a project's ID,
// or "") is rolled out planning everything (Refresh with force).
func (h *WebhookHandler) onClaimedCommits(commits map[*models.Repository]string, forceProject string) {
	ctx := context.Background()
	projects := map[string]bool{}
	for repo, sha := range commits {
		if _, err := h.snapshots.readSnapshot(ctx, h.gh, repo, sha); err != nil {
			log.Printf("webhook: %s: %v", repo.FullName, err)
		}
		ids, err := h.projects.ProjectsUsing(repo.ID)
		if err != nil {
			log.Printf("webhook: %s: %v", repo.FullName, err)
			continue
		}
		for _, id := range ids {
			projects[id] = true
		}
	}
	if forceProject != "" {
		projects[forceProject] = true
	}
	for id := range projects {
		project, err := h.repoStore.GetRepositoryByID(id)
		if err != nil {
			continue
		}
		h.processPush(project, id == forceProject)
	}
}

// claimCommit records sha as handled; false means someone already did.
func (h *WebhookHandler) claimCommit(repo *models.Repository, sha string) (bool, error) {
	if sha == "" {
		return true, nil
	}
	claimed, err := h.repoStore.ClaimCommit(repo.ID, sha)
	if err != nil {
		log.Printf("failed to record commit %s for %s: %v", sha, repo.FullName, err)
	}
	return claimed, err
}

// processPush starts the rollout of a project (its configuration
// repository) at the current commits of its repositories. What is merged
// is the policy: there is no second approval in TowerCrane.
//
// planAll plans every Application (Refresh with force); otherwise only the
// ones the new commits can affect are planned (see affected.go) and the
// rest are "unchanged".
func (h *WebhookHandler) processPush(project *models.Repository, planAll bool) {
	ctx := context.Background()

	// The configuration's files (manifests) at its current commit.
	snap, err := h.snapshots.readSnapshot(ctx, h.gh, project, "")
	if err != nil {
		log.Printf("rollout: %s: %v", project.FullName, err)
		return
	}
	configFiles, configHead := snap.Files, snap.SHA

	layout := h.projects.Layout(project.ID)
	apps := manifest.LoadIn(configFiles, layout)
	for _, err := range apps.Errors {
		log.Printf("rollout: %s: invalid manifest: %v", project.FullName, err)
	}

	// Ephemeral Applications (lifecycle: job) only exist while their Job
	// keeps them up: a merge never applies them.
	rolled := make([]*manifest.Application, 0, len(apps.Apps))
	for _, a := range apps.Apps {
		if a.Spec.Lifecycle != manifest.LifecycleJob {
			rolled = append(rolled, a)
		}
	}

	// Each Application's code: its repository's current commit and files.
	inputs := make([]appInputs, 0, len(rolled))
	codeHeads := map[string]string{}
	for _, a := range rolled {
		in := appInputs{App: a, Files: configFiles, CodeRepo: project.FullName, CodeHead: configHead}
		if code, err := h.ops.codeRepo(project, a); err != nil {
			log.Printf("rollout: %s: %s: %v", project.FullName, a.Metadata.Name, err) // its plan will fail with it
		} else if code.ID != project.ID {
			cs, err := h.snapshots.readSnapshot(ctx, h.gh, code, "")
			if err != nil {
				log.Printf("rollout: %s: %s: %v", project.FullName, code.FullName, err)
			} else {
				in.Files, in.CodeRepo, in.CodeHead = mergeProjectFiles(configFiles, cs.Files, layout), code.FullName, cs.SHA
			}
		}
		if in.Settled, err = h.rollouts.store.LastSettled(project.ID, a.Metadata.Name); err != nil {
			log.Printf("rollout: %s: %v", project.FullName, err)
		}
		codeHeads[a.Metadata.Name] = in.CodeHead
		inputs = append(inputs, in)
	}

	var reasons map[string]planReason // nil: plan everything
	if !planAll {
		reasons = affectedApps(inputs, project.FullName, configHead, h.compareFunc(ctx, project))
		for _, in := range inputs {
			if why := reasons[in.App.Metadata.Name]; why != "" {
				log.Printf("rollout: %s %s: planning %s (%s)", project.FullName, short(configHead), in.App.Metadata.Name, why)
			}
		}
	}
	h.rollouts.Start(project, configHead, rolled, reasons, codeHeads)
}

// compareFunc lists the files changed between two commits of a repository
// with GitHub's compare API (one call per pair, cached for the rollout).
func (h *WebhookHandler) compareFunc(ctx context.Context, project *models.Repository) changesFunc {
	type result struct {
		files []string
		err   error
	}
	cache := map[string]result{}
	return func(repoName, base, head string) ([]string, error) {
		key := repoName + " " + base + " " + head
		if r, ok := cache[key]; ok {
			return r.files, r.err
		}
		var r result
		repo := project
		if !sameRepo(repoName, project.FullName) {
			repo, r.err = h.repoStore.GetRepositoryByFullName(repoName)
		}
		if r.err == nil {
			var client *git.GitHubClient
			if client, r.err = h.gh.Client(ctx, repo); r.err == nil {
				comp, _, err := client.Raw().Repositories.CompareCommits(ctx, repo.Owner, repo.Name, base, head, &github.ListOptions{PerPage: 100})
				switch {
				case err != nil:
					r.err = err
				case len(comp.Files) >= 300: // GitHub's limit: the list may be incomplete
					r.err = fmt.Errorf("more than 300 files changed")
				default:
					for _, f := range comp.Files {
						r.files = append(r.files, f.GetFilename())
						if prev := f.GetPreviousFilename(); prev != "" {
							r.files = append(r.files, prev) // renames touch both paths
						}
					}
				}
			}
		}
		cache[key] = r
		return r.files, r.err
	}
}

func webhookSecretConfigured() bool { return os.Getenv("GITHUB_WEBHOOK_SECRET") != "" }
