package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JESUSLUG/TowerCrane/backend/internal/auth"
	"github.com/JESUSLUG/TowerCrane/backend/internal/cloud"
	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/git"
	"github.com/JESUSLUG/TowerCrane/backend/internal/iac"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
	"github.com/JESUSLUG/TowerCrane/backend/internal/secrets"
	"github.com/gin-gonic/gin"
)

type OperationHandler struct {
	gh              *GitHubAccess
	integrations    *db.IntegrationStore
	operationStore  *db.OperationStore
	repositoryStore *db.RepositoryStore
	userStore       *db.UserStore

	// onPlanResolved tells the rollout engine that a plan waiting for a
	// "yes" was confirmed (apply = its apply, possibly still running) or
	// discarded (apply = nil).
	onPlanResolved func(planID string, apply *db.Operation)

	// projects resolves an Application's code repository (spec.source.repo).
	projects *db.ProjectStore

	// Live logs of running operations (see livelog.go).
	logsMu sync.Mutex
	logs   map[string]*liveLog
}

type OperationRequest struct {
	RepositoryID string `json:"repository_id" binding:"required"`
	// Application is the manifest's metadata.name.
	Application string `json:"application" binding:"required"`
	// Emergency (admins only) runs the operation without the TowerCrane
	// policy ("break glass").
	Emergency bool `json:"emergency"`
	// Confirm must repeat the Application name for emergency apply/destroy,
	// as a deliberate second step.
	Confirm string `json:"confirm"`
}

type OperationResponse struct {
	ID                   string `json:"id"`
	RepositoryID         string `json:"repository_id"`
	Type                 string `json:"type"`
	Status               string `json:"status"`
	Application          string `json:"application"`
	Environment          string `json:"environment"`
	WorkingDir           string `json:"working_dir"`
	CommitSHA            string `json:"commit_sha,omitempty"`
	CreatedAt            string `json:"created_at"`
	Log                  string `json:"log,omitempty"`
	Error                string `json:"error,omitempty"`
	Plan                 string `json:"plan,omitempty"`
	Emergency            bool   `json:"emergency"`
	HasChanges           bool   `json:"has_changes"`
	AwaitingConfirmation bool   `json:"awaiting_confirmation"`
	PolicyNote           string `json:"policy_note,omitempty"`
	SourcePlanID         string `json:"source_plan_id,omitempty"`
}

func NewOperationHandler(operationStore *db.OperationStore, repositoryStore *db.RepositoryStore, userStore *db.UserStore,
	integrations *db.IntegrationStore, gh *GitHubAccess) *OperationHandler {
	return &OperationHandler{
		gh:              gh,
		integrations:    integrations,
		operationStore:  operationStore,
		repositoryStore: repositoryStore,
		userStore:       userStore,
	}
}

func toOperationResponse(op *db.Operation) *OperationResponse {
	return &OperationResponse{
		ID:                   op.ID,
		RepositoryID:         op.RepositoryID,
		Type:                 op.Type,
		Status:               op.Status,
		Application:          op.Application,
		Environment:          op.Environment,
		WorkingDir:           op.WorkingDir,
		CommitSHA:            op.CommitSHA,
		CreatedAt:            op.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Log:                  op.Log,
		Error:                op.Error,
		Plan:                 op.Plan,
		Emergency:            op.Emergency,
		HasChanges:           op.HasChanges,
		AwaitingConfirmation: op.AwaitingConfirmation,
		PolicyNote:           op.PolicyNote,
		SourcePlanID:         op.SourcePlanID,
	}
}

func (oh *OperationHandler) ExecutePlan(c *gin.Context)    { oh.createAndRun(c, "plan") }
func (oh *OperationHandler) ExecuteApply(c *gin.Context)   { oh.createAndRun(c, "apply") }
func (oh *OperationHandler) ExecuteDestroy(c *gin.Context) { oh.createAndRun(c, "destroy") }

