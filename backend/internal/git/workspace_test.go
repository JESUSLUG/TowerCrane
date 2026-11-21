package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureWorkspaceNeverPersistsToken(t *testing.T) {
	origin := newRepo(t)
	write(t, origin, "main.tf", "a\n")
	runGitCmd(t, origin, "add", ".")
	runGitCmd(t, origin, "commit", "-q", "-m", "one")
	runGitCmd(t, origin, "branch", "-M", "main")

	t.Setenv("WORKSPACE_DIR", t.TempDir())
	const token = "ghs_supersecrettoken"

	dir, err := EnsureWorkspace("acme/infra", "file://"+origin, "main", token)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	// Second call takes the fetch/reset path.
	write(t, origin, "main.tf", "b\n")
	runGitCmd(t, origin, "commit", "-q", "-am", "two")
	if _, err := EnsureWorkspace("acme/infra", "file://"+origin, "main", token); err != nil {
		t.Fatalf("update: %v", err)
	}

	cfg, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), token) {
		t.Fatalf(".git/config contains the token:\n%s", cfg)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "main.tf"))
	if string(got) != "b\n" {
		t.Errorf("workspace not updated, main.tf = %q", got)
	}
}

func TestCleanURLStripsCredentials(t *testing.T) {
	got, err := cleanURL("https://x-access-token:secret@github.com/acme/infra.git")
	if err != nil || got != "https://github.com/acme/infra.git" {
		t.Errorf("cleanURL = %q, %v", got, err)
	}
}
