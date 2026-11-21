//go:build linux

package harden

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// A child running as the same user can't read the hardened process's
// environment.
func TestChildCannotReadEnviron(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read any process")
	}
	if err := Process(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("cat", fmt.Sprintf("/proc/%d/environ", os.Getpid())).CombinedOutput()
	if err == nil {
		t.Fatalf("child read the environment: %q", out)
	}
}