// createAndRun validates the request, persists a pending Operation record,
// and kicks off the real `tofu` invocation in the background so the HTTP
// request returns immediately; the frontend polls GetOperation for progress.
func (oh *OperationHandler) createAndRun(c *gin.Context, opType string) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req OperationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Application = strings.TrimSpace(req.Application)
	if req.Emergency {
		if !auth.Role(c.GetString("role")).AtLeast(auth.RoleAdmin) {
			c.JSON(http.StatusForbidden, gin.H{"error": "only admins can run emergency operations"})
			return
		}
		if opType != "plan" && strings.TrimSpace(req.Confirm) != req.Application {
			c.JSON(http.StatusBadRequest, gin.H{"error": "to confirm an emergency " + opType + ", type the Application name (" + req.Application + ")"})
			return
		}
	}

	repo, err := oh.repositoryStore.GetRepositoryByID(req.RepositoryID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}

	logMsg := fmt.Sprintf("Initializing %s operation...", opType)
	if req.Emergency {
		logMsg = fmt.Sprintf("EMERGENCY %s requested by an admin: the TowerCrane policy is skipped for this operation.", opType)
	}
	operation := &db.Operation{
		RepositoryID: req.RepositoryID,
		UserID:       userID,
		Type:         opType,
		Status:       "pending",
		Application:  req.Application,
		Environment:  req.Application,
		Emergency:    req.Emergency,
		Log:          logMsg,
	}
	if err := oh.operationStore.Create(operation); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create operation"})
		return
	}

	go oh.run(operation, repo)

	c.JSON(http.StatusAccepted, toOperationResponse(operation))
}

func (oh *OperationHandler) GetOperation(c *gin.Context) {
	operation, err := oh.operationStore.GetByID(c.Param("operation_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Operation not found"})
		return
	}
	c.JSON(http.StatusOK, toOperationResponse(operation))
}

func (oh *OperationHandler) ListOperations(c *gin.Context) {
	operations, err := oh.operationStore.ListByRepository(c.Param("repo_id"), 50, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list operations"})
		return
	}
	responses := make([]*OperationResponse, 0, len(operations))
	for _, op := range operations {
		responses = append(responses, toOperationResponse(op))
	}
	c.JSON(http.StatusOK, gin.H{"operations": responses})
}

// ConfirmPlan is the "yes": an approver confirms a plan that is waiting for
// confirmation, and TowerCrane applies exactly that saved plan. Emergency
// plans can only be confirmed by admins.
func (oh *OperationHandler) ConfirmPlan(c *gin.Context) {
	plan, err := oh.operationStore.GetByID(c.Param("operation_id"))
	if err != nil || plan.Type != "plan" {
		c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"})
		return
	}
	if plan.Emergency && !auth.Role(c.GetString("role")).AtLeast(auth.RoleAdmin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only admins can confirm an emergency plan"})
		return
	}
	repo, err := oh.repositoryStore.GetRepositoryByID(plan.RepositoryID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}

	claimed, err := oh.operationStore.ClaimConfirmation(plan.ID, "Confirmed: applying this plan")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to confirm the plan"})
		return
	}
	if !claimed {
		c.JSON(http.StatusConflict, gin.H{"error": "this plan is not waiting for confirmation (already confirmed, discarded or superseded)"})
		return
	}

	apply := &db.Operation{
		RepositoryID: plan.RepositoryID,
		UserID:       c.GetString("user_id"),
		Type:         "apply",
		Status:       "pending",
		Application:  plan.Application,
		Environment:  plan.Environment,
		WorkingDir:   plan.WorkingDir,
		Emergency:    plan.Emergency,
		SourcePlanID: plan.ID,
		Log:          "Applying the confirmed plan " + plan.ID,
	}
	if err := oh.operationStore.Create(apply); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create the apply"})
		return
	}
	oh.planResolved(plan.ID, apply)
	go oh.runConfirmedApply(apply, plan, repo)
	c.JSON(http.StatusAccepted, toOperationResponse(apply))
}

