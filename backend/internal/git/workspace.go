package git

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// WorkspaceRoot is where repositories are cloned so `tofu`/`terraform` can be
// run against a real filesystem path. It can be overridden with the
// WORKSPACE_DIR environment variable (useful for tests or custom volumes).
func WorkspaceRoot() string {
	if dir := os.Getenv("WORKSPACE_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), "towercrane-workspaces")
}

// WorkspacePath is where a repository's clone lives.
func WorkspacePath(fullName string) string {
	return filepath.Join(WorkspaceRoot(), filepath.FromSlash(fullName))
}

var (
	locksMu sync.Mutex
	locks   = map[string]*sync.Mutex{}
)

// LockWorkspace serialises everything that touches one repository's clone
// (fetch/reset, tofu runs, PR evaluations) so they never interleave. Call
// the returned function to release it.
func LockWorkspace(fullName string) func() {
	locksMu.Lock()
	m, ok := locks[fullName]
	if !ok {
		m = &sync.Mutex{}
		locks[fullName] = m
	}
	locksMu.Unlock()
	m.Lock()
	return m.Unlock
}

// EnsureWorkspace makes sure a local, up-to-date clone of repoURL/branch
// exists on disk and returns its path. If the repository was already cloned
// it is fetched and hard-reset to the requested branch instead of being
// cloned again. Callers must hold LockWorkspace(fullName).
//
// token (a GitHub App installation token or an OAuth token) is passed to git
// through environment variables for the duration of each command only: it
// is never written to the remote URL or .git/config.
func EnsureWorkspace(fullName, repoURL, branch, token string) (string, error) {
	if fullName == "" {
		return "", fmt.Errorf("repository full name is required")
	}
	if branch == "" {
		branch = "main"
	}
	cloneURL, err := cleanURL(repoURL)
	if err != nil {
		return "", err
	}

	dest := WorkspacePath(fullName)

	// Full (non-shallow) history is required: the policy check diffs the
	// previously applied commit against the new one.
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		// Older versions stored the token in the remote URL; reset it.
		if err := runGit(dest, "", "remote", "set-url", "origin", cloneURL); err != nil {
			return "", err
		}
		if err := runGit(dest, token, "fetch", "--prune", "origin", "+refs/heads/"+branch+":refs/remotes/origin/"+branch); err != nil {
			return "", err
		}
		if err := runGit(dest, "", "reset", "--hard", "origin/"+branch); err != nil {
			return "", err
		}
		// Remove files tofu or a previous run left behind, but keep local
		// state and provider caches.
		if err := runGit(dest, "", "clean", "-fdx", "-e", ".terraform", "-e", ".terragrunt-cache", "-e", "*.tfstate", "-e", "*.tfstate.backup", "-e", ".terraform.lock.hcl"); err != nil {
			return "", err
		}
		return dest, nil
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("failed to create workspace directory: %w", err)
	}
	if err := runGit("", token, "clone", "--branch", branch, cloneURL, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// FetchPullRequest fetches a PR's head into refs/towercrane/pr/<n> (without
// touching the working tree) and returns the fetched commit. Callers must
// hold LockWorkspace.
func FetchPullRequest(dir, token string, number int) (string, error) {
	ref := fmt.Sprintf("refs/towercrane/pr/%d", number)
	if err := runGit(dir, token, "fetch", "origin", fmt.Sprintf("+refs/pull/%d/head:%s", number, ref)); err != nil {
		return "", err
	}
	out, err := gitOutput(dir, "rev-parse", ref)
	return strings.TrimSpace(out), err
}

// MergeBase returns the best common ancestor of two refs.
func MergeBase(dir, a, b string) (string, error) {
	out, err := gitOutput(dir, "merge-base", a, b)
	return strings.TrimSpace(out), err
}

// CurrentCommit returns the commit SHA currently checked out in the clone at
// dir.
func CurrentCommit(dir string) (string, error) {
	out, err := gitOutput(dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// CommitExists reports whether sha is a known object in the given clone
// (e.g. to check an old baseline commit is still reachable before diffing).
func CommitExists(dir, sha string) bool {
	cmd := exec.Command("git", "cat-file", "-e", sha+"^{commit}")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// cleanURL strips any credentials from a repository URL.
func cleanURL(repoURL string) (string, error) {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("invalid repository URL: %w", err)
	}
	parsed.User = nil
	return parsed.String(), nil
}

// authEnv passes the token to git as an HTTP Authorization header through
// GIT_CONFIG_* variables (git >= 2.31), which unlike command-line flags are
// not visible to other users in the process list.
func authEnv(token string) []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if token == "" {
		return env
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return append(env,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic,
	)
}

func runGit(dir, token string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = authEnv(token)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if token != "" {
			msg = strings.ReplaceAll(msg, token, "***")
		}
		return fmt.Errorf("git %s failed: %w (%s)", args[0], err, msg)
	}
	return nil
}

