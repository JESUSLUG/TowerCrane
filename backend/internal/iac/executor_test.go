package iac

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestRunCopiesOutputToLiveLog(t *testing.T) {
	var live, out, errBuf bytes.Buffer
	e := NewExecutor(ExecutorConfig{Log: &live})
	cmd := exec.Command("sh", "-c", "echo to-stdout; echo to-stderr >&2")
	if err := e.run(cmd, &out, &errBuf); err != nil {
		t.Fatal(err)
	}
	if out.String() != "to-stdout\n" || errBuf.String() != "to-stderr\n" {
		t.Fatalf("buffers: %q %q", out.String(), errBuf.String())
	}
	got := live.String()
	for _, want := range []string{"$ sh -c", "to-stdout", "to-stderr"} {
		if !strings.Contains(got, want) {
			t.Errorf("live log misses %q:\n%s", want, got)
		}
	}
}

func TestRunWithoutLiveLog(t *testing.T) {
	var out bytes.Buffer
	e := NewExecutor(ExecutorConfig{})
	if err := e.run(exec.Command("sh", "-c", "echo hi; echo x >&2"), &out, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hi\n" {
		t.Fatalf("got %q", out.String())
	}
}
