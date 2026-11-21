package iac

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ExecutorConfig struct {
	RepoPath    string // Path to cloned repository
	Environment string // dev, prod, etc
	WorkingDir  string // Subdirectory within repo (e.g., "environments/dev")
	// TargetAddresses restricts plan/apply/destroy to these Terraform resource
	// addresses via `-target`, when non-empty. General-purpose scoping
	// primitive; not used by the policy, which checks the whole plan instead
	// (see internal/tfmap.PlanGate).
	TargetAddresses []string
	// PlanFile is where Plan saves the binary plan (outside the repository
	// clone, so it survives workspace resets) for ApplyPlan to apply later.
	PlanFile string
	// Env is the complete environment tofu runs with (see cloud.BaseEnv);
	// the server's own environment is never inherited.
	Env []string
	// Engine is "opentofu" (default) or "terraform".
	Engine string
	// Terragrunt runs the engine through Terragrunt (the working directory
	// is a Terragrunt unit).
	Terragrunt bool
	// Log, when set, receives every command line and its output (stdout and
	// stderr) as it runs, so the UI can show live logs.
	Log io.Writer
}

// Binary paths, overridable for installs outside PATH.
func engineBinary(engine string) string {
	if engine == "terraform" {
		return envOr("TOWERCRANE_TERRAFORM_BIN", "terraform")
	}
	return envOr("TOWERCRANE_TOFU_BIN", "tofu")
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// Binary is what the executor runs: the engine, or Terragrunt.
func (e *Executor) Binary() string {
	if e.config.Terragrunt {
		return envOr("TOWERCRANE_TERRAGRUNT_BIN", "terragrunt")
	}
	return engineBinary(e.config.Engine)
}

// CheckBinaries reports a clear error when a required binary is missing.
func (e *Executor) CheckBinaries() error {
	bins := []string{engineBinary(e.config.Engine)}
	if e.config.Terragrunt {
		bins = append(bins, e.Binary())
	}
	for _, b := range bins {
		if _, err := exec.LookPath(b); err != nil {
			return fmt.Errorf("%s is not installed on the TowerCrane server (looked for %q)", filepath.Base(b), b)
		}
	}
	return nil
}

type Executor struct {
	config ExecutorConfig
}

type PlanResult struct {
	// HasChanges is false when tofu reported "No changes".
	HasChanges bool
	Output     string
	Changes  []string // List of resources that will change
	HasError bool
	Error    string
}

type ApplyResult struct {
	Output   string
	Error    string
	Changed  bool
	AppliedResources []string
}

type DestroyResult struct {
	Output   string
	Error    string
}

func NewExecutor(config ExecutorConfig) *Executor {
	return &Executor{config: config}
}

func (e *Executor) getWorkDir() string {
	if e.config.WorkingDir != "" {
		return filepath.Join(e.config.RepoPath, e.config.WorkingDir)
	}
	return e.config.RepoPath
}

// command builds a tofu invocation in dir with the configured environment.
func (e *Executor) command(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command(e.Binary(), args...)
	cmd.Dir = dir
	cmd.Env = append([]string{}, e.config.Env...)
	if len(cmd.Env) == 0 {
		cmd.Env = []string{} // non-nil: never inherit the server environment
	}
	if e.config.Terragrunt {
		engine := engineBinary(e.config.Engine)
		cmd.Env = append(cmd.Env,
			"TG_TF_PATH="+engine, "TERRAGRUNT_TFPATH="+engine,
			"TG_NON_INTERACTIVE=true", "TERRAGRUNT_NON_INTERACTIVE=true",
			// Create the remote state backend (e.g. the S3 bucket) the
			// first time a unit uses it.
			"TG_BACKEND_BOOTSTRAP=true",
			"TG_NO_COLOR=true",
		)
	}
	return cmd
}

// run runs cmd, copying its output to stdout/stderr and to the live log.
func (e *Executor) run(cmd *exec.Cmd, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if e.config.Log != nil {
		// stdout and stderr are copied from two goroutines.
		log := &lockedWriter{w: e.config.Log}
		fmt.Fprintf(log, "\n$ %s %s\n", filepath.Base(cmd.Path), strings.Join(cmd.Args[1:], " "))
		same := stdout == stderr
		stdout = io.MultiWriter(stdout, log)
		if same {
			stderr = stdout // one writer: os/exec then never writes to it concurrently
		} else {
			stderr = io.MultiWriter(stderr, log)
		}
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Its own process group, so a timeout also stops the providers it runs.
	newProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	limit := RunTimeout()
	done := make(chan struct{})
	timedOut := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case <-time.After(limit):
		}
		close(timedOut)
		// Ask nicely first (tofu releases its state lock), then stop it.
		stopProcessGroup(cmd, false)
		select {
		case <-done:
		case <-time.After(time.Minute):
			stopProcessGroup(cmd, true)
		}
	}()
	err := cmd.Wait()
	close(done)
	select {
	case <-timedOut:
		return fmt.Errorf("%s took longer than %s and was stopped (TOWERCRANE_RUN_TIMEOUT)", filepath.Base(cmd.Path), limit)
	default:
		return err
	}
}