// DiscardPlan says "no" to a plan waiting for confirmation.
func (oh *OperationHandler) DiscardPlan(c *gin.Context) {
	plan, err := oh.operationStore.GetByID(c.Param("operation_id"))
	if err != nil || plan.Type != "plan" {
		c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"})
		return
	}
	if plan.Emergency && !auth.Role(c.GetString("role")).AtLeast(auth.RoleAdmin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only admins can discard an emergency plan"})
		return
	}
	claimed, err := oh.operationStore.ClaimConfirmation(plan.ID, "Discarded: not applied")
	if err != nil || !claimed {
		c.JSON(http.StatusConflict, gin.H{"error": "this plan is not waiting for confirmation"})
		return
	}
	removePlanFile(plan.PlanFile)
	oh.planResolved(plan.ID, nil)
	c.Status(http.StatusNoContent)
}

func (oh *OperationHandler) planResolved(planID string, apply *db.Operation) {
	if oh.onPlanResolved != nil {
		oh.onPlanResolved(planID, apply)
	}
}

// runOptions tweaks how an operation is executed.
type runOptions struct {
	// auto marks operations TowerCrane started on its own after a merge: a
	// plan of an apply: auto Application that passes the policy is applied
	// right away; anything else waits for an approver's "yes".
	auto bool
	// job marks an apply or destroy started by a Job (kind: Job). The merged
	// Job authorizes it: it runs without a "yes", and may destroy its own
	// ephemeral Application (lifecycle: job).
	job bool
}

func (oh *OperationHandler) run(operation *db.Operation, repo *models.Repository) {
	oh.runWith(operation, repo, runOptions{})
}

// fail completes an operation as failed.
func (oh *OperationHandler) fail(op *db.Operation, output, msg string) {
	oh.complete(op, "failed", output, msg)
}

