package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/git"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
)

// appWorkspace is where one operation of an Application runs: the
// project's configuration repository gives the manifests (at ConfigSHA),
// and the repository with the Application's code (spec.source.repo, or the
// configuration repository itself) is cloned at CodeSHA. Terraform runs in
// CodePath, and the policy (editScope, diff, baseline) looks at the code's
// commits.
type appWorkspace struct {
	App       *manifest.Application
	ConfigSHA string
	Code      *models.Repository
	CodePath  string
	CodeSHA   string
	// Files: the code's .tf/.hcl files plus the configuration's manifests.
	Files  map[string]string
	unlock func()
}

// Close releases the workspace lock.
func (w *appWorkspace) Close() {
	if w != nil && w.unlock != nil {
		w.unlock()
		w.unlock = nil
	}
}

// sameRepo compares GitHub "owner/name" (case-insensitive, like GitHub).
func sameRepo(a, b string) bool { return strings.EqualFold(a, b) }

// codeRepo resolves the repository with an Application's code. It must be
// the project's configuration repository or one of its source repositories:
// a manifest can't run code from a repository an admin didn't add to the
// project.
func (oh *OperationHandler) codeRepo(project *models.Repository, app *manifest.Application) (*models.Repository, error) {
	return resolveCodeRepo(oh.projects, project, app)
}

func resolveCodeRepo(projects *db.ProjectStore, project *models.Repository, app *manifest.Application) (*models.Repository, error) {
	name := app.CodeRepo(project.FullName)
	if sameRepo(name, project.FullName) {
		return project, nil
	}
	sources, err := projects.Sources(project.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the project's source repositories: %w", err)
	}
	for _, r := range sources {
		if sameRepo(r.FullName, name) {
			return r, nil
		}
	}
	return nil, fmt.Errorf("%s (spec.source.repo) is not a source repository of project %s: an admin can add it in the project's settings",
		name, project.FullName)
}

// openWorkspace clones/updates what an operation of the Application needs
// and records both commits on the operation. The caller must Close it.
func (oh *OperationHandler) openWorkspace(op *db.Operation, project *models.Repository, application string) (*appWorkspace, error) {
	unlockConfig := git.LockWorkspace(project.FullName)
	configPath, err := oh.prepareWorkspace(op, project)
	if err != nil {
		unlockConfig()
		return nil, err
	}
	configFiles, err := readRepoFiles(configPath, op.CommitSHA)
	if err != nil {
		unlockConfig()
		return nil, err
	}
	layout := oh.projects.Layout(project.ID)
	app, err := resolveApp(configFiles, application, layout)
	if err != nil {
		unlockConfig()
		return nil, err
	}
	code, err := oh.codeRepo(project, app)
	if err != nil {
		unlockConfig()
		return nil, err
	}

	if code.ID == project.ID {
		op.SourceCommitSHA = op.CommitSHA
		oh.operationStore.UpdateSourceCommit(op.ID, op.CommitSHA)
		return &appWorkspace{App: app, ConfigSHA: op.CommitSHA, Code: project, CodePath: configPath,
			CodeSHA: op.CommitSHA, Files: configFiles, unlock: unlockConfig}, nil
	}

	// The code lives elsewhere: the configuration clone is only read (at a
	// fixed commit), so its lock can go; Terraform runs in the code clone.
	unlockConfig()
	unlockCode := git.LockWorkspace(code.FullName)
	token, err := oh.gh.Token(context.Background(), code)
	if err != nil {
		unlockCode()
		return nil, err
	}
	branch := code.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	codePath, err := git.EnsureWorkspace(code.FullName, code.URL, branch, token)
	if err != nil {
		unlockCode()
		return nil, fmt.Errorf("failed to prepare the workspace of %s: %w", code.FullName, err)
	}
	codeSHA, err := git.CurrentCommit(codePath)
	if err != nil {
		unlockCode()
		return nil, err
	}
	op.SourceCommitSHA = codeSHA
	oh.operationStore.UpdateSourceCommit(op.ID, codeSHA)
	codeFiles, err := readRepoFiles(codePath, codeSHA)
	if err != nil {
		unlockCode()
		return nil, err
	}
	return &appWorkspace{App: app, ConfigSHA: op.CommitSHA, Code: code, CodePath: codePath, CodeSHA: codeSHA,
		Files: mergeProjectFiles(configFiles, codeFiles, layout), unlock: unlockCode}, nil
}

// mergeProjectFiles combines the manifests of the configuration repository
// (where its layout keeps them) with the Terraform/Terragrunt files of a code
// repository (its YAML, manifests included, is not the project's
// configuration).
func mergeProjectFiles(config, code map[string]string, layout manifest.Layout) map[string]string {
	out := make(map[string]string, len(config)+len(code))
	for p, c := range code {
		if !strings.HasSuffix(p, ".yaml") && !strings.HasSuffix(p, ".yml") {
			out[p] = c
		}
	}
	for p, c := range config {
		if layout.Holds(p) {
			out[p] = c
		}
	}
	return out
}