// RunTimeout is how long one tofu/terraform/terragrunt command may run:
// TOWERCRANE_RUN_TIMEOUT (a Go duration such as 90m), 2 hours by default.
func RunTimeout() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("TOWERCRANE_RUN_TIMEOUT")); err == nil && d > 0 {
		return d
	}
	return 2 * time.Hour
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// targetArgs builds `-target=<address>` flags for every configured
// TargetAddresses entry, restricting the operation to just those resources.
func (e *Executor) targetArgs() []string {
	args := make([]string, 0, len(e.config.TargetAddresses))
	for _, addr := range e.config.TargetAddresses {
		args = append(args, "-target="+addr)
	}
	return args
}

// Plan executes terraform plan
func (e *Executor) Plan() (*PlanResult, error) {
	workDir := e.getWorkDir()

	// Check if working directory exists
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("working directory does not exist: %s", workDir)
	}

	result := &PlanResult{}

	// Init terraform/opentofu
	initCmd := e.command(workDir, "init", "-input=false", "-no-color")

	var initOutput bytes.Buffer
	if err := e.run(initCmd, nil, &initOutput); err != nil {
		result.HasError = true
		result.Error = fmt.Sprintf("Init failed: %s\nOutput: %s", err.Error(), initOutput.String())
		return result, nil
	}

	// Run the plan. -detailed-exitcode: 0 = no changes, 2 = changes.
	planFile := e.config.PlanFile
	if planFile == "" {
		planFile = "tfplan"
	}
	planArgs := append([]string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-out=" + planFile}, e.targetArgs()...)
	planCmd := e.command(workDir, planArgs...)

	var planOutput bytes.Buffer
	var planErr bytes.Buffer

	err := e.run(planCmd, &planOutput, &planErr)
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		result.HasChanges = true
		err = nil
	}
	if err != nil {
		result.HasError = true
		result.Error = fmt.Sprintf("Plan failed: %s\nStderr: %s", err.Error(), planErr.String())
		return result, nil
	}

	output := planOutput.String()
	result.Output = output
	result.HasError = false

	// Parse changes from output
	result.Changes = e.parseChanges(output)

	return result, nil
}

// Apply executes terraform apply
func (e *Executor) Apply() (*ApplyResult, error) {
	workDir := e.getWorkDir()

	// Check if working directory exists
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("working directory does not exist: %s", workDir)
	}

	result := &ApplyResult{}

	// Check if tfplan exists from previous plan. When TargetAddresses are set
	// (an edit-scope restriction applies), always regenerate the plan so a
	// stale, unrestricted tfplan can never be applied unchecked.
	tfplanPath := filepath.Join(workDir, "tfplan")
	_, statErr := os.Stat(tfplanPath)
	if statErr != nil || len(e.config.TargetAddresses) > 0 {
		// Run plan first
		planArgs := append([]string{"plan", "-input=false", "-no-color", "-out=tfplan"}, e.targetArgs()...)
		planCmd := e.command(workDir, planArgs...)

		var planErr bytes.Buffer
		if err := e.run(planCmd, nil, &planErr); err != nil && err.Error() != "exit status 2" {
			result.Error = fmt.Sprintf("Plan failed: %s", planErr.String())
			return result, nil
		}
	}

	// Run apply with tfplan
	applyCmd := e.command(workDir, "apply", "-input=false", "-no-color", "-auto-approve", "tfplan")

	var applyOutput bytes.Buffer
	var applyErr bytes.Buffer

	if err := e.run(applyCmd, &applyOutput, &applyErr); err != nil {
		result.Error = fmt.Sprintf("Apply failed: %s\nStderr: %s", err.Error(), applyErr.String())
		return result, nil
	}

	output := applyOutput.String()
	result.Output = output
	result.Changed = strings.Contains(output, "Apply complete")
	result.AppliedResources = e.parseAppliedResources(output)

	// Clean up tfplan file
	os.Remove(tfplanPath)

	return result, nil
}