func (oh *OperationHandler) runWith(operation *db.Operation, repo *models.Repository, opts runOptions) {
	oh.operationStore.Start(operation.ID)
	live := oh.liveLogFor(operation)

	// One operation at a time per repository clone (the workspace holds the
	// lock of the clone Terraform runs in).
	ws, err := oh.openWorkspace(operation, repo, operation.Application)
	if err != nil {
		oh.fail(operation, "", err.Error())
		return
	}
	defer ws.Close()
	if ws.Code.ID != repo.ID {
		live.WriteLine(fmt.Sprintf("Code from %s at %s (manifests at %s)", ws.Code.FullName, short(ws.CodeSHA), short(ws.ConfigSHA)))
	}

	// Steps 1-2 of the policy.
	dec, err := oh.evaluateApp(operation.RepositoryID, ws)
	if err != nil {
		oh.fail(operation, "", "Policy check failed: "+err.Error())
		return
	}
	operation.WorkingDir = dec.App.Spec.Source.Path
	operation.Environment = path.Base(operation.WorkingDir)
	oh.operationStore.SetLocation(operation.ID, operation.WorkingDir, operation.Environment)

	env, err := oh.tofuEnv(repo, dec.App)
	if err != nil {
		oh.fail(operation, "", err.Error())
		return
	}
	executor := iac.NewExecutor(iac.ExecutorConfig{
		RepoPath:    ws.CodePath,
		Environment: operation.Environment,
		WorkingDir:  operation.WorkingDir,
		Env:         env,
		PlanFile:    planFilePath(operation.ID),
		Engine:      dec.App.EngineName(),
		Terragrunt:  isTerragrunt(dec.Files, dec.App),
		Log:         live,
	})
	if err := executor.CheckBinaries(); err != nil {
		oh.fail(operation, "", err.Error())
		return
	}

	switch operation.Type {
	case "plan":
		result, err := executor.Plan()
		if err != nil {
			oh.fail(operation, "", err.Error())
			return
		}
		if result.HasError {
			removePlanFile(planFilePath(operation.ID))
			oh.fail(operation, result.Output, result.Error)
			return
		}
		oh.operationStore.UpdatePlan(operation.ID, result.Output)
		oh.complete(operation, "success", result.Output, "")
		oh.afterPlan(operation, repo, ws, env, executor, dec, result.HasChanges, opts)

	case "apply":
		// A direct apply (approver's explicit decision): plan, check the
		// plan, then apply exactly that plan.
		if !operation.Emergency && !dec.Allowed() {
			oh.fail(operation, "", dec.Describe())
			return
		}
		result, err := executor.Plan()
		if err != nil {
			oh.fail(operation, "", err.Error())
			return
		}
		if result.HasError {
			removePlanFile(planFilePath(operation.ID))
			oh.fail(operation, result.Output, result.Error)
			return
		}
		oh.operationStore.UpdatePlan(operation.ID, result.Output)
		if !operation.Emergency {
			review, err := reviewPlan(executor, planFilePath(operation.ID), dec)
			if err != nil {
				oh.fail(operation, result.Output, "Plan check failed, refusing to apply: "+err.Error())
				return
			}
			// A direct Apply is an explicit "yes": manual is fine, reject
			// (and editScope violations) are not.
			if why := review.blockedReason(); why != "" {
				removePlanFile(planFilePath(operation.ID))
				oh.fail(operation, result.Output, why)
				return
			}
		}
		applied, err := executor.ApplyPlan(planFilePath(operation.ID))
		removePlanFile(planFilePath(operation.ID))
		if err != nil {
			oh.fail(operation, "", err.Error())
			return
		}
		if applied.Error != "" {
			oh.fail(operation, applied.Output, applied.Error)
			return
		}
		oh.complete(operation, "success", applied.Output, "")

	case "destroy":
		jobDestroy := opts.job && dec.App.Spec.Lifecycle == manifest.LifecycleJob
		if !operation.Emergency && !jobDestroy {
			if !dec.Allowed() {
				oh.fail(operation, "", dec.Describe())
				return
			}
			allDelete := dec.App.Decide([]manifest.PlannedChange{{Address: "*", Action: manifest.ActionDelete}})
			if !dec.App.Spec.EditScope.All || allDelete.Apply == manifest.ApplyReject {
				oh.fail(operation, "", "Blocked: destroying needs the Application to declare editScope.all: true and a syncPolicy rule that doesn't reject delete (or an admin emergency destroy).")
				return
			}
		}
		result, err := executor.Destroy()
		if err != nil {
			oh.fail(operation, "", err.Error())
			return
		}
		if result.Error != "" {
			oh.fail(operation, result.Output, result.Error)
			return
		}
		oh.complete(operation, "success", result.Output, "")
	}
}

