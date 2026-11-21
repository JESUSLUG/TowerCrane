package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func head(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) string {
	dir := t.TempDir()
	runGitCmd(t, dir, "init", "-q")
	runGitCmd(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func TestDiffFiles(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "env/variables.tf", "a\nb\nc\nd\ne\n")
	write(t, dir, "env/gone.tf", "x\n")
	write(t, dir, "env/old-name.tf", "moved\n")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "initial")
	oldSHA := head(t, dir)

	// modify line 2, delete line 4, remove a file, rename a file.
	write(t, dir, "env/variables.tf", "a\nB\nc\ne\n")
	runGitCmd(t, dir, "rm", "-q", "env/gone.tf")
	runGitCmd(t, dir, "mv", "env/old-name.tf", "env/new-name.tf")
	runGitCmd(t, dir, "commit", "-q", "-am", "change")
	newSHA := head(t, dir)

	diffs, err := DiffFiles(dir, oldSHA, newSHA)
	if err != nil {
		t.Fatalf("DiffFiles: %v", err)
	}
	got := map[string]FileDiff{}
	for _, d := range diffs {
		got[d.Path] = d
	}

	want := map[string]FileDiff{
		"env/variables.tf": {Path: "env/variables.tf", Removed: []LineRange{{2, 2}, {4, 4}}, Added: []LineRange{{2, 2}}},
		"env/gone.tf":      {Path: "env/gone.tf", Removed: []LineRange{{1, 1}}},
		"env/old-name.tf":  {Path: "env/old-name.tf", Removed: []LineRange{{1, 1}}},
		"env/new-name.tf":  {Path: "env/new-name.tf", Added: []LineRange{{1, 1}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiffFiles =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDiffFiles_FirstCommitAgainstEmptyTree(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "main.tf", "a\nb\n")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "initial")

	diffs, err := DiffFiles(dir, EmptyTree, head(t, dir))
	if err != nil {
		t.Fatalf("DiffFiles: %v", err)
	}
	if len(diffs) != 1 || !reflect.DeepEqual(diffs[0].Added, []LineRange{{1, 2}}) {
		t.Errorf("unexpected diff against empty tree: %+v", diffs)
	}
}

func TestReadFilesAt(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.tf", "first\n")
	write(t, dir, "sub/b.hcl", "")
	write(t, dir, "notes.txt", "ignored")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "one")
	first := head(t, dir)

	write(t, dir, "a.tf", "second\n")
	runGitCmd(t, dir, "commit", "-q", "-am", "two")

	files, err := ReadFilesAt(dir, first, ".tf", ".hcl")
	if err != nil {
		t.Fatalf("ReadFilesAt: %v", err)
	}
	want := map[string]string{"a.tf": "first\n", "sub/b.hcl": ""}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("ReadFilesAt = %q, want %q", files, want)
	}
}
