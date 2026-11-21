package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v56/github"
)

// TowerCrane applies whatever is merged on the default branch: merging is
// the approval (GitOps). Who may change the policy (.towercrane/) is decided
// in GitHub. This report tells admins whether that is actually locked down:
// a protected default branch, pull requests required, and CODEOWNERS
// reviews on .towercrane/. For full separation, keep the manifests in a
// repository developers can't write to.

const (
	checkOK      = "ok"
	checkMissing = "missing"
	checkUnknown = "unknown"
)

type protectionCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	State  string `json:"state"` // ok, missing, unknown
	Detail string `json:"detail"`
}

type protectionReport struct {
	Branch string `json:"branch"`
	// Level: "ok" (every check passes), "warning" (something is missing:
	// people who can push may change the policy) or "unknown".
	Level  string            `json:"level"`
	Checks []protectionCheck `json:"checks"`
}

// codeownersPaths are where GitHub looks for CODEOWNERS, in its order.
var codeownersPaths = []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"}

// GetProtection reports how well a repository's TowerCrane policy is
// protected in GitHub.
func (h *RepositoryHandler) GetProtection(c *gin.Context) {
	repo, err := h.repoStore.GetRepositoryByID(c.Param("repo_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}
	ctx := c.Request.Context()
	client, err := h.gh.Client(ctx, repo)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to access GitHub: " + err.Error()})
		return
	}
	branch := repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	layout := manifest.DefaultLayout()
	if h.projects != nil {
		layout = h.projects.Layout(repo.ID)
	}
	c.JSON(http.StatusOK, checkProtection(ctx, client.Raw(), repo.Owner, repo.Name, branch, layout))
}

func checkProtection(ctx context.Context, gh *github.Client, owner, name, branch string, layout manifest.Layout) protectionReport {
	where := layout.String() // ".towercrane/", or "/" for the whole repository
	rule := "/" + strings.TrimSuffix(where, "/") + "/ @your-org/platform"
	if layout.Root() {
		rule = "* @your-org/platform"
	}
	report := protectionReport{Branch: branch}

	// 1. Branch protected at all (classic protection or a ruleset).
	protected := protectionCheck{ID: "branch_protected", Label: "Default branch is protected"}
	if b, _, err := gh.Repositories.GetBranch(ctx, owner, name, branch, 1); err != nil {
		protected.State, protected.Detail = checkUnknown, "Could not read the branch: "+err.Error()
	} else if b.GetProtected() {
		protected.State, protected.Detail = checkOK, branch+" has branch protection or rules"
	} else {
		protected.State, protected.Detail = checkMissing, "Anyone with push access can push to "+branch+" directly, changing the manifests ("+where+") without review"
	}

	// 2. Pull requests and code owner reviews, from rulesets and classic
	// protection (the latter needs the App's "Administration: read").
	requirePR, codeOwnerReview, approvals := false, false, 0
	rulesKnown, classicKnown := false, false
	if rules, _, err := gh.Repositories.GetRulesForBranch(ctx, owner, name, branch); err == nil {
		rulesKnown = true
		for _, r := range rules {
			if r.Type != "pull_request" || r.Parameters == nil {
				continue
			}
			var p github.PullRequestRuleParameters
			if json.Unmarshal(*r.Parameters, &p) == nil {
				requirePR = true
				codeOwnerReview = codeOwnerReview || p.RequireCodeOwnerReview
				approvals = max(approvals, p.RequiredApprovingReviewCount)
			}
		}
	}
	if p, resp, err := gh.Repositories.GetBranchProtection(ctx, owner, name, branch); err == nil {
		classicKnown = true
		if rv := p.RequiredPullRequestReviews; rv != nil {
			requirePR = true
			codeOwnerReview = codeOwnerReview || rv.RequireCodeOwnerReviews
			approvals = max(approvals, rv.RequiredApprovingReviewCount)
		}
	} else if resp != nil && resp.StatusCode == http.StatusNotFound {
		classicKnown = true // no classic protection
	}
	// An unprotected branch requires nothing: no need for the details.
	known := (rulesKnown && classicKnown) || protected.State == checkMissing
	pr := protectionCheck{ID: "requires_pr", Label: "Changes need a pull request"}
	switch {
	case requirePR:
		pr.State, pr.Detail = checkOK, "Pull requests are required"
		if approvals > 0 {
			pr.Detail += " with at least " + strconv.Itoa(approvals) + " approving review(s)"
		}
	case known:
		pr.State, pr.Detail = checkMissing, "Changes can reach "+branch+" without a pull request review"
	default:
		pr.State, pr.Detail = checkUnknown, "TowerCrane can't read the classic branch protection; give the GitHub App \"Administration: read\" to check it"
	}

	// 3. CODEOWNERS covering .towercrane/.
	owners := protectionCheck{ID: "codeowners", Label: "CODEOWNERS covers the manifests (" + where + ")"}
	file, content := "", ""
	for _, p := range codeownersPaths {
		fc, _, _, err := gh.Repositories.GetContents(ctx, owner, name, p, &github.RepositoryContentGetOptions{Ref: branch})
		if err != nil || fc == nil {
			continue
		}
		if text, err := fc.GetContent(); err == nil {
			file, content = p, text
			break
		}
	}
	who, uncovered := codeownersCoverage(content, layout)
	switch {
	case file == "":
		owners.State, owners.Detail = checkMissing, "No CODEOWNERS file: add one with a line like \""+rule+"\""
	case len(uncovered) > 0:
		owners.State, owners.Detail = checkMissing, file+" has no owner for "+strings.Join(uncovered, ", ")+
			": a rule like \""+rule+"\" covers every manifest, subfolders included"
	default:
		owners.State, owners.Detail = checkOK, "The manifests ("+where+") are owned by "+strings.Join(who, ", ")+" ("+file+")"
	}

	// 4. Code owner reviews enforced.
	review := protectionCheck{ID: "code_owner_review", Label: "Code owners must approve"}
	switch {
	case codeOwnerReview:
		review.State, review.Detail = checkOK, "Pull requests touching the manifests need a code owner's approval"
	case known:
		review.State, review.Detail = checkMissing, "Enable \"Require review from Code Owners\" so CODEOWNERS is enforced, not just suggested"
	default:
		review.State, review.Detail = checkUnknown, "Not visible to TowerCrane (needs \"Administration: read\" for classic protection)"
	}

	report.Checks = []protectionCheck{protected, pr, owners, review}
	report.Level = checkOK
	for _, ch := range report.Checks {
		if ch.State == checkMissing {
			report.Level = "warning"
			break
		}
		if ch.State == checkUnknown {
			report.Level = checkUnknown
		}
	}
	return report
}

// policyPaths are sample paths of every kind of TowerCrane manifest where a
// layout keeps them: CODEOWNERS must cover all of them (a rule like
// "/.towercrane/*.yaml" misses the subfolders, where Groups, Diagrams and
// Jobs live).
func policyPaths(layout manifest.Layout) []string {
	prefix := ""
	if !layout.Root() {
		prefix = layout.Dir + "/"
	}
	return []string{
		prefix + "app.yaml",
		prefix + "sandbox/app.yaml",
		prefix + "groups/group.yaml",
		prefix + "diagrams/diagram.yaml",
		prefix + "jobs/job.yaml",
	}
}

// codeownersCoverage returns the owners of the policy (from the first
// sample path) and the folders no rule covers.
func codeownersCoverage(content string, layout manifest.Layout) (owners []string, uncovered []string) {
	for i, p := range policyPaths(layout) {
		who := codeownersFor(content, p)
		if i == 0 {
			owners = who
		}
		if len(who) == 0 {
			dir := p[:strings.LastIndex(p, "/")+1]
			if dir == "" {
				dir = "/"
			}
			uncovered = append(uncovered, dir)
		}
	}
	return owners, uncovered
}

// codeownersFor returns the owners CODEOWNERS assigns to a path: the last
// matching line wins, as in GitHub (a matching line without owners means
// nobody owns it).
func codeownersFor(content, path string) []string {
	var owners []string
	for _, line := range strings.Split(content, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if codeownersMatch(fields[0], path) {
			owners = fields[1:]
		}
	}
	return owners
}

// codeownersMatch implements the gitignore-style patterns CODEOWNERS uses,
// enough for directory and glob rules.
func codeownersMatch(pattern, path string) bool {
	anchored := strings.HasPrefix(pattern, "/") || strings.Contains(strings.TrimSuffix(pattern, "/"), "/")
	p := strings.TrimPrefix(pattern, "/")
	dirOnly := strings.HasSuffix(p, "/")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return false
	}
	if !anchored {
		p = "**/" + p
	}
	if dirOnly {
		return manifest.MatchGlob(p+"/**", path)
	}
	return manifest.MatchGlob(p, path) || manifest.MatchGlob(p+"/**", path)
}