// afterPlan decides what happens to a successful plan: nothing (no
// changes), blocked by the policy, applied right away (apply: auto), or
// waiting for an approver's "yes". Called with the workspace lock held.
func (oh *OperationHandler) afterPlan(plan *db.Operation, repo *models.Repository, ws *appWorkspace, env []string,
	executor *iac.Executor, dec *appDecision, hasChanges bool, opts runOptions) {
	plan.HasChanges = hasChanges
	plan.PlanFile = planFilePath(plan.ID)

	if !hasChanges {
		plan.PolicyNote = "No changes: the infrastructure matches the code"
		removePlanFile(plan.PlanFile)
		plan.PlanFile = ""
		oh.operationStore.SetPlanResult(plan)
		return
	}
	sum, err := fileSHA256(plan.PlanFile)
	if err != nil {
		plan.PolicyNote = "Apply blocked: the saved plan could not be read: " + err.Error()
		oh.operationStore.SetPlanResult(plan)
		return
	}
	plan.PlanSHA256 = sum

	if plan.Emergency {
		plan.AwaitingConfirmation = true
		plan.PolicyNote = "Emergency plan: only an admin can confirm it, and it will be applied without the TowerCrane policy"
		oh.operationStore.SetPlanResult(plan)
		return
	}

	if !dec.Allowed() {
		plan.PolicyNote = "Apply blocked. " + dec.Describe()
		oh.operationStore.SetPlanResult(plan)
		return
	}
	review, err := reviewPlan(executor, plan.PlanFile, dec)
	switch {
	case err != nil:
		plan.PolicyNote = "Apply blocked: the plan check failed: " + err.Error()
	case len(review.Violations) > 0:
		plan.PolicyNote = "Apply blocked. " + review.blockedReason()
	case review.Decision.Apply == manifest.ApplyReject:
		plan.PolicyNote = review.blockedReason()
	case opts.auto && review.Decision.Apply == manifest.ApplyAuto && !dec.Onboarding:
		plan.PolicyNote = "syncPolicy says auto for every change in this plan: applying automatically"
		oh.operationStore.SetPlanResult(plan)
		apply := &db.Operation{
			RepositoryID: plan.RepositoryID,
			UserID:       plan.UserID,
			Type:         "apply",
			Status:       "pending",
			Application:  plan.Application,
			Environment:  plan.Environment,
			WorkingDir:   plan.WorkingDir,
			SourcePlanID: plan.ID,
			Log:          "Auto-applying plan " + plan.ID + " (syncPolicy.apply: auto)",
		}
		if err := oh.operationStore.Create(apply); err == nil {
			oh.applySavedPlan(apply, plan, ws, env)
		}
		return
	default:
		plan.AwaitingConfirmation = true
		switch {
		case dec.Onboarding:
			plan.PolicyNote = "First apply of this Application: waiting for an approver's yes"
		case review.Decision.Apply == manifest.ApplyManual:
			plan.PolicyNote = describeReasons("Waiting for an approver's yes:", review.Decision.Reasons)
		default:
			plan.PolicyNote = "Waiting for an approver's yes"
		}
	}
	oh.operationStore.SetPlanResult(plan)
}

// runConfirmedApply applies a confirmed plan.
func (oh *OperationHandler) runConfirmedApply(apply, plan *db.Operation, repo *models.Repository) {
	// Whatever happens, report the final apply to the rollout engine (after
	// releasing the workspace, since the rollout may start the next plan).
	defer func() {
		if final, err := oh.operationStore.GetByID(apply.ID); err == nil {
			oh.planResolved(plan.ID, final)
		}
	}()
	oh.operationStore.Start(apply.ID)
	oh.liveLogFor(apply)
	ws, err := oh.openWorkspace(apply, repo, apply.Application)
	if err != nil {
		oh.fail(apply, "", err.Error())
		return
	}
	defer ws.Close()
	env, err := oh.tofuEnv(repo, ws.App)
	if err != nil {
		oh.fail(apply, "", err.Error())
		return
	}
	oh.applySavedPlan(apply, plan, ws, env)
}

