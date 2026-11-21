package api

import (
	"fmt"
	"path"
	"strings"

	"github.com/JESUSLUG/TowerCrane/backend/internal/git"
	"github.com/JESUSLUG/TowerCrane/backend/internal/iac"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/tfmap"
)

// The TowerCrane policy lives in Application manifests (.towercrane/*.yaml).
// For every operation TowerCrane:
//
//  1. loads the Application at the commit being applied;
//  2. checks the diff against the manifest's editScope (files + blocks);
//  3. after planning, checks the real plan (resources + actions).
//
// Manifests themselves are not approved in TowerCrane: what is merged on the
// default branch is the policy (GitOps). Who may change .towercrane/ is
// decided in GitHub (branch protection, CODEOWNERS); TowerCrane reports how
// well a repository is protected (see protection.go).
//
// The first apply of an Application ("onboarding") has nothing to diff
// against: the edit scope is skipped (only the plan guard applies) and it
// always needs an approver's "yes", even for apply: auto.

// repoFileSuffixes are the files TowerCrane reads from a commit.
var repoFileSuffixes = []string{".tf", ".hcl", ".yaml", ".yml"}

// readRepoFiles reads the Terraform files and YAML files (manifests, wherever
// the project keeps them) of a commit. Which YAML is a manifest is decided by
// the project's layout (manifest.LoadIn).
func readRepoFiles(dir, ref string) (map[string]string, error) {
	return git.ReadFilesAt(dir, ref, repoFileSuffixes...)
}

// resolveApp loads an Application from a commit's files.
func resolveApp(files map[string]string, name string, layout manifest.Layout) (*manifest.Application, error) {
	res := manifest.LoadIn(files, layout)
	if app := res.ByName(name); app != nil {
		return app, nil
	}
	for _, err := range res.Errors {
		if strings.Contains(err.Error(), fmt.Sprintf("%q", name)) {
			return nil, err
		}
	}
	if len(res.Errors) > 0 {
		return nil, fmt.Errorf("Application %q not found (some manifests are invalid: %v)", name, res.Errors[0])
	}
	return nil, fmt.Errorf("Application %q not found in %s", name, layout)
}

// appDecision is the outcome of evaluating an Application at a commit.
type appDecision struct {
	App *manifest.Application
	// Onboarding: the Application was never applied by TowerCrane.
	Onboarding bool
	Violations     []tfmap.Violation
	// Files holds the repository's .tf files and manifests at the commit.
	Files map[string]string
}

func (d *appDecision) Allowed() bool { return len(d.Violations) == 0 }

// NeedsConfirmation: apply: manual, or the very first apply.
func (d *appDecision) NeedsConfirmation() bool { return d.App.ApplyMode() != "auto" || d.Onboarding }

// planApp is the Application used for the plan gate: during onboarding
// everything is new, so the editScope doesn't apply (the syncPolicy rules
// still do).
func (d *appDecision) planApp() *manifest.Application {
	if !d.Onboarding {
		return d.App
	}
	cp := *d.App
	cp.Spec.EditScope = manifest.EditScope{All: true}
	return &cp
}

// Describe renders why a decision blocks an apply.
func (d *appDecision) Describe() string {
	var b strings.Builder
	if len(d.Violations) > 0 {
		b.WriteString("Changes outside the Application's editScope:")
		for _, v := range d.Violations {
			b.WriteString("\n  - " + v.String())
		}
	}
	return b.String()
}

// baselineFor is the commit an Application's policy is evaluated against:
// the commit of its code TowerCrane last applied, or git's empty tree.
func (oh *OperationHandler) baselineFor(repoID, application, localPath string) (string, error) {
	last, err := oh.operationStore.GetLastSuccessfulApply(repoID, application)
	if err != nil || last.SourceCommitSHA == "" {
		return git.EmptyTree, nil
	}
	if !git.CommitExists(localPath, last.SourceCommitSHA) {
		return "", fmt.Errorf(
			"last applied commit %s is no longer in the repository history (force-push?); an admin must re-baseline this Application with an emergency apply",
			short(last.SourceCommitSHA))
	}
	return last.SourceCommitSHA, nil
}

// evaluateApp runs steps 1-2 of the policy for an Application in its
// workspace (the code's commit is what is compared). Every failure is
// returned as an error so callers fail closed.
func (oh *OperationHandler) evaluateApp(repoID string, ws *appWorkspace) (*appDecision, error) {
	app, localPath, sha, newFiles := ws.App, ws.CodePath, ws.CodeSHA, ws.Files
	dec := &appDecision{App: app, Files: newFiles}

	baseline, err := oh.baselineFor(repoID, app.Metadata.Name, localPath)
	if err != nil {
		return nil, err
	}
	dec.Onboarding = baseline == git.EmptyTree
	if baseline == sha {
		return dec, nil // nothing changed since the last apply
	}

	oldFiles, err := readRepoFiles(localPath, baseline)
	if err != nil {
		return nil, err
	}

	// 2. The code changes, against the manifest.
	if !dec.Onboarding {
		diffs, err := git.DiffFiles(localPath, baseline, sha)
		if err != nil {
			return nil, err
		}
		changed := make([]string, len(diffs))
		for i, d := range diffs {
			changed[i] = d.Path
		}
		dec.Violations = tfmap.DiffGate(app, oldFiles, newFiles, changed)
	}
	return dec, nil
}

// planReview is step 3 on a saved plan: the editScope check (violations)
// and the syncPolicy rules (how the plan may be applied).
type planReview struct {
	Violations []tfmap.Violation
	Decision   manifest.Decision
}

func reviewPlan(executor *iac.Executor, planFile string, dec *appDecision) (*planReview, error) {
	planned, err := executor.ShowPlan(planFile)
	if err != nil {
		return nil, err
	}
	var gate []tfmap.PlanChange
	var rules []manifest.PlannedChange
	for _, c := range planned {
		if c.Mode == "data" {
			continue
		}
		action := tfmap.PlanActionFromTofu(c.Actions)
		gate = append(gate, tfmap.PlanChange{Address: c.Address, Action: action})
		rules = append(rules, manifest.PlannedChange{Address: c.Address, Action: action})
	}
	return &planReview{
		Violations: tfmap.PlanGate(dec.planApp(), dec.Files, gate),
		Decision:   dec.App.Decide(rules),
	}, nil
}

// blockedReason explains why a reviewed plan can't be applied, or "".
func (r *planReview) blockedReason() string {
	if len(r.Violations) > 0 {
		return describeViolations("The plan changes things outside the Application's editScope:", r.Violations)
	}
	if r.Decision.Apply == manifest.ApplyReject {
		return describeReasons("Rejected by the Application's syncPolicy rules:", r.Decision.Reasons)
	}
	return ""
}

func describeReasons(title string, reasons []string) string {
	var b strings.Builder
	b.WriteString(title)
	for _, r := range reasons {
		b.WriteString("\n  - " + r)
	}
	return b.String()
}

// isTerragrunt reports whether an Application's path is a Terragrunt unit.
func isTerragrunt(files map[string]string, app *manifest.Application) bool {
	_, ok := files[path.Join(app.Spec.Source.Path, "terragrunt.hcl")]
	return ok
}

func describeViolations(title string, vs []tfmap.Violation) string {
	var b strings.Builder
	b.WriteString(title)
	for _, v := range vs {
		b.WriteString("\n  - " + v.String())
	}
	return b.String()
}