// ApplyPlan applies exactly the saved plan in planFile (what an approver
// confirmed). tofu refuses it if the state changed since it was created.
func (e *Executor) ApplyPlan(planFile string) (*ApplyResult, error) {
	workDir := e.getWorkDir()
	if _, err := os.Stat(planFile); err != nil {
		return nil, fmt.Errorf("saved plan not found (%v); run a new plan", err)
	}

	result := &ApplyResult{}
	initCmd := e.command(workDir, "init", "-input=false", "-no-color")
	var initOut bytes.Buffer
	if err := e.run(initCmd, &initOut, &initOut); err != nil {
		result.Error = fmt.Sprintf("Init failed: %s\n%s", err, initOut.String())
		return result, nil
	}

	applyCmd := e.command(workDir, "apply", "-input=false", "-no-color", "-auto-approve", planFile)
	var out, stderr bytes.Buffer
	if err := e.run(applyCmd, &out, &stderr); err != nil {
		result.Output = out.String()
		result.Error = fmt.Sprintf("Apply failed: %s\nStderr: %s", err.Error(), stderr.String())
		return result, nil
	}
	result.Output = out.String()
	result.Changed = strings.Contains(result.Output, "Apply complete")
	result.AppliedResources = e.parseAppliedResources(result.Output)
	return result, nil
}

// Destroy executes terraform destroy
func (e *Executor) Destroy() (*DestroyResult, error) {
	workDir := e.getWorkDir()

	// Check if working directory exists
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("working directory does not exist: %s", workDir)
	}

	result := &DestroyResult{}

	// Run destroy
	destroyArgs := append([]string{"destroy", "-input=false", "-no-color", "-auto-approve"}, e.targetArgs()...)
	destroyCmd := e.command(workDir, destroyArgs...)

	var destroyOutput bytes.Buffer
	var destroyErr bytes.Buffer

	if err := e.run(destroyCmd, &destroyOutput, &destroyErr); err != nil {
		result.Error = fmt.Sprintf("Destroy failed: %s\nStderr: %s", err.Error(), destroyErr.String())
		return result, nil
	}

	result.Output = destroyOutput.String()

	return result, nil
}

// parseChanges extracts resource changes from terraform plan output
func (e *Executor) parseChanges(output string) []string {
	var changes []string
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Terraform plan output shows changes with # prefix
		if strings.HasPrefix(line, "#") || strings.Contains(line, "will be") {
			changes = append(changes, line)
		}
	}

	return changes
}

// parseAppliedResources extracts applied resources from apply output
func (e *Executor) parseAppliedResources(output string) []string {
	var resources []string
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for applied resources
		if strings.Contains(line, ": Creating") || strings.Contains(line, ": Modifying") {
			resources = append(resources, line)
		}
	}

	return resources
}

// PlannedChange is one resource change in a saved plan.
type PlannedChange struct {
	Address string   `json:"address"`
	Mode    string   `json:"mode"` // managed or data
	Actions []string `json:"actions"`
}

// ShowPlan reads a saved plan with `tofu show -json` and returns its
// resource changes. Must run in the same workspace the plan was made in.
func (e *Executor) ShowPlan(planFile string) ([]PlannedChange, error) {
	cmd := e.command(e.getWorkDir(), "show", "-json", "-no-color", planFile)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tofu show failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	var doc struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Mode    string `json:"mode"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		return nil, fmt.Errorf("failed to read the plan JSON: %w", err)
	}
	changes := make([]PlannedChange, 0, len(doc.ResourceChanges))
	for _, rc := range doc.ResourceChanges {
		changes = append(changes, PlannedChange{Address: rc.Address, Mode: rc.Mode, Actions: rc.Change.Actions})
	}
	return changes, nil
}