// applySavedPlan applies exactly the plan an approver saw. It refuses if the
// default branch moved on since the plan was made and (outside emergencies)
// re-checks the whole policy, plan included. Called with the workspace lock
// held.
func (oh *OperationHandler) applySavedPlan(apply, plan *db.Operation, ws *appWorkspace, env []string) {
	oh.operationStore.Start(apply.ID)
	live := oh.liveLogFor(apply)

	oh.operationStore.UpdateCommit(apply.ID, ws.ConfigSHA)
	oh.operationStore.UpdateSourceCommit(apply.ID, ws.CodeSHA)
	planCode := plan.SourceCommitSHA
	if planCode == "" {
		planCode = plan.CommitSHA
	}
	switch {
	case ws.ConfigSHA != plan.CommitSHA:
		removePlanFile(plan.PlanFile)
		oh.fail(apply, "", fmt.Sprintf(
			"The manifests' default branch moved on since this plan (plan: %s, now: %s). Run a new plan and confirm that one.",
			short(plan.CommitSHA), short(ws.ConfigSHA)))
		return
	case ws.CodeSHA != planCode:
		removePlanFile(plan.PlanFile)
		oh.fail(apply, "", fmt.Sprintf(
			"%s's default branch moved on since this plan (plan: %s, now: %s). Run a new plan and confirm that one.",
			ws.Code.FullName, short(planCode), short(ws.CodeSHA)))
		return
	}

	app := ws.App
	executor := iac.NewExecutor(iac.ExecutorConfig{
		RepoPath: ws.CodePath, Environment: apply.Environment, WorkingDir: apply.WorkingDir, Env: env,
		Engine: app.EngineName(), Terragrunt: isTerragrunt(ws.Files, app), Log: live,
	})
	if !plan.Emergency {
		dec, err := oh.evaluateApp(plan.RepositoryID, ws)
		if err != nil {
			oh.fail(apply, "", "Policy check failed, refusing to apply: "+err.Error())
			return
		}
		if !dec.Allowed() {
			oh.fail(apply, "", dec.Describe())
			return
		}
		review, err := reviewPlan(executor, plan.PlanFile, dec)
		if err != nil {
			oh.fail(apply, "", "Plan check failed, refusing to apply: "+err.Error())
			return
		}
		// Confirming is a "yes": manual is fine; reject never is.
		if why := review.blockedReason(); why != "" {
			oh.fail(apply, "", why)
			return
		}
	}

	// The plan file is checked last, right before applying it: it must be
	// exactly the plan that was reviewed.
	if err := verifyPlanFile(plan); err != nil {
		removePlanFile(plan.PlanFile)
		oh.fail(apply, "", err.Error())
		return
	}
	result, err := executor.ApplyPlan(plan.PlanFile)
	if err != nil {
		oh.fail(apply, "", err.Error())
		return
	}
	removePlanFile(plan.PlanFile)
	if result.Error != "" {
		oh.fail(apply, result.Output, result.Error)
		return
	}
	oh.complete(apply, "success", result.Output, "")
}

// cloudAccountFor resolves the cloud account an instance runs with. An admin
// must have authorized it for the project (a manifest can't send code to an
// account nobody gave it); a project defined by a Project manifest must also
// request it there (spec.cloudAccounts). An instance that names no account
// uses the project's only authorized account, if it has exactly one.
func (oh *OperationHandler) cloudAccountFor(repo *models.Repository, app *manifest.Application) (*db.CloudAccount, error) {
	authorized, err := oh.integrations.RepositoryCloudAccounts(repo.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the project's cloud accounts: %w", err)
	}
	if app.CloudAccount == "" {
		if len(authorized) == 1 {
			return authorized[0], nil
		}
		return nil, nil // no cloud credentials (e.g. modules without providers)
	}
	where := "spec.destination.cloudAccount"
	if app.Group != "" {
		where = fmt.Sprintf("Group %q in %s", app.Group, app.GroupFile)
	}
	if oh.projects != nil {
		if p, err := oh.projects.Get(repo.ID); err == nil && p.ManifestFile != "" {
			requested := false
			for _, name := range p.RequestedCloudAccounts {
				requested = requested || name == app.CloudAccount
			}
			if !requested {
				return nil, fmt.Errorf("cloud account %q (%s) is not requested by the project: add it to spec.cloudAccounts in %s",
					app.CloudAccount, where, p.ManifestFile)
			}
		}
	}
	for _, a := range authorized {
		if a.Name == app.CloudAccount {
			return a, nil
		}
	}
	if _, err := oh.integrations.GetCloudAccountByName(app.CloudAccount); err != nil {
		return nil, fmt.Errorf("cloud account %q (%s) not found in TowerCrane", app.CloudAccount, where)
	}
	return nil, fmt.Errorf("cloud account %q (%s) is not authorized for this project yet: an admin can authorize it on the project's page",
		app.CloudAccount, where)
}

// planFilePath is where an operation's saved plan lives: outside the
// repository clone (which is reset before every operation), readable only
// by TowerCrane since plans can contain variable values.
func planFilePath(operationID string) string {
	dir := filepath.Join(git.WorkspaceRoot(), "plans")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, operationID+".tfplan")
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyPlanFile refuses a saved plan that changed since it was made (or
// that has no recorded hash, from an older version).
func verifyPlanFile(plan *db.Operation) error {
	if plan.PlanSHA256 == "" {
		return errors.New("this plan has no recorded checksum; run a new plan and confirm that one")
	}
	sum, err := fileSHA256(plan.PlanFile)
	if err != nil {
		return fmt.Errorf("the saved plan could not be read: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(sum), []byte(plan.PlanSHA256)) != 1 {
		return errors.New("the saved plan file changed since it was made, refusing to apply it; run a new plan")
	}
	return nil
}

func removePlanFile(p string) {
	if p != "" {
		_ = os.Remove(p)
	}
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// prepareWorkspace clones/updates the repository and records on the
// operation which commit it runs against.
func (oh *OperationHandler) prepareWorkspace(operation *db.Operation, repo *models.Repository) (string, error) {
	token, err := oh.gh.Token(context.Background(), repo)
	if err != nil {
		return "", err
	}
	branch := repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	localPath, err := git.EnsureWorkspace(repo.FullName, repo.URL, branch, token)
	if err != nil {
		return "", fmt.Errorf("failed to prepare workspace: %w", err)
	}
	sha, err := git.CurrentCommit(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve current commit: %w", err)
	}
	oh.operationStore.UpdateCommit(operation.ID, sha)
	operation.CommitSHA = sha
	return localPath, nil
}

// tofuEnv is the environment tofu runs with: the server environment minus
// every credential, plus the instance's cloud account (its Group's, or
// spec.destination.cloudAccount), TOWERCRANE_* facts about the instance and
// the manifest's spec.variables as TF_VAR_<name>. TowerCrane never stores
// application secrets: code that needs one reads it from the cloud (e.g.
// AWS Secrets Manager).
func (oh *OperationHandler) tofuEnv(repo *models.Repository, app *manifest.Application) ([]string, error) {
	env := cloud.BaseEnv()

	acct, err := oh.cloudAccountFor(repo, app)
	if err != nil {
		return nil, err
	}
	// Facts code can use, e.g. Terragrunt keying its state by group:
	// get_env("TOWERCRANE_GROUP", "default").
	env = append(env, "TOWERCRANE_APPLICATION="+app.App, "TOWERCRANE_INSTANCE="+app.Metadata.Name)
	if app.Group != "" { // unset (not empty) without groups, so get_env defaults apply
		env = append(env, "TOWERCRANE_GROUP="+app.Group)
	}
	if acct != nil {
		env = append(env, "TOWERCRANE_CLOUD_ACCOUNT="+acct.Name)
	}
	if acct != nil {
		secret, err := secrets.Decrypt(acct.SecretEnc)
		if err != nil {
			return nil, err
		}
		creds := cloud.AWSCredentials{
			AccountID: acct.AWSAccountID, AccessKeyID: acct.AccessKeyID, SecretAccessKey: secret,
			Region: acct.Region, EndpointURL: acct.EndpointURL,
		}
		env = append(env, creds.Env()...)
	}

	values := map[string]string{}
	for name, v := range app.Spec.Variables {
		s, err := manifest.VariableValue(v)
		if err != nil {
			return nil, fmt.Errorf("spec.variables.%s: %w", name, err)
		}
		values[name] = s
	}
	for name, value := range values {
		env = append(env, "TF_VAR_"+name+"="+value)
	}
	return env, nil
}
